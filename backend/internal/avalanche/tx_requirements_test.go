package avalanche

import (
	"context"
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

type requirementsRPCStub struct {
	reader        *SwapRequirementsReader
	owner         common.Address
	token         common.Address
	headerTime    uint64
	balance       *big.Int
	allowance     *big.Int
	native        *big.Int
	budget        *big.Int
	expiry        *big.Int
	inputEnabled  bool
	outputEnabled bool
	adapter       common.Address
	failMethod    string
	callErr       error
	headerCalls   int
	changeHeader  bool
}

func (s *requirementsRPCStub) ChainID(context.Context) (*big.Int, error) {
	return big.NewInt(FujiChainID), nil
}

func (s *requirementsRPCStub) BlockNumber(context.Context) (uint64, error) {
	return 12345, nil
}

func (s *requirementsRPCStub) HeaderByNumber(
	ctx context.Context,
	block *big.Int,
) (*types.Header, error) {
	if block == nil || block.Cmp(big.NewInt(12345)) != 0 {
		return nil, errors.New("expected pinned header")
	}
	s.headerCalls++
	header := &types.Header{
		Number: big.NewInt(12345),
		Time:   s.headerTime,
	}
	if s.changeHeader && s.headerCalls > 1 {
		header.Time++
	}
	return header, nil
}

func (s *requirementsRPCStub) BalanceAt(
	ctx context.Context,
	account common.Address,
	block *big.Int,
) (*big.Int, error) {
	if account != s.owner ||
		block == nil || block.Cmp(big.NewInt(12345)) != 0 {
		return nil, errors.New("wrong owner or native balance block")
	}
	return new(big.Int).Set(s.native), nil
}

func (s *requirementsRPCStub) CallContract(
	ctx context.Context,
	msg ethereum.CallMsg,
	block *big.Int,
) ([]byte, error) {
	if msg.To == nil || len(msg.Data) < 4 ||
		block == nil || block.Cmp(big.NewInt(12345)) != 0 {
		return nil, errors.New("invalid pinned contract call")
	}

	manager := s.reader.builder.manager

	if *msg.To == s.token {
		switch string(msg.Data[:4]) {
		case string(common.FromHex("0x70a08231")):
			if len(msg.Data) != 36 ||
				common.BytesToAddress(msg.Data[4:36]) != s.owner {
				return nil, errors.New("wrong token balance owner")
			}
			return s.balance.FillBytes(make([]byte, 32)), nil

		case string(common.FromHex("0xdd62ed3e")):
			if len(msg.Data) != 68 ||
				common.BytesToAddress(msg.Data[4:36]) != s.owner ||
				common.BytesToAddress(msg.Data[36:68]) != manager {
				return nil, errors.New("wrong allowance owner or spender")
			}
			return s.allowance.FillBytes(make([]byte, 32)), nil
		}
		return nil, errors.New("unexpected token method")
	}

	if *msg.To == common.HexToAddress(fujiLFJSwapAdapter) {
		method, err := s.reader.adapterABI.MethodById(msg.Data[:4])
		if err != nil {
			return nil, err
		}

		switch method.Name {
		case "manager":
			return method.Outputs.Pack(manager)
		case "router":
			return method.Outputs.Pack(common.HexToAddress(lfjFujiRouter))
		case "tokenX":
			return method.Outputs.Pack(common.HexToAddress(lfjFujiTokenX))
		case "tokenY":
			return method.Outputs.Pack(common.HexToAddress(lfjFujiTokenY))
		case "BIN_STEP":
			return method.Outputs.Pack(big.NewInt(1))
		default:
			return nil, errors.New("unexpected adapter method")
		}
	}

	if *msg.To != manager {
		return nil, errors.New("wrong manager target")
	}

	method, err := s.reader.builder.abi.MethodById(msg.Data[:4])
	if err != nil {
		return nil, err
	}
	if method.Name == s.failMethod {
		return nil, s.callErr
	}
	args, err := method.Inputs.Unpack(msg.Data[4:])
	if err != nil {
		return nil, err
	}

	switch method.Name {
	case "supportedTokens":
		token := args[0].(common.Address)
		if token == s.token {
			return method.Outputs.Pack(s.inputEnabled)
		}
		if token == common.HexToAddress(lfjFujiTokenY) {
			return method.Outputs.Pack(s.outputEnabled)
		}
		return nil, errors.New("unexpected supported token")

	case "swapAdapters":
		if args[0].(common.Address) != s.token ||
			args[1].(common.Address) != common.HexToAddress(lfjFujiTokenY) {
			return nil, errors.New("wrong route tokens")
		}
		return method.Outputs.Pack(s.adapter)

	case "remaining":
		if args[0].(common.Address) != s.owner ||
			args[1].(common.Address) != s.token {
			return nil, errors.New("wrong budget owner or token")
		}

		effective := new(big.Int).Set(s.budget)
		if s.expiry.Cmp(new(big.Int).SetUint64(s.headerTime)) <= 0 {
			effective.SetUint64(0)
		}
		return method.Outputs.Pack(effective)

	default:
		return nil, errors.New("unexpected manager method")
	}
}

func newRequirementsTestReader(
	t *testing.T,
) (*SwapRequirementsReader, *requirementsRPCStub, BuildSwapRequest) {
	t.Helper()

	request, _ := swapBuildTestRequest()
	now := time.Now().UTC()
	request.Quote.Provenance.ObservedAt = now
	request.Quote.ExpiresAt = now.Add(60 * time.Second)
	request.Deadline = uint64(now.Add(50 * time.Second).Unix())

	client := &requirementsRPCStub{
		owner:         common.HexToAddress(request.OwnerAddress),
		token:         common.HexToAddress(lfjFujiTokenX),
		headerTime:    uint64(now.Unix()),
		balance:       big.NewInt(1_000_000),
		allowance:     big.NewInt(1_000_000),
		native:        big.NewInt(1_000_000_000_000_000),
		budget:        big.NewInt(1_000_000),
		expiry:        big.NewInt(now.Add(time.Hour).Unix()),
		inputEnabled:  true,
		outputEnabled: true,
	}

	reader, err := NewSwapRequirementsReader(client)
	if err != nil {
		t.Fatal(err)
	}
	client.reader = reader
	return reader, client, request
}

func TestSwapRequirementsBlocksMissingRoute(t *testing.T) {
	reader, _, request := newRequirementsTestReader(t)

	result, err := reader.ReadSwapRequirements(
		context.Background(), request,
	)
	if err != nil {
		t.Fatal(err)
	}

	if !result.HasTokenBalance || !result.HasERC20Approval ||
		!result.HasBudget ||
		result.BudgetActive == nil || !*result.BudgetActive {
		t.Fatalf("unexpected funding requirements: %+v", result)
	}
	if result.RouteConfigured ||
		result.SwapAdapterAddress != (common.Address{}).Hex() {
		t.Fatal("missing adapter must not become a configured route")
	}
	if len(result.Blockers) != 1 ||
		result.Blockers[0] != "SWAP_ROUTE_NOT_CONFIGURED" {
		t.Fatalf("unexpected blockers: %v", result.Blockers)
	}

	if result.GasAffordable != nil {
		t.Fatal("gas affordability must remain unknown before estimation")
	}
	if result.Provenance.BlockNumber == nil ||
		*result.Provenance.BlockNumber != 12345 {
		t.Fatal("missing pinned block provenance")
	}
}

func TestSwapRequirementsReportsFundingBlockers(t *testing.T) {
	tests := []struct {
		name        string
		change      func(*requirementsRPCStub)
		wantBlocker string
	}{
		{
			name: "insufficient token balance",
			change: func(s *requirementsRPCStub) {
				s.balance = big.NewInt(999_999)
			},
			wantBlocker: "INSUFFICIENT_TOKEN_BALANCE",
		},
		{
			name: "insufficient ERC20 approval",
			change: func(s *requirementsRPCStub) {
				s.allowance = big.NewInt(999_999)
			},
			wantBlocker: "ERC20_APPROVAL_REQUIRED",
		},
		{
			name: "budget below amount",
			change: func(s *requirementsRPCStub) {
				s.budget = big.NewInt(999_999)
			},
			wantBlocker: "INSUFFICIENT_BUDGET",
		},
		{
			name: "budget expires exactly at block time",
			change: func(s *requirementsRPCStub) {
				s.expiry = new(big.Int).SetUint64(s.headerTime)
			},
			wantBlocker: "INSUFFICIENT_BUDGET",
		},
		{
			name: "budget already expired",
			change: func(s *requirementsRPCStub) {
				s.expiry = new(big.Int).SetUint64(s.headerTime - 1)
			},
			wantBlocker: "INSUFFICIENT_BUDGET",
		},
		{
			name: "zero budget",
			change: func(s *requirementsRPCStub) {
				s.budget = big.NewInt(0)
			},
			wantBlocker: "INSUFFICIENT_BUDGET",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader, client, request := newRequirementsTestReader(t)

			// Nonzero address tests the mapping-presence flag only.
			// It does not establish a verified executable adapter.
			client.adapter = common.HexToAddress(
				fujiLFJSwapAdapter,
			)
			tt.change(client)

			result, err := reader.ReadSwapRequirements(
				context.Background(), request,
			)
			if err != nil {
				t.Fatal(err)
			}

			if len(result.Blockers) != 1 ||
				result.Blockers[0] != tt.wantBlocker {
				t.Fatalf("blockers=%v; want [%s]",
					result.Blockers, tt.wantBlocker)
			}

			if result.BudgetExpiry != nil {
				t.Fatal("reader must not invent an unavailable budget expiry")
			}

			if result.BudgetRemainingRaw == "0" &&
				result.BudgetActive != nil {
				t.Fatal("zero remaining budget must have unknown active status")
			}

			if result.GasAffordable != nil {
				t.Fatal("gas affordability requires a fee estimate")
			}
		})
	}
}

func TestSwapRequirementsPreservesAllBlockers(t *testing.T) {
	reader, client, request := newRequirementsTestReader(t)
	client.balance = big.NewInt(0)
	client.allowance = big.NewInt(0)
	client.budget = big.NewInt(0)
	client.inputEnabled = false
	client.outputEnabled = false

	result, err := reader.ReadSwapRequirements(
		context.Background(), request,
	)
	if err != nil {
		t.Fatal(err)
	}

	want := []string{
		"INPUT_TOKEN_DISABLED",
		"OUTPUT_TOKEN_DISABLED",
		"SWAP_ROUTE_NOT_CONFIGURED",
		"INSUFFICIENT_TOKEN_BALANCE",
		"ERC20_APPROVAL_REQUIRED",
		"INSUFFICIENT_BUDGET",
	}

	if len(result.Blockers) != len(want) {
		t.Fatalf("blockers=%v; want %v", result.Blockers, want)
	}
	for i, code := range want {
		if result.Blockers[i] != code {
			t.Fatalf("blockers=%v; want %v", result.Blockers, want)
		}
	}
}

func TestSwapRequirementsPreservesRPCErrors(t *testing.T) {
	tests := []struct {
		name   string
		method string
		err    error
	}{
		{
			"budget timeout",
			"remaining",
			context.DeadlineExceeded,
		},
		{
			"route unavailable",
			"swapAdapters",
			errors.New("RPC unavailable"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader, client, request := newRequirementsTestReader(t)
			client.failMethod = tt.method
			client.callErr = tt.err

			result, err := reader.ReadSwapRequirements(
				context.Background(), request,
			)
			if !errors.Is(err, tt.err) {
				t.Fatalf("want preserved error %v, got %v", tt.err, err)
			}
			if result.OwnerAddress != "" {
				t.Fatal("RPC failure must not return partial requirements")
			}
		})
	}
}

func TestSwapRequirementsRejectsChangedBlock(t *testing.T) {
	reader, client, request := newRequirementsTestReader(t)
	client.changeHeader = true

	result, err := reader.ReadSwapRequirements(
		context.Background(), request,
	)
	if err == nil {
		t.Fatal("expected changed block to be rejected")
	}
	if result.OwnerAddress != "" {
		t.Fatal("changed block must not return requirements")
	}
}

func TestSwapRequirementsHonorsCancellation(t *testing.T) {
	reader, client, request := newRequirementsTestReader(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := reader.ReadSwapRequirements(ctx, request)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
	if client.headerCalls != 0 {
		t.Fatal("cancelled request reached block header reads")
	}
}

func TestSwapRequirementsBlocksUnverifiedAdapter(t *testing.T) {
	reader, client, request := newRequirementsTestReader(t)

	// Deliberately use a different address from the configured adapter.
	client.adapter = common.HexToAddress(
		"0x0000000000000000000000000000000000000099",
	)

	result, err := reader.ReadSwapRequirements(
		context.Background(), request,
	)
	if err != nil {
		t.Fatal(err)
	}

	if !result.RouteConfigured {
		t.Fatal("nonzero mapping should be reported as configured")
	}
	if result.RouteVerified {
		t.Fatal("unexpected adapter must not be reported as verified")
	}
	if len(result.Blockers) != 1 ||
		result.Blockers[0] != "SWAP_ROUTE_NOT_VERIFIED" {
		t.Fatalf("unexpected blockers: %v", result.Blockers)
	}
}

func TestSwapRequirementsVerifiesConfiguredAdapter(t *testing.T) {
	reader, client, request := newRequirementsTestReader(t)
	client.adapter = common.HexToAddress(fujiLFJSwapAdapter)

	result, err := reader.ReadSwapRequirements(
		context.Background(), request,
	)
	if err != nil {
		t.Fatal(err)
	}

	if !result.RouteConfigured || !result.RouteVerified {
		t.Fatal("expected configured adapter bindings to be verified")
	}
	if len(result.Blockers) != 0 {
		t.Fatalf("unexpected blockers: %v", result.Blockers)
	}
	if result.GasAffordable != nil {
		t.Fatal("route verification does not establish gas affordability")
	}
}
