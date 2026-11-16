package avalanche

import (
	"bytes"
	"context"
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

type depositSimulationStub struct {
	*depositRequirementsStub

	output          *big.Int
	gas             uint64
	price           *big.Int
	native          *big.Int
	executionErr    error
	estimateErr     error
	malformedOutput bool
	executionCalls  int
	estimateCalls   int
	executionData   []byte
}

func newDepositSimulationStub(t *testing.T) *depositSimulationStub {
	t.Helper()
	return &depositSimulationStub{
		depositRequirementsStub: newDepositRequirementsStub(t),
		output:                  big.NewInt(900),
		gas:                     100000,
		price:                   big.NewInt(1000000000),
		native:                  big.NewInt(1000000000000000000),
	}
}

func (s *depositSimulationStub) BalanceAt(
	ctx context.Context,
	account common.Address,
	block *big.Int,
) (*big.Int, error) {
	if block == nil || block.Uint64() != 123 {
		return nil, errors.New("native balance must use pinned block")
	}
	return s.native, nil
}

func (s *depositSimulationStub) checkExecution(
	msg ethereum.CallMsg,
) error {
	builder, err := NewDepositTxBuilder()
	if err != nil {
		return err
	}
	if msg.To == nil ||
		*msg.To != common.HexToAddress(fujiExecutionManager) ||
		msg.From != common.HexToAddress(
			"0x37E146a4141C8fd82f0c8c5Bb8035930fD0ea588",
		) ||
		msg.Value == nil || msg.Value.Sign() != 0 {
		return errors.New("wrong deposit transaction fields")
	}
	method := builder.abi.Methods["executeDeposit"]
	if len(msg.Data) != 132 ||
		!bytes.Equal(msg.Data[:4], method.ID) {
		return errors.New("wrong deposit calldata")
	}
	values, err := method.Inputs.Unpack(msg.Data[4:])
	if err != nil {
		return err
	}
	if values[0].(common.Address) != common.HexToAddress(demoFujiAsset) ||
		values[1].(*big.Int).Cmp(big.NewInt(1000)) != 0 ||
		values[2].(*big.Int).Cmp(big.NewInt(891)) != 0 {
		return errors.New("wrong deposit arguments")
	}
	return nil
}

func (s *depositSimulationStub) CallContract(
	ctx context.Context,
	msg ethereum.CallMsg,
	block *big.Int,
) ([]byte, error) {
	builder, err := NewDepositTxBuilder()
	if err != nil {
		return nil, err
	}
	method := builder.abi.Methods["executeDeposit"]
	if len(msg.Data) >= 4 && bytes.Equal(msg.Data[:4], method.ID) {
		s.executionCalls++
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if block == nil || block.Uint64() != 123 {
			return nil, errors.New("simulation must use pinned block")
		}
		if err := s.checkExecution(msg); err != nil {
			return nil, err
		}
		s.executionData = append([]byte(nil), msg.Data...)
		if s.executionErr != nil {
			return nil, s.executionErr
		}
		if s.malformedOutput {
			return []byte{1}, nil
		}
		return method.Outputs.Pack(s.output)
	}
	return s.depositRequirementsStub.CallContract(ctx, msg, block)
}

func (s *depositSimulationStub) EstimateGas(
	ctx context.Context,
	msg ethereum.CallMsg,
) (uint64, error) {
	s.estimateCalls++
	if err := s.checkExecution(msg); err != nil {
		return 0, err
	}
	if !bytes.Equal(msg.Data, s.executionData) {
		return 0, errors.New("gas estimate calldata differs from simulation")
	}
	return s.gas, s.estimateErr
}

func (s *depositSimulationStub) SuggestGasPrice(
	ctx context.Context,
) (*big.Int, error) {
	return s.price, nil
}

type depositRPCRevert struct {
	data string
}

func (e depositRPCRevert) Error() string {
	return "execution reverted"
}
func (e depositRPCRevert) ErrorCode() int {
	return 3
}
func (e depositRPCRevert) ErrorData() interface{} {
	return e.data
}

func depositBudgetRevert(t *testing.T) error {
	t.Helper()
	builder, err := NewDepositTxBuilder()
	if err != nil {
		t.Fatal(err)
	}
	definition, ok := builder.abi.Errors["BudgetExceeded"]
	if !ok {
		t.Fatal("manager ABI missing BudgetExceeded")
	}
	args, err := definition.Inputs.Pack(
		big.NewInt(999), big.NewInt(1000),
	)
	if err != nil {
		t.Fatal(err)
	}
	data := append([]byte(nil), definition.ID.Bytes()[:4]...)
	data = append(data, args...)
	return depositRPCRevert{data: hexutil.Encode(data)}
}

func TestDepositSimulationSuccess(t *testing.T) {
	stub := newDepositSimulationStub(t)
	simulator, err := NewDepositSimulator(stub)
	if err != nil {
		t.Fatal(err)
	}
	result, err := simulator.SimulateDeposit(
		context.Background(), liveDepositRequirementsRequest(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "succeeded" ||
		result.SimulatedSharesRaw == nil ||
		*result.SimulatedSharesRaw != "900" {
		t.Fatalf("unexpected simulation: %+v", result)
	}
	if result.GasEstimate == nil || *result.GasEstimate != 100000 ||
		result.GasLimit == nil || *result.GasLimit != 120000 ||
		result.EstimatedFeeWei == nil ||
		*result.EstimatedFeeWei != "120000000000000" {
		t.Fatal("wrong gas estimate or fee")
	}
	if result.Requirements.GasAffordable == nil ||
		!*result.Requirements.GasAffordable {
		t.Fatal("expected affordable gas")
	}
	if !result.Provenance.IsDemo ||
		result.Provenance.BlockNumber != nil {
		t.Fatal("wrong combined simulation provenance")
	}
	if stub.executionCalls != 1 || stub.estimateCalls != 1 {
		t.Fatal("unexpected execution or estimation count")
	}
}

func TestDepositSimulationBlockedBeforeExecution(t *testing.T) {
	stub := newDepositSimulationStub(t)
	stub.allowance = big.NewInt(0)
	stub.budget = big.NewInt(0)
	simulator, err := NewDepositSimulator(stub)
	if err != nil {
		t.Fatal(err)
	}
	result, err := simulator.SimulateDeposit(
		context.Background(), liveDepositRequirementsRequest(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "blocked" ||
		len(result.Requirements.Blockers) != 2 {
		t.Fatalf("unexpected blockers: %+v", result)
	}
	if stub.executionCalls != 0 || stub.estimateCalls != 0 ||
		result.SimulatedSharesRaw != nil {
		t.Fatal("blocked deposit must not reach execution simulation")
	}
}

func TestDepositSimulationInsufficientGas(t *testing.T) {
	stub := newDepositSimulationStub(t)
	stub.native = big.NewInt(1)
	simulator, err := NewDepositSimulator(stub)
	if err != nil {
		t.Fatal(err)
	}
	result, err := simulator.SimulateDeposit(
		context.Background(), liveDepositRequirementsRequest(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "blocked" ||
		result.Requirements.GasAffordable == nil ||
		*result.Requirements.GasAffordable {
		t.Fatal("insufficient gas must block")
	}
	found := false
	for _, code := range result.Requirements.Blockers {
		if code == "INSUFFICIENT_GAS_BALANCE" {
			found = true
		}
	}
	if !found {
		t.Fatal("missing gas blocker")
	}
}

func TestDepositSimulationReverts(t *testing.T) {
	for _, stage := range []string{"call", "estimate"} {
		t.Run(stage, func(t *testing.T) {
			stub := newDepositSimulationStub(t)
			if stage == "call" {
				stub.executionErr = depositBudgetRevert(t)
			} else {
				stub.estimateErr = depositBudgetRevert(t)
			}
			simulator, err := NewDepositSimulator(stub)
			if err != nil {
				t.Fatal(err)
			}
			result, err := simulator.SimulateDeposit(
				context.Background(), liveDepositRequirementsRequest(),
			)
			if err != nil {
				t.Fatal(err)
			}
			if result.Status != "reverted" ||
				result.RevertName == nil ||
				*result.RevertName != "BudgetExceeded" {
				t.Fatalf("unexpected revert: %+v", result)
			}
		})
	}
}

func TestDepositSimulationErrors(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*depositSimulationStub)
	}{
		{"RPC unavailable", func(s *depositSimulationStub) {
			s.executionErr = errors.New("upstream unavailable")
		}},
		{"RPC timeout", func(s *depositSimulationStub) {
			s.executionErr = context.DeadlineExceeded
		}},
		{"estimate unavailable", func(s *depositSimulationStub) {
			s.estimateErr = errors.New("estimate unavailable")
		}},
		{"malformed output", func(s *depositSimulationStub) {
			s.malformedOutput = true
		}},
		{"below minimum", func(s *depositSimulationStub) {
			s.output = big.NewInt(890)
		}},
		{"zero shares", func(s *depositSimulationStub) {
			s.output = big.NewInt(0)
		}},
		{"zero gas", func(s *depositSimulationStub) {
			s.gas = 0
		}},
		{"gas overflow", func(s *depositSimulationStub) {
			s.gas = ^uint64(0)
		}},
		{"invalid gas price", func(s *depositSimulationStub) {
			s.price = big.NewInt(0)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			stub := newDepositSimulationStub(t)
			test.mutate(stub)
			simulator, err := NewDepositSimulator(stub)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := simulator.SimulateDeposit(
				context.Background(), liveDepositRequirementsRequest(),
			); err == nil {
				t.Fatal("expected error, not successful simulation")
			}
		})
	}
}
