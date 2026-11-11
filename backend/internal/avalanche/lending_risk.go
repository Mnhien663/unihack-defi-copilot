package avalanche

import (
	"errors"
	"fmt"
	"math/big"
)

var (
	ErrLendingPriceMissing = errors.New("required lending oracle price is missing")
	ErrLendingScenario     = errors.New("unsupported liquidation scenario")
)

// LendingRiskAsset represents one unique underlying asset.
// AssetID must be canonical, for example the lowercased contract address.
// A nil OraclePrice means missing data. Zero amounts must be explicit.
type LendingRiskAsset struct {
	AssetID                 string
	Supplied                *big.Rat
	Debt                    *big.Rat
	OraclePrice             *big.Rat
	CollateralEnabled       bool
	LiquidationThresholdBPS uint16
}

type LendingRiskResult struct {
	HealthFactor     *big.Rat // nil when there is no debt
	LiquidationPrice *big.Rat // nil when there is no positive threshold price
	HealthStatus     string
	ScenarioStatus   string
}

// CalculateLendingRisk uses exact arithmetic for a fixed-threshold model.
// An empty targetAssetID requests HF only. Otherwise, only the target
// collateral's price changes; quantities, other prices and LT stay fixed.
func CalculateLendingRisk(
	assets []LendingRiskAsset,
	targetAssetID string,
) (LendingRiskResult, error) {
	seen := make(map[string]bool, len(assets))
	var hasDebt bool

	for _, asset := range assets {
		if asset.AssetID == "" || seen[asset.AssetID] {
			return LendingRiskResult{}, fmt.Errorf(
				"empty or duplicate lending asset ID: %q", asset.AssetID,
			)
		}
		seen[asset.AssetID] = true

		if asset.Supplied == nil || asset.Debt == nil ||
			asset.Supplied.Sign() < 0 || asset.Debt.Sign() < 0 ||
			asset.LiquidationThresholdBPS > 10_000 {
			return LendingRiskResult{}, fmt.Errorf(
				"invalid lending inputs for %s", asset.AssetID,
			)
		}
		hasDebt = hasDebt || asset.Debt.Sign() > 0
	}

	result := LendingRiskResult{ScenarioStatus: "not_requested"}
	if !hasDebt {
		result.HealthStatus = "no_debt"
		if targetAssetID != "" {
			result.ScenarioStatus = "no_debt"
		}
		return result, nil
	}

	totalDebt := new(big.Rat)
	weightedCollateral := new(big.Rat)
	otherCollateral := new(big.Rat)
	targetCoefficient := new(big.Rat)

	for _, asset := range assets {
		collateral := asset.CollateralEnabled &&
			asset.Supplied.Sign() > 0 &&
			asset.LiquidationThresholdBPS > 0
		borrowed := asset.Debt.Sign() > 0
		isTarget := asset.AssetID == targetAssetID

		if isTarget && (!collateral || borrowed) {
			return LendingRiskResult{}, fmt.Errorf(
				"%w: target must contribute collateral and have no debt in the same asset",
				ErrLendingScenario,
			)
		}
		if !collateral && !borrowed {
			continue
		}
		if asset.OraclePrice == nil || asset.OraclePrice.Sign() <= 0 {
			return LendingRiskResult{}, fmt.Errorf(
				"%w: %s", ErrLendingPriceMissing, asset.AssetID,
			)
		}
		if borrowed {
			totalDebt.Add(totalDebt, new(big.Rat).Mul(asset.Debt, asset.OraclePrice))
		}
		if collateral {
			threshold := big.NewRat(int64(asset.LiquidationThresholdBPS), 10_000)
			coefficient := new(big.Rat).Mul(asset.Supplied, threshold)
			weighted := new(big.Rat).Mul(coefficient, asset.OraclePrice)
			weightedCollateral.Add(weightedCollateral, weighted)

			if isTarget {
				targetCoefficient.Set(coefficient)
			} else {
				otherCollateral.Add(otherCollateral, weighted)
			}
		}
	}

	result.HealthFactor = new(big.Rat).Quo(weightedCollateral, totalDebt)
	switch result.HealthFactor.Cmp(big.NewRat(1, 1)) {
	case -1:
		result.HealthStatus = "below_threshold"
	case 0:
		result.HealthStatus = "at_threshold"
	default:
		result.HealthStatus = "above_threshold"
	}

	if targetAssetID == "" {
		return result, nil
	}
	if targetCoefficient.Sign() == 0 {
		return LendingRiskResult{}, fmt.Errorf("%w: target asset not found", ErrLendingScenario)
	}

	remainingDebt := new(big.Rat).Sub(totalDebt, otherCollateral)
	if remainingDebt.Sign() <= 0 {
		result.ScenarioStatus = "no_positive_threshold_price"
		return result, nil
	}

	result.LiquidationPrice = new(big.Rat).Quo(remainingDebt, targetCoefficient)
	result.ScenarioStatus = "threshold_price"
	return result, nil
}
