package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"

	"unihack-backend/internal/ai"
	"unihack-backend/internal/avalanche"

	"github.com/ethereum/go-ethereum/common"
)

const GetLiquidationPriceToolName = "get_liquidation_price"

type LendingRiskReader interface {
	ReadLendingRisk(context.Context, string) (avalanche.AaveRiskSnapshot, error)
}

type GetLiquidationPriceTool struct {
	reader LendingRiskReader
}

func NewGetLiquidationPriceTool(reader LendingRiskReader) (*GetLiquidationPriceTool, error) {
	if reader == nil {
		return nil, errors.New("lending risk reader is nil")
	}
	return &GetLiquidationPriceTool{reader: reader}, nil
}

func (t *GetLiquidationPriceTool) Definition() ai.ToolDefinition {
	return ai.ToolDefinition{
		Name: GetLiquidationPriceToolName,
		Description: "Calculate the hypothetical oracle price at which the modeled " +
			"Aave V3 health factor reaches 1 on Avalanche Fuji. Change only one " +
			"specified collateral token price. Ask for the target token address " +
			"if missing; do not guess it. Not a forecast or a liquidation transaction.",
		InputSchema: json.RawMessage(`{
			"type":"object",
			"properties":{
				"wallet_address":{"type":"string","pattern":"^0x[0-9a-fA-F]{40}$"},
				"chain_id":{"type":"integer","const":43113},
				"target_token_address":{"type":"string","pattern":"^0x[0-9a-fA-F]{40}$"}
			},
			"required":["wallet_address","chain_id","target_token_address"],
			"additionalProperties":false
		}`),
	}
}

func (t *GetLiquidationPriceTool) Execute(
	ctx context.Context,
	arguments json.RawMessage,
) (ai.ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return ai.ToolResult{}, err
	}

	var input struct {
		WalletAddress      string `json:"wallet_address"`
		ChainID            uint64 `json:"chain_id"`
		TargetTokenAddress string `json:"target_token_address"`
	}
	if err := decodeStrictJSON(arguments, &input); err != nil {
		return newToolValidationError(
			"INVALID_TOOL_ARGUMENTS", "invalid liquidation price arguments", 0,
		), nil
	}

	fail := func(code, message string) (ai.ToolResult, error) {
		return newToolValidationError(code, message, input.ChainID), nil
	}

	if input.ChainID != uint64(avalanche.FujiChainID) {
		return fail("UNSUPPORTED_CHAIN_ID", "chain_id must be 43113")
	}

	walletText := strings.TrimSpace(input.WalletAddress)
	targetText := strings.TrimSpace(input.TargetTokenAddress)
	wallet := common.HexToAddress(walletText)
	target := common.HexToAddress(targetText)

	if !common.IsHexAddress(walletText) || wallet == (common.Address{}) {
		return fail("INVALID_WALLET_ADDRESS", "wallet_address is invalid")
	}
	if !common.IsHexAddress(targetText) || target == (common.Address{}) {
		return fail("INVALID_TOKEN_ADDRESS", "target_token_address is invalid")
	}

	snapshot, err := t.reader.ReadLendingRisk(ctx, wallet.Hex())
	if ctx.Err() != nil {
		return ai.ToolResult{}, ctx.Err()
	}
	if err != nil {
		var timeoutErr net.Error
		switch {
		case errors.Is(err, avalanche.ErrWrongChain):
			return fail("UNSUPPORTED_CHAIN_ID", "lending RPC endpoint is not Avalanche Fuji")
		case errors.Is(err, context.DeadlineExceeded),
			errors.As(err, &timeoutErr) && timeoutErr.Timeout():
			return fail("UPSTREAM_TIMEOUT", "lending risk read timed out")
		case errors.Is(err, avalanche.ErrLendingPriceMissing):
			return fail("ORACLE_PRICE_MISSING", "a required oracle price is missing")
		case errors.Is(err, avalanche.ErrLendingScenario):
			return fail("UNSUPPORTED_LIQUIDATION_SCENARIO", "this account mode is not supported")
		default:
			return fail("UPSTREAM_LENDING_ERROR", "could not read a complete lending risk snapshot")
		}
	}

	position := snapshot.Position
	source := position.Provenance
	if !common.IsHexAddress(position.WalletAddress) ||
		common.HexToAddress(position.WalletAddress) != wallet ||
		source.ChainID != input.ChainID ||
		source.BlockNumber == nil ||
		source.ObservedAt.IsZero() ||
		strings.TrimSpace(source.Source) == "" {
		return fail("INVALID_LENDING_RESPONSE", "invalid lending snapshot provenance")
	}

	risk, err := avalanche.CalculateLendingRisk(snapshot.Assets, target.Hex())
	if err != nil {
		switch {
		case errors.Is(err, avalanche.ErrLendingPriceMissing):
			return fail("ORACLE_PRICE_MISSING", "a required oracle price is missing")
		case errors.Is(err, avalanche.ErrLendingScenario):
			return fail("UNSUPPORTED_LIQUIDATION_SCENARIO",
				"target must be enabled collateral with a positive threshold and no debt in that token")
		default:
			return fail("INVALID_LENDING_RESPONSE", "lending risk inputs are invalid")
		}
	}

	// The reader can omit calculation inputs only when reserve debt is zero.
	if risk.HealthStatus == "no_debt" {
		for _, p := range position.Positions {
			if p.StableDebt.Raw != "0" || p.VariableDebt.Raw != "0" {
				return fail("INVALID_LENDING_RESPONSE", "risk inputs omit reserve debt")
			}
		}
	}

	type riskInput struct {
		TokenAddress      string `json:"token_address"`
		Supplied          string `json:"supplied"`
		Debt              string `json:"debt"`
		OraclePrice       string `json:"oracle_price,omitempty"`
		CollateralEnabled bool   `json:"collateral_enabled"`
		ThresholdBPS      uint16 `json:"liquidation_threshold_bps"`
	}

	inputs := make([]riskInput, 0, len(snapshot.Assets))
	for _, asset := range snapshot.Assets {
		item := riskInput{
			TokenAddress:      asset.AssetID,
			Supplied:          asset.Supplied.RatString(),
			Debt:              asset.Debt.RatString(),
			CollateralEnabled: asset.CollateralEnabled,
			ThresholdBPS:      asset.LiquidationThresholdBPS,
		}
		if asset.OraclePrice != nil {
			item.OraclePrice = asset.OraclePrice.RatString()
		}
		inputs = append(inputs, item)
	}

	payload := map[string]any{
		"wallet_address":        wallet.Hex(),
		"target_token_address":  target.Hex(),
		"health_status":         risk.HealthStatus,
		"scenario_status":       risk.ScenarioStatus,
		"inputs":                inputs,
		"provenance":            source,
		"health_factor_formula": "sum(supplied * oracle_price * threshold_bps / 10000) / total_debt_value",
		"threshold_price_formula": "(total_debt_value - other_weighted_collateral) / " +
			"(target_supplied * target_threshold_bps / 10000)",
		"assumption": "Only the target collateral oracle price changes. Quantities, other prices, and thresholds stay fixed.",
		"warning": "Scenario only. Not a forecast or an exact reproduction of protocol rounding. " +
			"Do not treat this price as a guarantee of liquidation.",
	}
	if snapshot.Oracle != "" {
		payload["oracle_address"] = snapshot.Oracle
		payload["oracle_base_unit"] = snapshot.BaseUnit
	}
	if position.HealthFactorRaw != nil {
		payload["protocol_health_factor_raw"] = *position.HealthFactorRaw
	}
	if risk.HealthFactor != nil {
		payload["calculated_health_factor_exact"] = risk.HealthFactor.RatString()
	}
	if risk.LiquidationPrice != nil {
		payload["threshold_price_exact"] = risk.LiquidationPrice.RatString()
		payload["threshold_price_display"] = risk.LiquidationPrice.FloatString(8)
		payload["threshold_price_unit"] = "oracle base currency per target token"
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return ai.ToolResult{}, fmt.Errorf("encode liquidation scenario: %w", err)
	}

	return ai.ToolResult{
		Data:        data,
		Source:      source.Source,
		ChainID:     source.ChainID,
		BlockNumber: source.BlockNumber,
		ObservedAt:  source.ObservedAt,
	}, nil
}

var _ ai.Tool = (*GetLiquidationPriceTool)(nil)
