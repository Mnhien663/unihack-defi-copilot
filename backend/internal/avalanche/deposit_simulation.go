package avalanche

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/rpc"
)

type DepositSimulationResult struct {
	Transaction  BuiltDepositTransaction `json:"transaction"`
	Requirements DepositRequirements     `json:"requirements"`
	Status       string                  `json:"status"`

	SimulatedSharesRaw *string    `json:"simulated_shares_raw"`
	GasEstimate        *uint64    `json:"gas_estimate"`
	GasLimit           *uint64    `json:"gas_limit"`
	GasPriceWei        *string    `json:"gas_price_wei"`
	EstimatedFeeWei    *string    `json:"estimated_fee_wei"`
	RevertName         *string    `json:"revert_name"`
	Provenance         Provenance `json:"provenance"`
}

type DepositSimulator struct {
	client       SwapSimulationRPC
	builder      *DepositTxBuilder
	requirements *DepositRequirementsReader
}

func NewDepositSimulator(
	client SwapSimulationRPC,
) (*DepositSimulator, error) {
	if client == nil {
		return nil, errors.New("deposit simulation RPC client is nil")
	}
	builder, err := NewDepositTxBuilder()
	if err != nil {
		return nil, err
	}
	requirements, err := NewDepositRequirementsReader(client)
	if err != nil {
		return nil, err
	}
	return &DepositSimulator{
		client:       client,
		builder:      builder,
		requirements: requirements,
	}, nil
}

func (s *DepositSimulator) SimulateDeposit(
	ctx context.Context,
	request BuildDepositRequest,
) (DepositSimulationResult, error) {
	var empty DepositSimulationResult
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	readCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	built, err := s.builder.BuildDepositTx(request, time.Now().UTC())
	if err != nil {
		return empty, err
	}
	requirements, err := s.requirements.ReadDepositRequirements(
		readCtx, request,
	)
	if err != nil {
		return empty, err
	}

	result := DepositSimulationResult{
		Transaction:  built,
		Requirements: requirements,
		Status:       "blocked",
		Provenance:   requirements.Provenance,
	}
	finish := func() (DepositSimulationResult, error) {
		if err := readCtx.Err(); err != nil {
			return empty, err
		}
		if _, err := s.builder.BuildDepositTx(
			request, time.Now().UTC(),
		); err != nil {
			return empty, err
		}
		return result, nil
	}
	if len(requirements.Blockers) > 0 {
		return finish()
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
		if name, reverted := decodeDepositRevert(err, s.builder.abi); reverted {
			result.Status = "reverted"
			result.RevertName = &name
			return finish()
		}
		return empty, fmt.Errorf("simulate deposit: %w", err)
	}
	if len(raw) != 32 {
		return empty, errors.New("invalid simulated deposit output length")
	}
	values, err := s.builder.abi.Methods["executeDeposit"].Outputs.Unpack(raw)
	if err != nil {
		return empty, err
	}
	if len(values) != 1 {
		return empty, errors.New("invalid simulated deposit output")
	}
	shares, ok := values[0].(*big.Int)
	minimum, err := ParseRawAmount(built.Summary.MinShares.Raw)
	if err != nil {
		return empty, err
	}
	if !ok || shares == nil || shares.Sign() <= 0 ||
		shares.BitLen() > 256 || shares.Cmp(minimum) < 0 {
		return empty, errors.New("simulated shares below intended minimum")
	}

	// Gas estimation uses current node state, not the pinned call block.
	gas, err := s.client.EstimateGas(readCtx, msg)
	if err != nil {
		if name, reverted := decodeDepositRevert(err, s.builder.abi); reverted {
			result.Status = "reverted"
			result.RevertName = &name
			return finish()
		}
		return empty, fmt.Errorf("estimate deposit gas: %w", err)
	}
	if gas == 0 {
		return empty, errors.New("zero deposit gas estimate")
	}

	buffered := new(big.Int).Mul(
		new(big.Int).SetUint64(gas), big.NewInt(120),
	)
	buffered.Add(buffered, big.NewInt(99))
	buffered.Quo(buffered, big.NewInt(100))
	if !buffered.IsUint64() {
		return empty, errors.New("deposit gas limit overflows uint64")
	}
	gasLimit := buffered.Uint64()

	gasPrice, err := s.client.SuggestGasPrice(readCtx)
	if err != nil {
		return empty, err
	}
	if gasPrice == nil || gasPrice.Sign() <= 0 ||
		gasPrice.BitLen() > 256 {
		return empty, errors.New("invalid deposit gas price")
	}
	fee := new(big.Int).Mul(
		new(big.Int).SetUint64(gasLimit), gasPrice,
	)
	native, err := ParseRawAmount(requirements.NativeBalanceWei)
	if err != nil {
		return empty, err
	}
	affordable := native.Cmp(fee) >= 0
	result.Requirements.GasAffordable = &affordable

	sharesRaw := shares.String()
	priceRaw := gasPrice.String()
	feeRaw := fee.String()
	result.SimulatedSharesRaw = &sharesRaw
	result.GasEstimate = &gas
	result.GasLimit = &gasLimit
	result.GasPriceWei = &priceRaw
	result.EstimatedFeeWei = &feeRaw

	if affordable {
		result.Status = "succeeded"
	} else {
		result.Requirements.Blockers = append(
			result.Requirements.Blockers,
			"INSUFFICIENT_GAS_BALANCE",
		)
	}

	// Combined simulation and current-state gas estimate have no single block.
	result.Provenance = Provenance{
		Source:     "Fuji RPC DemoVault deposit simulation",
		SourceType: "rpc",
		ChainID:    uint64(FujiChainID),
		ObservedAt: time.Now().UTC(),
		Mode:       "live",
		IsDemo:     true,
	}
	return finish()
}

func decodeDepositRevert(
	err error,
	definition abi.ABI,
) (string, bool) {
	if errors.Is(err, context.Canceled) ||
		errors.Is(err, context.DeadlineExceeded) {
		return "", false
	}
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
		for _, item := range definition.Errors {
			if bytes.Equal(raw[:4], item.ID.Bytes()[:4]) {
				if _, unpackErr := item.Inputs.Unpack(raw[4:]); unpackErr == nil {
					return item.Name, true
				}
			}
		}
	}
	return "UnknownRevert", true
}
