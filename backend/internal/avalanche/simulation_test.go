package avalanche

import (
	"bytes"
	"context"
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

type simulationRPCStub struct {
	*requirementsRPCStub
	expectedTx    UnsignedTx
	simulateCalls int
	estimateCalls int
	gasPriceCalls int
	simulationErr error
	estimateErr   error
	simulationRaw []byte
}

func (s *simulationRPCStub) checkExecutionCall(
	msg ethereum.CallMsg,
) error {
	data, err := hexutil.Decode(s.expectedTx.Data)
	if err != nil {
		return err
	}
	if msg.From != common.HexToAddress(s.expectedTx.From) ||
		msg.To == nil ||
		*msg.To != common.HexToAddress(s.expectedTx.To) ||
		msg.Value == nil || msg.Value.Sign() != 0 ||
		!bytes.Equal(msg.Data, data) {
		return errors.New("execution call differs from unsigned intent")
	}
	return nil
}

func (s *simulationRPCStub) CallContract(
	ctx context.Context,
	msg ethereum.CallMsg,
	block *big.Int,
) ([]byte, error) {
	method := s.reader.builder.abi.Methods["executeSwap"]

	if len(msg.Data) >= 4 &&
		bytes.Equal(msg.Data[:4], method.ID) {
		s.simulateCalls++

		if s.simulationErr != nil {
			return nil, s.simulationErr
		}
		if err := s.checkExecutionCall(msg); err != nil {
			return nil, err
		}
		if block == nil || block.Cmp(big.NewInt(12345)) != 0 {
			return nil, errors.New("simulation must use requirements block")
		}
		if s.simulationRaw != nil {
			return s.simulationRaw, nil
		}
		return method.Outputs.Pack(big.NewInt(999900))
	}

	return s.requirementsRPCStub.CallContract(ctx, msg, block)
}

func (s *simulationRPCStub) EstimateGas(
	ctx context.Context,
	msg ethereum.CallMsg,
) (uint64, error) {
	s.estimateCalls++
	if s.estimateErr != nil {
		return 0, s.estimateErr
	}
	if err := s.checkExecutionCall(msg); err != nil {
		return 0, err
	}
	return 100_000, nil
}

func (s *simulationRPCStub) SuggestGasPrice(
	context.Context,
) (*big.Int, error) {
	s.gasPriceCalls++
	return big.NewInt(100), nil
}

func newSimulationTestReader(
	t *testing.T,
) (*SwapSimulator, *simulationRPCStub, BuildSwapRequest) {
	t.Helper()

	_, requirementsClient, request := newRequirementsTestReader(t)

	client := &simulationRPCStub{
		requirementsRPCStub: requirementsClient,
	}

	simulator, err := NewSwapSimulator(client)
	if err != nil {
		t.Fatal(err)
	}
	requirementsClient.reader = simulator.requirements

	built, err := simulator.builder.BuildSwapTx(
		request, time.Now().UTC(),
	)
	if err != nil {
		t.Fatal(err)
	}
	client.expectedTx = built.Prepared.Tx

	return simulator, client, request
}

func TestSwapSimulationStopsAtRequirementsBlocker(t *testing.T) {
	simulator, client, request := newSimulationTestReader(t)

	// The mock requirements have no configured route.
	result, err := simulator.SimulateSwap(
		context.Background(), request,
	)
	if err != nil {
		t.Fatal(err)
	}

	if result.Status != "blocked" ||
		len(result.Requirements.Blockers) != 1 ||
		result.Requirements.Blockers[0] != "SWAP_ROUTE_NOT_CONFIGURED" {
		t.Fatalf("unexpected blocked result: %+v", result)
	}
	if client.simulateCalls != 0 ||
		client.estimateCalls != 0 ||
		client.gasPriceCalls != 0 {
		t.Fatal("blocked requirements must not trigger execution or gas calls")
	}
	if result.Requirements.GasAffordable != nil ||
		result.SimulatedAmountOutRaw != nil ||
		result.GasEstimate != nil {
		t.Fatal("blocked result must not invent simulation or gas values")
	}
}

func TestSwapSimulationUsesOwnerAndMatchesIntent(t *testing.T) {
	simulator, client, request := newSimulationTestReader(t)
	client.adapter = common.HexToAddress(
		fujiLFJSwapAdapter,
	)

	result, err := simulator.SimulateSwap(
		context.Background(), request,
	)
	if err != nil {
		t.Fatal(err)
	}

	if result.Status != "succeeded" {
		t.Fatalf("unexpected simulation status: %s", result.Status)
	}
	if client.simulateCalls != 1 ||
		client.estimateCalls != 1 ||
		client.gasPriceCalls != 1 {
		t.Fatal("expected one simulation, gas estimate and gas-price read")
	}

	if result.SimulatedAmountOutRaw == nil ||
		*result.SimulatedAmountOutRaw != "999900" ||
		result.GasEstimate == nil || *result.GasEstimate != 100000 ||
		result.GasLimit == nil || *result.GasLimit != 120000 ||
		result.GasPriceWei == nil || *result.GasPriceWei != "100" ||
		result.EstimatedFeeWei == nil ||
		*result.EstimatedFeeWei != "12000000" {
		t.Fatalf("unexpected simulation or gas values: %+v", result)
	}

	if result.Requirements.GasAffordable == nil ||
		!*result.Requirements.GasAffordable {
		t.Fatal("expected sufficient AVAX under the estimated fee")
	}
	if result.Transaction.Prepared.Tx != client.expectedTx {
		t.Fatal("returned transaction differs from simulated intent")
	}
	if result.Provenance.BlockNumber != nil {
		t.Fatal("combined current gas estimate must not claim a single block")
	}
}

// Implements the RPC error interfaces without making a network call.
type simulationRevertError struct {
	data string
}

func (e simulationRevertError) Error() string {
	return "execution reverted"
}

func (e simulationRevertError) ErrorCode() int {
	return 3
}

func (e simulationRevertError) ErrorData() interface{} {
	return e.data
}

func TestSwapSimulationBlocksInsufficientGasBalance(t *testing.T) {
	simulator, client, request := newSimulationTestReader(t)
	client.adapter = common.HexToAddress(
		fujiLFJSwapAdapter,
	)
	client.native = big.NewInt(11_999_999)

	result, err := simulator.SimulateSwap(
		context.Background(), request,
	)
	if err != nil {
		t.Fatal(err)
	}

	if result.Status != "blocked" ||
		result.Requirements.GasAffordable == nil ||
		*result.Requirements.GasAffordable {
		t.Fatalf("unexpected gas affordability result: %+v", result)
	}
	if len(result.Requirements.Blockers) != 1 ||
		result.Requirements.Blockers[0] != "INSUFFICIENT_GAS_BALANCE" {
		t.Fatalf("unexpected blockers: %v", result.Requirements.Blockers)
	}
}

func TestSwapSimulationDecodesManagerRevert(t *testing.T) {
	simulator, client, request := newSimulationTestReader(t)
	client.adapter = common.HexToAddress(
		fujiLFJSwapAdapter,
	)

	definition, ok := simulator.builder.abi.Errors["BudgetExceeded"]
	if !ok {
		t.Fatal("manager ABI is missing BudgetExceeded")
	}
	args, err := definition.Inputs.Pack(
		big.NewInt(999999), big.NewInt(1000000),
	)
	if err != nil {
		t.Fatal(err)
	}
	data := append(
		append([]byte{}, definition.ID.Bytes()[:4]...),
		args...,
	)
	client.simulationErr = simulationRevertError{
		data: hexutil.Encode(data),
	}

	result, err := simulator.SimulateSwap(
		context.Background(), request,
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "reverted" ||
		result.RevertName == nil ||
		*result.RevertName != "BudgetExceeded" {
		t.Fatalf("unexpected decoded revert: %+v", result)
	}
	if client.estimateCalls != 0 || result.GasEstimate != nil {
		t.Fatal("reverted eth_call must stop before gas estimation")
	}
}

func TestSwapSimulationPreservesUpstreamErrors(t *testing.T) {
	tests := []struct {
		name       string
		atEstimate bool
		err        error
	}{
		{"simulation timeout", false, context.DeadlineExceeded},
		{"RPC unavailable", false, errors.New("RPC unavailable")},
		{"gas estimate timeout", true, context.DeadlineExceeded},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			simulator, client, request := newSimulationTestReader(t)
			client.adapter = common.HexToAddress(
				fujiLFJSwapAdapter,
			)

			if tt.atEstimate {
				client.estimateErr = tt.err
			} else {
				client.simulationErr = tt.err
			}

			result, err := simulator.SimulateSwap(
				context.Background(), request,
			)
			if !errors.Is(err, tt.err) {
				t.Fatalf("want preserved error %v, got %v", tt.err, err)
			}
			if result.Status != "" {
				t.Fatal("upstream failure must not become a transaction status")
			}
		})
	}
}

func TestSwapSimulationRejectsInvalidOutput(t *testing.T) {
	tests := []struct {
		name string
		raw  []byte
	}{
		{"empty output", []byte{}},
		{"short output", []byte{0x01}},
		{"zero output", big.NewInt(0).FillBytes(make([]byte, 32))},
		{"below minimum", big.NewInt(994899).FillBytes(make([]byte, 32))},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			simulator, client, request := newSimulationTestReader(t)
			client.adapter = common.HexToAddress(
				fujiLFJSwapAdapter,
			)
			client.simulationRaw = tt.raw

			result, err := simulator.SimulateSwap(
				context.Background(), request,
			)
			if err == nil {
				t.Fatal("expected invalid simulation output to be rejected")
			}
			if result.Status != "" {
				t.Fatal("invalid output must not produce a successful status")
			}
			if client.estimateCalls != 0 {
				t.Fatal("invalid output must stop before gas estimation")
			}
		})
	}
}

func TestSwapSimulationAcceptsExactEstimatedGasBalance(t *testing.T) {
	simulator, client, request := newSimulationTestReader(t)
	client.adapter = common.HexToAddress(
		fujiLFJSwapAdapter,
	)
	client.native = big.NewInt(12_000_000)

	result, err := simulator.SimulateSwap(
		context.Background(), request,
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "succeeded" ||
		result.Requirements.GasAffordable == nil ||
		!*result.Requirements.GasAffordable {
		t.Fatal("balance equal to estimated fee should pass the estimate check")
	}
}

func TestSwapSimulationHonorsCancellation(t *testing.T) {
	simulator, client, request := newSimulationTestReader(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := simulator.SimulateSwap(ctx, request)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want cancellation, got %v", err)
	}
	if client.simulateCalls != 0 || client.estimateCalls != 0 {
		t.Fatal("cancelled request must not execute simulation")
	}
}
