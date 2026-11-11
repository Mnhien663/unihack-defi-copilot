package avalanche

import (
	"errors"
	"math/big"
	"testing"
)

func TestCalculateLendingRisk(t *testing.T) {
	asset := func(id string, supplied, debt int64, price *big.Rat, lt uint16) LendingRiskAsset {
		return LendingRiskAsset{
			AssetID:                 id,
			Supplied:                big.NewRat(supplied, 1),
			Debt:                    big.NewRat(debt, 1),
			OraclePrice:             price,
			CollateralEnabled:       supplied > 0,
			LiquidationThresholdBPS: lt,
		}
	}

	debt100 := asset("debt", 0, 100, big.NewRat(1, 1), 0)

	tests := []struct {
		name, target             string
		assets                   []LendingRiskAsset
		wantHF, wantPrice        string
		wantHealth, wantScenario string
		wantErr                  error
	}{
		{
			name: "zero debt needs no oracle price", target: "a",
			assets:     []LendingRiskAsset{asset("a", 2, 0, nil, 8000)},
			wantHealth: "no_debt", wantScenario: "no_debt",
		},
		{
			name: "multiple collateral", target: "a",
			assets: []LendingRiskAsset{
				asset("a", 2, 0, big.NewRat(60, 1), 8000),
				asset("b", 1, 0, big.NewRat(40, 1), 5000),
				debt100,
			},
			wantHF: "29/25", wantPrice: "50",
			wantHealth: "above_threshold", wantScenario: "threshold_price",
		},
		{
			name:    "missing oracle price",
			assets:  []LendingRiskAsset{asset("a", 1, 0, nil, 8000), debt100},
			wantErr: ErrLendingPriceMissing,
		},
		{
			name: "just below one",
			assets: []LendingRiskAsset{
				asset("a", 1, 0, big.NewRat(9999, 80), 8000), debt100,
			},
			wantHF: "9999/10000", wantHealth: "below_threshold", wantScenario: "not_requested",
		},
		{
			name: "exactly one",
			assets: []LendingRiskAsset{
				asset("a", 1, 0, big.NewRat(125, 1), 8000), debt100,
			},
			wantHF: "1", wantHealth: "at_threshold", wantScenario: "not_requested",
		},
		{
			name: "just above one",
			assets: []LendingRiskAsset{
				asset("a", 1, 0, big.NewRat(10001, 80), 8000), debt100,
			},
			wantHF: "10001/10000", wantHealth: "above_threshold", wantScenario: "not_requested",
		},
		{
			name: "other collateral alone covers debt", target: "a",
			assets: []LendingRiskAsset{
				asset("a", 1, 0, big.NewRat(10, 1), 8000),
				asset("b", 1, 0, big.NewRat(200, 1), 5000),
				debt100,
			},
			wantHF: "27/25", wantHealth: "above_threshold",
			wantScenario: "no_positive_threshold_price",
		},
		{
			name: "target is also borrowed", target: "a",
			assets:  []LendingRiskAsset{asset("a", 2, 1, big.NewRat(100, 1), 8000)},
			wantErr: ErrLendingScenario,
		},
	}

	ratString := func(value *big.Rat) string {
		if value == nil {
			return ""
		}
		return value.RatString()
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := CalculateLendingRisk(tt.assets, tt.target)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("error = %v; want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if ratString(got.HealthFactor) != tt.wantHF ||
				ratString(got.LiquidationPrice) != tt.wantPrice ||
				got.HealthStatus != tt.wantHealth ||
				got.ScenarioStatus != tt.wantScenario {
				t.Fatalf("unexpected lending risk: %+v", got)
			}
		})
	}
}
