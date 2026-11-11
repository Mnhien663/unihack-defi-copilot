package avalanche

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/rpc"

	"github.com/ethereum/go-ethereum"
)

type SwapSimulationRPC interface {
	RequirementsRPCClient

	EstimateGas(
		ctx context.Context,
		msg ethereum.CallMsg,
	) (uint64, error)

	SuggestGasPrice(ctx context.Context) (*big.Int, error)
}

type SwapSimulationResult struct {
	Transaction  BuiltSwapTransaction `json:"transaction"`
	Requirements SwapRequirements     `json:"requirements"`

	// blocked, reverted, or succeeded.
	// RPC failures are returned as Go errors.
	Status string `json:"status"`

	// Raw output from a successful eth_call.
	SimulatedAmountOutRaw *string `json:"simulated_amount_out_raw"`

	GasEstimate     *uint64 `json:"gas_estimate"`
	GasLimit        *uint64 `json:"gas_limit"`
	GasPriceWei     *string `json:"gas_price_wei"`
	EstimatedFeeWei *string `json:"estimated_fee_wei"`

	// Decoded when the RPC supplies recognizable revert data.
	RevertName *string `json:"revert_name"`

	Provenance Provenance `json:"provenance"`
}

type SwapSimulator struct {
	client       SwapSimulationRPC
	builder      *SwapTxBuilder
	requirements *SwapRequirementsReader
}

func NewSwapSimulator(client SwapSimulationRPC) (*SwapSimulator, error) {
	if client == nil {
		return nil, errors.New("simulation RPC client is nil")
	}

	builder, err := NewSwapTxBuilder()
	if err != nil {
		return nil, err
	}
	requirements, err := NewSwapRequirementsReader(client)
	if err != nil {
		return nil, err
	}

	return &SwapSimulator{
		client:       client,
		builder:      builder,
		requirements: requirements,
	}, nil
}

func (s *SwapSimulator) SimulateSwap(
	ctx context.Context,
	request BuildSwapRequest,
) (SwapSimulationResult, error) {
	var empty SwapSimulationResult

	readCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	built, err := s.builder.BuildSwapTx(request, time.Now().UTC())
	if err != nil {
		return empty, err
	}

	requirements, err := s.requirements.ReadSwapRequirements(
		readCtx, request,
	)
	if err != nil {
		return empty, err
	}

	result := SwapSimulationResult{
		Transaction:  built,
		Requirements: requirements,
		Status:       "blocked",
		Provenance:   requirements.Provenance,
	}

	// Revalidate expiry before returning any successful observation.
	finish := func() (SwapSimulationResult, error) {
		if err := readCtx.Err(); err != nil {
			return empty, err
		}
		if _, err := s.builder.BuildSwapTx(
			request, time.Now().UTC(),
		); err != nil {
			return empty, err
		}
		return result, nil
	}

	if len(requirements.Blockers) > 0 {
		return finish()
	}
	if requirements.BlockTimestamp >= request.Deadline {
		return empty, errors.New("swap deadline is not future at selected block")
	}

	data, err := hexutil.Decode(built.Prepared.Tx.Data)
	if err != nil {
		return empty, err
	}
	target := common.HexToAddress(built.Prepared.Tx.To)
	msg := ethereum.CallMsg{
		From:  common.HexToAddress(built.Prepared.Tx.From),
		To:    &target,
		Value: big.NewInt(0),
		Data:  data,
	}

	block := new(big.Int).SetUint64(
		*requirements.Provenance.BlockNumber,
	)

	raw, err := s.client.CallContract(readCtx, msg, block)
	if err != nil {
		if name, reverted := s.decodeSwapRevert(err); reverted {
			result.Status = "reverted"
			result.RevertName = &name
			return finish()
		}
		return empty, fmt.Errorf("simulate swap: %w", err)
	}

	if len(raw) != 32 {
		return empty, errors.New("invalid simulated swap output length")
	}

	method := s.builder.abi.Methods["executeSwap"]
	values, err := method.Outputs.Unpack(raw)
	if err != nil {
		return empty, fmt.Errorf("decode simulated swap output: %w", err)
	}
	if len(values) != 1 {
		return empty, errors.New("invalid simulated swap output")
	}

	output, ok := values[0].(*big.Int)
	minimum, parseErr := ParseRawAmount(built.Summary.MinAmountOut.Raw)
	if !ok || output == nil || output.Sign() <= 0 ||
		output.BitLen() > 256 || parseErr != nil ||
		output.Cmp(minimum) < 0 {
		return empty, errors.New("simulated output is below the intended minimum")
	}

	// EstimateGas uses the node's current state, not the pinned eth_call block.
	gas, err := s.client.EstimateGas(readCtx, msg)
	if err != nil {
		if name, reverted := s.decodeSwapRevert(err); reverted {
			result.Status = "reverted"
			result.RevertName = &name
			return finish()
		}
		return empty, fmt.Errorf("estimate swap gas: %w", err)
	}
	if gas == 0 {
		return empty, errors.New("RPC returned zero gas estimate")
	}

	// Add a 20% gas-limit buffer, rounding upward.
	buffered := new(big.Int).Mul(
		new(big.Int).SetUint64(gas), big.NewInt(120),
	)
	buffered.Add(buffered, big.NewInt(99))
	buffered.Quo(buffered, big.NewInt(100))
	if !buffered.IsUint64() {
		return empty, errors.New("buffered gas limit overflows uint64")
	}
	gasLimit := buffered.Uint64()

	gasPrice, err := s.client.SuggestGasPrice(readCtx)
	if err != nil {
		return empty, fmt.Errorf("read suggested gas price: %w", err)
	}
	if gasPrice == nil || gasPrice.Sign() <= 0 ||
		gasPrice.BitLen() > 256 {
		return empty, errors.New("invalid suggested gas price")
	}

	fee := new(big.Int).Mul(
		new(big.Int).SetUint64(gasLimit), gasPrice,
	)
	nativeBalance, err := ParseRawAmount(requirements.NativeBalanceWei)
	if err != nil {
		return empty, err
	}
	affordable := nativeBalance.Cmp(fee) >= 0

	outputRaw := output.String()
	gasPriceRaw := gasPrice.String()
	feeRaw := fee.String()

	result.SimulatedAmountOutRaw = &outputRaw
	result.GasEstimate = &gas
	result.GasLimit = &gasLimit
	result.GasPriceWei = &gasPriceRaw
	result.EstimatedFeeWei = &feeRaw
	result.Requirements.GasAffordable = &affordable

	if affordable {
		result.Status = "succeeded"
	} else {
		result.Status = "blocked"
		result.Requirements.Blockers = append(
			result.Requirements.Blockers,
			"INSUFFICIENT_GAS_BALANCE",
		)
	}

	result.Provenance = Provenance{
		Source:     "Fuji RPC swap simulation",
		SourceType: "rpc",
		ChainID:    uint64(FujiChainID),
		ObservedAt: time.Now().UTC(),
		Mode:       "live",
		IsDemo:     false,
	}
	// No single block is claimed for the combined simulation/gas estimate.
	// The requirements retain their own pinned block provenance.
	return finish()
}

func (s *SwapSimulator) decodeSwapRevert(err error) (string, bool) {
	if errors.Is(err, context.Canceled) ||
		errors.Is(err, context.DeadlineExceeded) {
		return "", false
	}

	// Require the RPC execution-reverted code and hex revert data.
	var rpcErr rpc.Error
	var dataErr rpc.DataError
	if !errors.As(err, &rpcErr) || rpcErr.ErrorCode() != 3 ||
		!errors.As(err, &dataErr) {
		return "", false
	}

	encoded, ok := dataErr.ErrorData().(string)
	if !ok {
		return "", false
	}
	raw, decodeErr := hexutil.Decode(encoded)
	if decodeErr != nil {
		return "", false
	}

	if reason, unpackErr := abi.UnpackRevert(raw); unpackErr == nil {
		return reason, true
	}

	if len(raw) >= 4 {
		for _, definition := range s.builder.abi.Errors {
			if bytes.Equal(raw[:4], definition.ID.Bytes()[:4]) {
				if _, unpackErr := definition.Inputs.Unpack(raw[4:]); unpackErr == nil {
					return definition.Name, true
				}
			}
		}
	}

	// A downstream custom error may not exist in the manager ABI.
	return "UnknownRevert", true
}
