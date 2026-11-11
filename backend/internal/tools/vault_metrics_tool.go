package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"unihack-backend/internal/ai"
	"unihack-backend/internal/avalanche"

	"github.com/ethereum/go-ethereum/common"
)

const GetVaultMetricsToolName = "get_vault_metrics"

type GetVaultMetricsTool struct {
	reader avalanche.VaultMetricsReader
	maxAge time.Duration
	now    func() time.Time
}

func NewGetVaultMetricsTool(
	reader avalanche.VaultMetricsReader,
	maxAge time.Duration,
) (*GetVaultMetricsTool, error) {
	if reader == nil {
		return nil, errors.New("vault metrics reader is nil")
	}
	if maxAge <= 0 {
		return nil, errors.New("vault metric max age must be positive")
	}
	return &GetVaultMetricsTool{
		reader: reader,
		maxAge: maxAge,
		now:    time.Now,
	}, nil
}

func (t *GetVaultMetricsTool) Definition() ai.ToolDefinition {
	return ai.ToolDefinition{
		Name: GetVaultMetricsToolName,
		Description: "Read total underlying assets and total shares of the configured " +
			"DemoVault on Avalanche Fuji. This vault has no yield strategy. " +
			"Do not interpret total assets as USD TVL or infer APR or APY. " +
			"Report freshness warnings. This tool does not submit transactions.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"wallet_address": {"type": "string", "pattern": "^0x[0-9a-fA-F]{40}$"},
				"chain_id": {"type": "integer", "const": 43113},
				"vault_address": {"type": "string", "pattern": "^0x[0-9a-fA-F]{40}$"}
			},
			"required": ["wallet_address", "chain_id", "vault_address"],
			"additionalProperties": false
		}`),
	}
}

func (t *GetVaultMetricsTool) Execute(
	ctx context.Context,
	arguments json.RawMessage,
) (ai.ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return ai.ToolResult{}, err
	}

	var input struct {
		WalletAddress string `json:"wallet_address"`
		ChainID       uint64 `json:"chain_id"`
		VaultAddress  string `json:"vault_address"`
	}
	if err := decodeStrictJSON(arguments, &input); err != nil {
		return newToolValidationError(
			"INVALID_TOOL_ARGUMENTS", "invalid vault metrics arguments", 0,
		), nil
	}

	fail := func(code, message string) (ai.ToolResult, error) {
		return newToolValidationError(code, message, input.ChainID), nil
	}

	if input.ChainID != uint64(avalanche.FujiChainID) {
		return fail("UNSUPPORTED_CHAIN_ID", "chain_id must be 43113")
	}

	walletText := strings.TrimSpace(input.WalletAddress)
	vaultText := strings.TrimSpace(input.VaultAddress)
	wallet := common.HexToAddress(walletText)
	vault := common.HexToAddress(vaultText)

	if !common.IsHexAddress(walletText) || wallet == (common.Address{}) {
		return fail("INVALID_WALLET_ADDRESS", "wallet_address is invalid")
	}
	if !common.IsHexAddress(vaultText) || vault == (common.Address{}) {
		return fail("INVALID_VAULT_ADDRESS", "vault_address is invalid")
	}

	snapshot, err := t.reader.ReadVault(ctx, avalanche.VaultReadRequest{
		VaultAddress:     vault.Hex(),
		WalletAddress:    wallet.Hex(),
		DepositAssetsRaw: "0",
	})
	if ctx.Err() != nil {
		return ai.ToolResult{}, ctx.Err()
	}
	if err != nil {
		var timeoutErr net.Error
		switch {
		case errors.Is(err, avalanche.ErrUnsupportedVault):
			return fail("UNSUPPORTED_VAULT", "vault is not configured")
		case errors.Is(err, avalanche.ErrWrongChain):
			return fail("UNSUPPORTED_CHAIN_ID", "vault RPC endpoint is not Avalanche Fuji")
		case errors.Is(err, context.DeadlineExceeded),
			errors.As(err, &timeoutErr) && timeoutErr.Timeout():
			return fail("UPSTREAM_TIMEOUT", "vault RPC request timed out")
		default:
			return fail("UPSTREAM_VAULT_ERROR", "could not read vault metrics")
		}
	}

	position := snapshot.Position
	source := position.Provenance
	if !common.IsHexAddress(position.VaultAddress) ||
		common.HexToAddress(position.VaultAddress) != vault ||
		source.ChainID != input.ChainID ||
		source.BlockNumber == nil ||
		source.ObservedAt.IsZero() ||
		strings.TrimSpace(source.Source) == "" {
		return fail("INVALID_VAULT_RESPONSE", "vault snapshot has invalid provenance")
	}

	if snapshot.TotalAssets.Asset.Decimals != 18 ||
		snapshot.TotalShares.Asset.Decimals != 18 {
		return fail("INVALID_VAULT_RESPONSE", "unexpected DemoVault token decimals")
	}

	var sourceTime time.Time
	if snapshot.BlockTime != nil {
		sourceTime = *snapshot.BlockTime
	}
	freshness := avalanche.AssessVaultFreshness(t.now(), sourceTime, t.maxAge)

	warnings := make([]string, 0, 1)
	switch freshness {
	case avalanche.VaultFreshnessStale:
		warnings = append(warnings, "Vault metrics are stale; refresh before using them for a decision.")
	case avalanche.VaultFreshnessUnknown:
		warnings = append(warnings, "Vault metric freshness could not be verified.")
	}

	type metric struct {
		Amount         avalanche.TokenAmount `json:"amount"`
		SourceContract string                `json:"source_contract"`
		SourceBlock    uint64                `json:"source_block"`
		SourceTime     *time.Time            `json:"source_time"`
		RawField       string                `json:"raw_field"`
	}

	payload := map[string]any{
		"vault_address": vault.Hex(),
		"total_assets": metric{
			Amount:         snapshot.TotalAssets,
			SourceContract: vault.Hex(),
			SourceBlock:    *source.BlockNumber,
			SourceTime:     snapshot.BlockTime,
			RawField:       "totalAssets()",
		},
		"total_shares": metric{
			Amount:         snapshot.TotalShares,
			SourceContract: vault.Hex(),
			SourceBlock:    *source.BlockNumber,
			SourceTime:     snapshot.BlockTime,
			RawField:       "totalSupply()",
		},
		"tvl_usd":        nil,
		"apr":            nil,
		"apy":            nil,
		"yield_strategy": nil,
		"metric_notes": map[string]string{
			"tvl_usd": "No verified USD price source for DDEMO.",
			"apr":     "DemoVault has no configured yield strategy.",
			"apy":     "DemoVault has no configured yield strategy.",
		},
		"freshness":            freshness,
		"max_data_age_seconds": t.maxAge.Seconds(),
		"warnings":             warnings,
		"provenance":           source,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return ai.ToolResult{}, fmt.Errorf("encode vault metrics: %w", err)
	}

	return ai.ToolResult{
		Data:        data,
		Source:      source.Source,
		ChainID:     source.ChainID,
		BlockNumber: source.BlockNumber,
		ObservedAt:  source.ObservedAt,
	}, nil
}

var _ ai.Tool = (*GetVaultMetricsTool)(nil)
