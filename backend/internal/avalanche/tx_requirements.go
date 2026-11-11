package avalanche

import (
	"context"
	_ "embed"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

const fujiLFJSwapAdapter = "0xD5cae0dF0f11a0Cf9818aCc47b11ce183b0E8AF5"

//go:embed abi/LFJSwapAdapter.json
var lfjExecutionAdapterABIJSON string

// RequirementsRPCClient supplies a block timestamp for expiry checks.
type RequirementsRPCClient interface {
	RPCReader

	HeaderByNumber(
		ctx context.Context,
		blockNumber *big.Int,
	) (*types.Header, error)
}

// SwapRequirements describes observations at one selected block.
// It is not authorization to sign and is not a simulation result.
type SwapRequirements struct {
	OwnerAddress    string `json:"owner_address"`
	ManagerAddress  string `json:"manager_address"`
	TokenInAddress  string `json:"token_in_address"`
	TokenOutAddress string `json:"token_out_address"`

	AmountInRaw        string  `json:"amount_in_raw"`
	TokenBalanceRaw    string  `json:"token_balance_raw"`
	ERC20AllowanceRaw  string  `json:"erc20_allowance_raw"`
	BudgetRemainingRaw string  `json:"budget_remaining_raw"`
	BudgetExpiry       *string `json:"budget_expiry"`
	NativeBalanceWei   string  `json:"native_balance_wei"`

	InputTokenSupported  bool   `json:"input_token_supported"`
	OutputTokenSupported bool   `json:"output_token_supported"`
	SwapAdapterAddress   string `json:"swap_adapter_address"`

	HasTokenBalance  bool  `json:"has_token_balance"`
	HasERC20Approval bool  `json:"has_erc20_approval"`
	HasBudget        bool  `json:"has_budget"`
	BudgetActive     *bool `json:"budget_active"`
	RouteConfigured  bool  `json:"route_configured"`

	// Gas affordability is unknown until a fee estimate is available.
	GasAffordable *bool `json:"gas_affordable"`

	// All applicable blockers are retained, not only the first one.
	Blockers []string `json:"blockers"`

	BlockTimestamp uint64     `json:"block_timestamp"`
	Provenance     Provenance `json:"provenance"`

	RouteVerified bool `json:"route_verified"`
}

type SwapRequirementsReader struct {
	client     RequirementsRPCClient
	builder    *SwapTxBuilder
	adapterABI abi.ABI
}

func NewSwapRequirementsReader(
	client RequirementsRPCClient,
) (*SwapRequirementsReader, error) {
	if client == nil {
		return nil, fmt.Errorf("requirements RPC client is nil")
	}

	builder, err := NewSwapTxBuilder()
	if err != nil {
		return nil, err
	}

	adapterABI, err := abi.JSON(
		strings.NewReader(lfjExecutionAdapterABIJSON),
	)
	if err != nil {
		return nil, fmt.Errorf("parse LFJ execution adapter ABI: %w", err)
	}

	return &SwapRequirementsReader{
		client:     client,
		builder:    builder,
		adapterABI: adapterABI,
	}, nil
}

func (r *SwapRequirementsReader) ReadSwapRequirements(
	ctx context.Context,
	request BuildSwapRequest,
) (SwapRequirements, error) {
	var empty SwapRequirements

	if err := ctx.Err(); err != nil {
		return empty, err
	}

	built, err := r.builder.BuildSwapTx(request, time.Now().UTC())
	if err != nil {
		return empty, err
	}

	owner := common.HexToAddress(built.Prepared.Tx.From)
	manager := r.builder.manager
	tokenIn := common.HexToAddress(built.Summary.TokenIn.Address)
	tokenOut := common.HexToAddress(built.Summary.TokenOut.Address)
	amount, err := ParseRawAmount(built.Summary.AmountIn.Raw)
	if err != nil {
		return empty, err
	}

	readCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	chainID, err := r.client.ChainID(readCtx)
	if err != nil {
		return empty, fmt.Errorf("verify requirements chain: %w", err)
	}
	if chainID == nil || chainID.Cmp(big.NewInt(FujiChainID)) != 0 {
		return empty, ErrWrongChain
	}

	height, err := r.client.BlockNumber(readCtx)
	if err != nil {
		return empty, fmt.Errorf("read requirements block: %w", err)
	}
	block := new(big.Int).SetUint64(height)

	header, err := r.client.HeaderByNumber(readCtx, block)
	if err != nil {
		return empty, fmt.Errorf("read requirements header: %w", err)
	}
	if header == nil || header.Number == nil ||
		header.Number.Cmp(block) != 0 || header.Time == 0 {
		return empty, fmt.Errorf("invalid requirements block header")
	}

	tokenBalance, err := GetERC20Balance(
		readCtx, r.client, tokenIn.Hex(), owner.Hex(), block,
	)
	if err != nil {
		return empty, fmt.Errorf("read swap token balance: %w", err)
	}
	allowance, err := GetERC20Allowance(
		readCtx, r.client, tokenIn.Hex(), owner.Hex(), manager.Hex(), block,
	)
	if err != nil {
		return empty, fmt.Errorf("read owner-to-manager allowance: %w", err)
	}
	nativeBalance, err := r.client.BalanceAt(readCtx, owner, block)
	if err != nil {
		return empty, fmt.Errorf("read gas balance: %w", err)
	}

	for _, value := range []*big.Int{
		tokenBalance, allowance, nativeBalance,
	} {
		if value == nil || value.Sign() < 0 || value.BitLen() > 256 {
			return empty, fmt.Errorf("invalid requirements balance or allowance")
		}
	}

	callManager := func(
		method string,
		args ...interface{},
	) ([]interface{}, error) {
		definition, ok := r.builder.abi.Methods[method]
		if !ok {
			return nil, fmt.Errorf("manager ABI missing %s", method)
		}

		data, err := r.builder.abi.Pack(method, args...)
		if err != nil {
			return nil, fmt.Errorf("encode manager %s: %w", method, err)
		}

		raw, err := r.client.CallContract(
			readCtx,
			ethereum.CallMsg{To: &manager, Data: data},
			block,
		)
		if err != nil {
			return nil, fmt.Errorf("read manager %s: %w", method, err)
		}
		if len(raw) != len(definition.Outputs)*32 {
			return nil, fmt.Errorf("invalid manager %s response length", method)
		}

		values, err := definition.Outputs.Unpack(raw)
		if err != nil {
			return nil, fmt.Errorf("decode manager %s: %w", method, err)
		}
		return values, nil
	}

	inputSupport, err := callManager("supportedTokens", tokenIn)
	if err != nil {
		return empty, err
	}
	outputSupport, err := callManager("supportedTokens", tokenOut)
	if err != nil {
		return empty, err
	}
	route, err := callManager("swapAdapters", tokenIn, tokenOut)
	if err != nil {
		return empty, err
	}
	budgetValues, err := callManager("remaining", owner, tokenIn)
	if err != nil {
		return empty, err
	}

	effectiveRemaining := budgetValues[0].(*big.Int)
	if effectiveRemaining.Sign() < 0 ||
		effectiveRemaining.BitLen() > 256 {
		return empty, fmt.Errorf("invalid manager remaining budget")
	}

	// A positive remaining amount establishes an active budget.
	// Zero cannot distinguish unset, expired, revoked or exhausted.
	var budgetActive *bool
	if effectiveRemaining.Sign() > 0 {
		active := true
		budgetActive = &active
	}

	adapter := route[0].(common.Address)

	routeVerified, err := r.verifySwapAdapter(readCtx, adapter, block)
	if err != nil {
		return empty, err
	}

	result := SwapRequirements{
		OwnerAddress:         owner.Hex(),
		ManagerAddress:       manager.Hex(),
		TokenInAddress:       tokenIn.Hex(),
		TokenOutAddress:      tokenOut.Hex(),
		AmountInRaw:          amount.String(),
		TokenBalanceRaw:      tokenBalance.String(),
		ERC20AllowanceRaw:    allowance.String(),
		BudgetRemainingRaw:   effectiveRemaining.String(),
		BudgetExpiry:         nil,
		NativeBalanceWei:     nativeBalance.String(),
		InputTokenSupported:  inputSupport[0].(bool),
		OutputTokenSupported: outputSupport[0].(bool),
		SwapAdapterAddress:   adapter.Hex(),
		HasTokenBalance:      tokenBalance.Cmp(amount) >= 0,
		HasERC20Approval:     allowance.Cmp(amount) >= 0,
		HasBudget:            effectiveRemaining.Cmp(amount) >= 0,
		BudgetActive:         budgetActive,
		RouteConfigured:      adapter != (common.Address{}),
		GasAffordable:        nil,
		Blockers:             make([]string, 0),
		BlockTimestamp:       header.Time,
		RouteVerified:        routeVerified,
	}

	for _, check := range []struct {
		ok   bool
		code string
	}{
		{result.InputTokenSupported, "INPUT_TOKEN_DISABLED"},
		{result.OutputTokenSupported, "OUTPUT_TOKEN_DISABLED"},
		{result.RouteConfigured, "SWAP_ROUTE_NOT_CONFIGURED"},
		{result.HasTokenBalance, "INSUFFICIENT_TOKEN_BALANCE"},
		{result.HasERC20Approval, "ERC20_APPROVAL_REQUIRED"},
		{result.HasBudget, "INSUFFICIENT_BUDGET"},
	} {
		if !check.ok {
			result.Blockers = append(result.Blockers, check.code)
		}
	}

	if result.RouteConfigured && !result.RouteVerified {
		result.Blockers = append(
			result.Blockers,
			"SWAP_ROUTE_NOT_VERIFIED",
		)
	}

	finalHeader, err := r.client.HeaderByNumber(readCtx, block)
	if err != nil {
		return empty, fmt.Errorf("recheck requirements header: %w", err)
	}
	if finalHeader == nil || finalHeader.Number == nil ||
		finalHeader.Number.Cmp(block) != 0 ||
		finalHeader.Hash() != header.Hash() {
		return empty, fmt.Errorf("requirements block changed; retry required")
	}
	if err := readCtx.Err(); err != nil {
		return empty, err
	}
	if _, err := r.builder.BuildSwapTx(request, time.Now().UTC()); err != nil {
		return empty, fmt.Errorf("revalidate intent after requirements read: %w", err)
	}

	result.Provenance = Provenance{
		Source:      "Fuji RPC and AllowanceManager",
		SourceType:  "rpc",
		ChainID:     uint64(FujiChainID),
		BlockNumber: &height,
		ObservedAt:  time.Now().UTC(),
		Mode:        "live",
		IsDemo:      false,
	}

	return result, nil
}

func (r *SwapRequirementsReader) verifySwapAdapter(
	ctx context.Context,
	adapter common.Address,
	block *big.Int,
) (bool, error) {
	if adapter != common.HexToAddress(fujiLFJSwapAdapter) {
		return false, nil
	}

	for _, check := range []struct {
		method string
		want   common.Address
	}{
		{"manager", r.builder.manager},
		{"router", common.HexToAddress(lfjFujiRouter)},
		{"tokenX", common.HexToAddress(lfjFujiTokenX)},
		{"tokenY", common.HexToAddress(lfjFujiTokenY)},
		{"BIN_STEP", common.Address{}},
	} {
		data, err := r.adapterABI.Pack(check.method)
		if err != nil {
			return false, err
		}

		raw, err := r.client.CallContract(
			ctx,
			ethereum.CallMsg{To: &adapter, Data: data},
			block,
		)
		if err != nil {
			return false, fmt.Errorf("read adapter %s: %w", check.method, err)
		}
		if len(raw) != 32 {
			return false, fmt.Errorf("invalid adapter %s response", check.method)
		}

		values, err := r.adapterABI.Unpack(check.method, raw)
		if err != nil {
			return false, err
		}

		if check.method == "BIN_STEP" {
			value, ok := values[0].(*big.Int)
			if !ok || value == nil || value.Cmp(big.NewInt(1)) != 0 {
				return false, nil
			}
		} else {
			value, ok := values[0].(common.Address)
			if !ok || value != check.want {
				return false, nil
			}
		}
	}

	return true, nil
}
