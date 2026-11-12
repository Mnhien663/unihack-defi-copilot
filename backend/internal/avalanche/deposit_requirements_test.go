package avalanche

import (
	"context"
	"errors"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

type depositRequirementsStub struct {
	RequirementsRPCClient

	chain        int64
	balance      *big.Int
	allowance    *big.Int
	budget       *big.Int
	preview      *big.Int
	maximum      *big.Int
	adapter      common.Address
	shareToken   common.Address
	inputEnabled bool
	shareEnabled bool
	wrongBinding bool
	rpcErr       error
	reorg        bool
	headerCalls  int
	header       *types.Header
	abis         []abi.ABI
}

func newDepositRequirementsStub(t *testing.T) *depositRequirementsStub {
	t.Helper()
	builder, err := NewDepositTxBuilder()
	if err != nil {
		t.Fatal(err)
	}

	definitions := []string{
		vaultReadABIJSON,
		`[
			{"type":"function","name":"balanceOf","inputs":[{"type":"address"}],"outputs":[{"type":"uint256"}]},
			{"type":"function","name":"allowance","inputs":[{"type":"address"},{"type":"address"}],"outputs":[{"type":"uint256"}]},
			{"type":"function","name":"manager","inputs":[],"outputs":[{"type":"address"}]},
			{"type":"function","name":"underlying","inputs":[],"outputs":[{"type":"address"}]},
			{"type":"function","name":"vault","inputs":[],"outputs":[{"type":"address"}]}
		]`,
	}

	stub := &depositRequirementsStub{
		chain:        FujiChainID,
		balance:      big.NewInt(1000),
		allowance:    big.NewInt(1000),
		budget:       big.NewInt(1000),
		preview:      big.NewInt(900),
		maximum:      big.NewInt(1000),
		adapter:      common.HexToAddress(fujiVaultExecutionAdapter),
		shareToken:   common.HexToAddress(demoFujiVault),
		inputEnabled: true,
		shareEnabled: true,
		header: &types.Header{
			Number: big.NewInt(123),
			Time:   uint64(time.Now().Add(-time.Second).Unix()),
		},
		abis: []abi.ABI{builder.abi},
	}
	for _, definition := range definitions {
		parsed, err := abi.JSON(strings.NewReader(definition))
		if err != nil {
			t.Fatal(err)
		}
		stub.abis = append(stub.abis, parsed)
	}
	return stub
}

func (s *depositRequirementsStub) ChainID(
	ctx context.Context,
) (*big.Int, error) {
	return big.NewInt(s.chain), nil
}

func (s *depositRequirementsStub) BlockNumber(
	ctx context.Context,
) (uint64, error) {
	return 123, nil
}

func (s *depositRequirementsStub) HeaderByNumber(
	ctx context.Context,
	block *big.Int,
) (*types.Header, error) {
	if block == nil || block.Uint64() != 123 {
		return nil, errors.New("header must use pinned block")
	}
	s.headerCalls++
	header := *s.header
	if s.reorg && s.headerCalls > 1 {
		header.Extra = []byte{1}
	}
	return &header, nil
}

func (s *depositRequirementsStub) BalanceAt(
	ctx context.Context,
	account common.Address,
	block *big.Int,
) (*big.Int, error) {
	if block == nil || block.Uint64() != 123 {
		return nil, errors.New("balance must use pinned block")
	}
	return big.NewInt(1000000000000000000), nil
}

func (s *depositRequirementsStub) CallContract(
	ctx context.Context,
	msg ethereum.CallMsg,
	block *big.Int,
) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.rpcErr != nil {
		return nil, s.rpcErr
	}
	if block == nil || block.Uint64() != 123 {
		return nil, errors.New("contract call must use pinned block")
	}
	if msg.To == nil || len(msg.Data) < 4 {
		return nil, errors.New("invalid call")
	}

	var method *abi.Method
	for _, definition := range s.abis {
		found, err := definition.MethodById(msg.Data[:4])
		if err == nil {
			method = found
			break
		}
	}
	if method == nil {
		return nil, errors.New("unexpected method")
	}
	args, err := method.Inputs.Unpack(msg.Data[4:])
	if err != nil {
		return nil, err
	}

	manager := common.HexToAddress(fujiExecutionManager)
	asset := common.HexToAddress(demoFujiAsset)
	vault := common.HexToAddress(demoFujiVault)
	adapter := common.HexToAddress(fujiVaultExecutionAdapter)

	var target common.Address
	var values []interface{}
	switch method.Name {
	case "balanceOf":
		target, values = asset, []interface{}{s.balance}
	case "allowance":
		target = asset
		if args[1].(common.Address) != manager {
			return nil, errors.New("wrong allowance spender")
		}
		values = []interface{}{s.allowance}
	case "remaining":
		target, values = manager, []interface{}{s.budget}
	case "supportedTokens":
		target = manager
		token := args[0].(common.Address)
		switch token {
		case asset:
			values = []interface{}{s.inputEnabled}
		case vault:
			values = []interface{}{s.shareEnabled}
		default:
			return nil, errors.New("unexpected supported token")
		}
	case "vaultConfigs":
		target, values = manager, []interface{}{s.adapter, s.shareToken}
	case "asset":
		target, values = vault, []interface{}{asset}
	case "previewDeposit":
		target, values = vault, []interface{}{s.preview}
	case "maxDeposit":
		target, values = vault, []interface{}{s.maximum}
	case "manager":
		target = adapter
		if s.wrongBinding {
			values = []interface{}{asset}
		} else {
			values = []interface{}{manager}
		}
	case "underlying":
		target, values = adapter, []interface{}{asset}
	case "vault":
		target, values = adapter, []interface{}{vault}
	default:
		return nil, errors.New("unexpected method name")
	}
	if *msg.To != target {
		return nil, errors.New("wrong contract target")
	}
	return method.Outputs.Pack(values...)
}

func liveDepositRequirementsRequest() BuildDepositRequest {
	request, _ := depositBuilderFixture()
	now := time.Now().UTC()
	blockTime := now.Add(-time.Second)
	request.Metrics.Position.Provenance.ObservedAt = now
	request.Metrics.BlockTime = &blockTime
	request.Metrics.DepositAssets.Raw = "1000"
	request.Metrics.PreviewShares.Raw = "900"
	request.Metrics.MaxDeposit.Raw = "1000"
	request.MinSharesRaw = "891"
	request.Deadline = uint64(now.Add(50 * time.Second).Unix())
	return request
}

func TestDepositRequirementsReady(t *testing.T) {
	stub := newDepositRequirementsStub(t)
	reader, err := NewDepositRequirementsReader(stub)
	if err != nil {
		t.Fatal(err)
	}
	result, err := reader.ReadDepositRequirements(
		context.Background(), liveDepositRequirementsRequest(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Blockers) != 0 || !result.RouteVerified ||
		!result.HasTokenBalance || !result.HasERC20Approval ||
		!result.HasBudget || !result.WithinMaxDeposit ||
		!result.MeetsMinimumShares {
		t.Fatalf("unexpected requirements: %+v", result)
	}
	if result.GasAffordable != nil {
		t.Fatal("gas affordability must remain unknown before simulation")
	}
	if !result.Provenance.IsDemo ||
		result.Provenance.BlockNumber == nil ||
		*result.Provenance.BlockNumber != 123 {
		t.Fatal("wrong requirements provenance")
	}
	if stub.headerCalls != 2 {
		t.Fatal("pinned block must be rechecked")
	}
}

func TestDepositRequirementsBlockers(t *testing.T) {
	tests := []struct {
		name   string
		code   string
		mutate func(*depositRequirementsStub)
	}{
		{"balance", "INSUFFICIENT_TOKEN_BALANCE", func(s *depositRequirementsStub) {
			s.balance = big.NewInt(999)
		}},
		{"approval", "ERC20_APPROVAL_REQUIRED", func(s *depositRequirementsStub) {
			s.allowance = big.NewInt(999)
		}},
		{"budget", "INSUFFICIENT_BUDGET", func(s *depositRequirementsStub) {
			s.budget = big.NewInt(999)
		}},
		{"max deposit", "MAX_DEPOSIT_EXCEEDED", func(s *depositRequirementsStub) {
			s.maximum = big.NewInt(999)
		}},
		{"changed preview", "MIN_SHARES_NOT_MET", func(s *depositRequirementsStub) {
			s.preview = big.NewInt(890)
		}},
		{"zero shares", "MIN_SHARES_NOT_MET", func(s *depositRequirementsStub) {
			s.preview = big.NewInt(0)
		}},
		{"input disabled", "INPUT_TOKEN_DISABLED", func(s *depositRequirementsStub) {
			s.inputEnabled = false
		}},
		{"shares disabled", "SHARE_TOKEN_DISABLED", func(s *depositRequirementsStub) {
			s.shareEnabled = false
		}},
		{"missing route", "VAULT_ROUTE_NOT_CONFIGURED", func(s *depositRequirementsStub) {
			s.adapter = common.Address{}
		}},
		{"wrong share token", "VAULT_ROUTE_NOT_VERIFIED", func(s *depositRequirementsStub) {
			s.shareToken = common.HexToAddress(demoFujiAsset)
		}},
		{"wrong binding", "VAULT_ROUTE_NOT_VERIFIED", func(s *depositRequirementsStub) {
			s.wrongBinding = true
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			stub := newDepositRequirementsStub(t)
			test.mutate(stub)
			reader, err := NewDepositRequirementsReader(stub)
			if err != nil {
				t.Fatal(err)
			}
			result, err := reader.ReadDepositRequirements(
				context.Background(), liveDepositRequirementsRequest(),
			)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, code := range result.Blockers {
				if code == test.code {
					found = true
				}
			}
			if !found {
				t.Fatalf("missing %s: %v", test.code, result.Blockers)
			}
		})
	}
}

func TestDepositRequirementsErrors(t *testing.T) {
	for _, name := range []string{"wrong chain", "RPC error", "reorg", "canceled"} {
		t.Run(name, func(t *testing.T) {
			stub := newDepositRequirementsStub(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch name {
			case "wrong chain":
				stub.chain = 43114
			case "RPC error":
				stub.rpcErr = errors.New("upstream unavailable")
			case "reorg":
				stub.reorg = true
			case "canceled":
				cancel()
			}
			reader, err := NewDepositRequirementsReader(stub)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := reader.ReadDepositRequirements(
				ctx, liveDepositRequirementsRequest(),
			); err == nil {
				t.Fatal("expected error, not a zero balance result")
			}
		})
	}
}
