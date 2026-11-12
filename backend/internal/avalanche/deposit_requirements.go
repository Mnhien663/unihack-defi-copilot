package avalanche

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
)

const fujiVaultExecutionAdapter = "0x78b5aADB9a8570C7B4387F29119201AF53008b7D"

type DepositRequirements struct {
	OwnerAddress        string `json:"owner_address"`
	ManagerAddress      string `json:"manager_address"`
	AssetAddress        string `json:"asset_address"`
	VaultAddress        string `json:"vault_address"`
	VaultAdapterAddress string `json:"vault_adapter_address"`
	ShareTokenAddress   string `json:"share_token_address"`

	AssetsRaw          string `json:"assets_raw"`
	TokenBalanceRaw    string `json:"token_balance_raw"`
	ERC20AllowanceRaw  string `json:"erc20_allowance_raw"`
	BudgetRemainingRaw string `json:"budget_remaining_raw"`
	NativeBalanceWei   string `json:"native_balance_wei"`
	PreviewSharesRaw   string `json:"preview_shares_raw"`
	MaxDepositRaw      string `json:"max_deposit_raw"`

	InputTokenSupported bool `json:"input_token_supported"`
	ShareTokenSupported bool `json:"share_token_supported"`
	RouteConfigured     bool `json:"route_configured"`
	RouteVerified       bool `json:"route_verified"`
	HasTokenBalance     bool `json:"has_token_balance"`
	HasERC20Approval    bool `json:"has_erc20_approval"`
	HasBudget           bool `json:"has_budget"`
	WithinMaxDeposit    bool `json:"within_max_deposit"`
	MeetsMinimumShares  bool `json:"meets_minimum_shares"`

	GasAffordable  *bool      `json:"gas_affordable"`
	Blockers       []string   `json:"blockers"`
	BlockTimestamp uint64     `json:"block_timestamp"`
	Provenance     Provenance `json:"provenance"`
}

type DepositRequirementsReader struct {
	client     RequirementsRPCClient
	builder    *DepositTxBuilder
	vaultABI   abi.ABI
	adapterABI abi.ABI
}

func NewDepositRequirementsReader(
	client RequirementsRPCClient,
) (*DepositRequirementsReader, error) {
	if client == nil {
		return nil, errors.New("deposit requirements RPC client is nil")
	}
	builder, err := NewDepositTxBuilder()
	if err != nil {
		return nil, err
	}
	vaultABI, err := abi.JSON(strings.NewReader(vaultReadABIJSON))
	if err != nil {
		return nil, err
	}
	adapterABI, err := abi.JSON(strings.NewReader(`[
		{"type":"function","name":"manager","stateMutability":"view",
		 "inputs":[],"outputs":[{"type":"address"}]},
		{"type":"function","name":"underlying","stateMutability":"view",
		 "inputs":[],"outputs":[{"type":"address"}]},
		{"type":"function","name":"vault","stateMutability":"view",
		 "inputs":[],"outputs":[{"type":"address"}]}
	]`))
	if err != nil {
		return nil, err
	}
	return &DepositRequirementsReader{
		client: client, builder: builder,
		vaultABI: vaultABI, adapterABI: adapterABI,
	}, nil
}

// Reads fixed-size ABI outputs at one pinned block.
// Repacking also rejects non-canonical output encodings.
func (r *DepositRequirementsReader) callAt(
	ctx context.Context,
	block *big.Int,
	target common.Address,
	definition abi.ABI,
	method string,
	args ...interface{},
) ([]interface{}, error) {
	m, ok := definition.Methods[method]
	if !ok {
		return nil, fmt.Errorf("ABI missing %s", method)
	}
	data, err := definition.Pack(method, args...)
	if err != nil {
		return nil, err
	}
	raw, err := r.client.CallContract(
		ctx, ethereum.CallMsg{To: &target, Data: data}, block,
	)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", method, err)
	}
	if len(raw) != len(m.Outputs)*32 {
		return nil, fmt.Errorf("invalid %s output length", method)
	}
	values, err := m.Outputs.Unpack(raw)
	if err != nil {
		return nil, err
	}
	canonical, err := m.Outputs.Pack(values...)
	if err != nil || !bytes.Equal(raw, canonical) {
		return nil, fmt.Errorf("non-canonical %s output", method)
	}
	return values, nil
}

func (r *DepositRequirementsReader) ReadDepositRequirements(
	ctx context.Context,
	request BuildDepositRequest,
) (DepositRequirements, error) {
	var empty DepositRequirements
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	built, err := r.builder.BuildDepositTx(request, time.Now().UTC())
	if err != nil {
		return empty, err
	}

	readCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	chain, err := r.client.ChainID(readCtx)
	if err != nil {
		return empty, err
	}
	if chain == nil || chain.Cmp(big.NewInt(FujiChainID)) != 0 {
		return empty, ErrWrongChain
	}
	height, err := r.client.BlockNumber(readCtx)
	if err != nil {
		return empty, err
	}
	if height == 0 {
		return empty, errors.New("invalid deposit requirements block")
	}
	block := new(big.Int).SetUint64(height)
	header, err := r.client.HeaderByNumber(readCtx, block)
	if err != nil {
		return empty, err
	}
	if header == nil || header.Number == nil ||
		header.Number.Cmp(block) != 0 || header.Time == 0 {
		return empty, errors.New("invalid deposit requirements header")
	}
	if header.Time >= request.Deadline {
		return empty, errors.New("deposit deadline is not future at selected block")
	}

	owner := common.HexToAddress(built.Prepared.Tx.From)
	manager := r.builder.manager
	asset := common.HexToAddress(demoFujiAsset)
	vault := common.HexToAddress(demoFujiVault)
	amount, err := ParseRawAmount(built.Summary.Assets.Raw)
	if err != nil {
		return empty, err
	}
	minimum, err := ParseRawAmount(built.Summary.MinShares.Raw)
	if err != nil {
		return empty, err
	}

	balance, err := GetERC20Balance(
		readCtx, r.client, asset.Hex(), owner.Hex(), block,
	)
	if err != nil {
		return empty, err
	}
	allowance, err := GetERC20Allowance(
		readCtx, r.client, asset.Hex(), owner.Hex(), manager.Hex(), block,
	)
	if err != nil {
		return empty, err
	}
	native, err := r.client.BalanceAt(readCtx, owner, block)
	if err != nil {
		return empty, err
	}
	for _, value := range []*big.Int{balance, allowance, native} {
		if value == nil || value.Sign() < 0 || value.BitLen() > 256 {
			return empty, errors.New("invalid deposit balance or allowance")
		}
	}

	callManager := func(method string, args ...interface{}) ([]interface{}, error) {
		return r.callAt(readCtx, block, manager, r.builder.abi, method, args...)
	}
	inputSupport, err := callManager("supportedTokens", asset)
	if err != nil {
		return empty, err
	}
	shareSupport, err := callManager("supportedTokens", vault)
	if err != nil {
		return empty, err
	}
	config, err := callManager("vaultConfigs", asset)
	if err != nil {
		return empty, err
	}
	budget, err := callManager("remaining", owner, asset)
	if err != nil {
		return empty, err
	}

	adapter := config[0].(common.Address)
	shareToken := config[1].(common.Address)
	remaining := budget[0].(*big.Int)

	underlying, err := r.callAt(
		readCtx, block, vault, r.vaultABI, "asset",
	)
	if err != nil {
		return empty, err
	}
	if underlying[0].(common.Address) != asset {
		return empty, errors.New("vault underlying changed")
	}

	previewValues, err := r.callAt(
		readCtx, block, vault, r.vaultABI, "previewDeposit", amount,
	)
	if err != nil {
		return empty, err
	}
	maxValues, err := r.callAt(
		readCtx, block, vault, r.vaultABI, "maxDeposit", owner,
	)
	if err != nil {
		return empty, err
	}
	preview := previewValues[0].(*big.Int)
	maximum := maxValues[0].(*big.Int)

	verified := adapter == common.HexToAddress(fujiVaultExecutionAdapter) &&
		shareToken == vault
	if verified {
		for _, binding := range []struct {
			method string
			want   common.Address
		}{
			{"manager", manager},
			{"underlying", asset},
			{"vault", vault},
		} {
			values, err := r.callAt(
				readCtx, block, adapter, r.adapterABI, binding.method,
			)
			if err != nil {
				return empty, err
			}
			if values[0].(common.Address) != binding.want {
				verified = false
			}
		}
	}

	result := DepositRequirements{
		OwnerAddress:        owner.Hex(),
		ManagerAddress:      manager.Hex(),
		AssetAddress:        asset.Hex(),
		VaultAddress:        vault.Hex(),
		VaultAdapterAddress: adapter.Hex(),
		ShareTokenAddress:   shareToken.Hex(),
		AssetsRaw:           amount.String(),
		TokenBalanceRaw:     balance.String(),
		ERC20AllowanceRaw:   allowance.String(),
		BudgetRemainingRaw:  remaining.String(),
		NativeBalanceWei:    native.String(),
		PreviewSharesRaw:    preview.String(),
		MaxDepositRaw:       maximum.String(),
		InputTokenSupported: inputSupport[0].(bool),
		ShareTokenSupported: shareSupport[0].(bool),
		RouteConfigured:     adapter != (common.Address{}),
		RouteVerified:       verified,
		HasTokenBalance:     balance.Cmp(amount) >= 0,
		HasERC20Approval:    allowance.Cmp(amount) >= 0,
		HasBudget:           remaining.Cmp(amount) >= 0,
		WithinMaxDeposit:    amount.Cmp(maximum) <= 0,
		MeetsMinimumShares:  preview.Sign() > 0 && preview.Cmp(minimum) >= 0,
		Blockers:            make([]string, 0),
		BlockTimestamp:      header.Time,
	}

	for _, check := range []struct {
		ok   bool
		code string
	}{
		{result.InputTokenSupported, "INPUT_TOKEN_DISABLED"},
		{result.ShareTokenSupported, "SHARE_TOKEN_DISABLED"},
		{result.RouteConfigured, "VAULT_ROUTE_NOT_CONFIGURED"},
		{result.HasTokenBalance, "INSUFFICIENT_TOKEN_BALANCE"},
		{result.HasERC20Approval, "ERC20_APPROVAL_REQUIRED"},
		{result.HasBudget, "INSUFFICIENT_BUDGET"},
		{result.WithinMaxDeposit, "MAX_DEPOSIT_EXCEEDED"},
		{result.MeetsMinimumShares, "MIN_SHARES_NOT_MET"},
	} {
		if !check.ok {
			result.Blockers = append(result.Blockers, check.code)
		}
	}
	if result.RouteConfigured && !result.RouteVerified {
		result.Blockers = append(result.Blockers, "VAULT_ROUTE_NOT_VERIFIED")
	}

	finalHeader, err := r.client.HeaderByNumber(readCtx, block)
	if err != nil {
		return empty, err
	}
	if finalHeader == nil || finalHeader.Number == nil ||
		finalHeader.Number.Cmp(block) != 0 ||
		finalHeader.Hash() != header.Hash() {
		return empty, errors.New("deposit requirements block changed; retry")
	}
	if err := readCtx.Err(); err != nil {
		return empty, err
	}
	if _, err := r.builder.BuildDepositTx(request, time.Now().UTC()); err != nil {
		return empty, err
	}

	result.Provenance = Provenance{
		Source:      "Fuji RPC DemoVault deposit requirements",
		SourceType:  "rpc",
		ChainID:     uint64(FujiChainID),
		BlockNumber: &height,
		ObservedAt:  time.Now().UTC(),
		Mode:        "live",
		IsDemo:      true,
	}
	return result, nil
}
