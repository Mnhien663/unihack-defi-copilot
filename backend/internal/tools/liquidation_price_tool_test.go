package tools

import (
	"context"
	"encoding/json"
	"math/big"
	"testing"
	"time"

	"unihack-backend/internal/avalanche"
)

type liquidationRiskReaderStub struct {
	snapshot avalanche.AaveRiskSnapshot
	wallet   string
	calls    int
}

func (s *liquidationRiskReaderStub) ReadLendingRisk(
	_ context.Context,
	wallet string,
) (avalanche.AaveRiskSnapshot, error) {
	s.calls++
	s.wallet = wallet
	return s.snapshot, nil
}

func TestGetLiquidationPriceToolContract(t *testing.T) {
	const wallet = "0x0000000000000000000000000000000000000001"
	const target = "0x0000000000000000000000000000000000000010"
	const other = "0x0000000000000000000000000000000000000020"
	const debtToken = "0x0000000000000000000000000000000000000030"
	const oracle = "0x0000000000000000000000000000000000000040"

	block := uint64(12345)
	observedAt := time.Date(2026, 11, 11, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name          string
		noDebt        bool
		missingPrice  bool
		wantErrorCode string
	}{
		{name: "multiple collateral threshold price"},
		{name: "no debt has no threshold price", noDebt: true},
		{
			name:          "missing oracle price",
			missingPrice:  true,
			wantErrorCode: "ORACLE_PRICE_MISSING",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snapshot := avalanche.AaveRiskSnapshot{
				Position: avalanche.LendingPositionResult{
					Protocol:      "Aave V3",
					WalletAddress: wallet,
					Provenance: avalanche.Provenance{
						Source:      "aave_test_fixture",
						SourceType:  "mock",
						ChainID:     43113,
						BlockNumber: &block,
						ObservedAt:  observedAt,
						IsDemo:      true,
					},
				},
			}

			if !tt.noDebt {
				protocolHF := "1160000000000000000"
				snapshot.Position.HealthFactorRaw = &protocolHF
				snapshot.Oracle = oracle
				snapshot.BaseUnit = "100000000"
				snapshot.Assets = []avalanche.LendingRiskAsset{
					{
						AssetID:                 target,
						Supplied:                big.NewRat(2, 1),
						Debt:                    big.NewRat(0, 1),
						OraclePrice:             big.NewRat(60, 1),
						CollateralEnabled:       true,
						LiquidationThresholdBPS: 8000,
					},
					{
						AssetID:                 other,
						Supplied:                big.NewRat(1, 1),
						Debt:                    big.NewRat(0, 1),
						OraclePrice:             big.NewRat(40, 1),
						CollateralEnabled:       true,
						LiquidationThresholdBPS: 5000,
					},
					{
						AssetID:     debtToken,
						Supplied:    big.NewRat(0, 1),
						Debt:        big.NewRat(100, 1),
						OraclePrice: big.NewRat(1, 1),
					},
				}
				if tt.missingPrice {
					snapshot.Assets[2].OraclePrice = nil
				}
			}

			reader := &liquidationRiskReaderStub{snapshot: snapshot}
			tool, err := NewGetLiquidationPriceTool(reader)
			if err != nil {
				t.Fatal(err)
			}

			args, err := json.Marshal(map[string]any{
				"wallet_address":       wallet,
				"chain_id":             43113,
				"target_token_address": target,
			})
			if err != nil {
				t.Fatal(err)
			}

			result, err := tool.Execute(context.Background(), args)
			if err != nil {
				t.Fatal(err)
			}
			if reader.calls != 1 || reader.wallet != wallet {
				t.Fatalf("unexpected reader call: calls=%d wallet=%s", reader.calls, reader.wallet)
			}

			if tt.wantErrorCode != "" {
				if result.Error == nil || result.Error.Code != tt.wantErrorCode {
					t.Fatalf("want %s, got %+v", tt.wantErrorCode, result.Error)
				}
				return
			}
			if result.Error != nil {
				t.Fatalf("unexpected tool error: %+v", result.Error)
			}
			if result.Source != "aave_test_fixture" ||
				result.ChainID != 43113 ||
				result.BlockNumber == nil ||
				*result.BlockNumber != block ||
				!result.ObservedAt.Equal(observedAt) {
				t.Fatalf("unexpected provenance: %+v", result)
			}

			var data map[string]json.RawMessage
			if err := json.Unmarshal(result.Data, &data); err != nil {
				t.Fatal(err)
			}

			assertString := func(key, want string) {
				t.Helper()
				var got string
				if err := json.Unmarshal(data[key], &got); err != nil || got != want {
					t.Fatalf("%s = %s; want %q", key, data[key], want)
				}
			}

			if tt.noDebt {
				assertString("health_status", "no_debt")
				assertString("scenario_status", "no_debt")
				for _, key := range []string{
					"threshold_price_exact",
					"threshold_price_display",
					"calculated_health_factor_exact",
				} {
					if _, exists := data[key]; exists {
						t.Fatalf("no-debt response must omit %s", key)
					}
				}
				return
			}

			assertString("health_status", "above_threshold")
			assertString("scenario_status", "threshold_price")
			assertString("threshold_price_exact", "50")
			assertString("calculated_health_factor_exact", "29/25")
			assertString("protocol_health_factor_raw", "1160000000000000000")
			assertString("oracle_address", oracle)
			assertString("oracle_base_unit", "100000000")

			var inputs []json.RawMessage
			if err := json.Unmarshal(data["inputs"], &inputs); err != nil || len(inputs) != 3 {
				t.Fatalf("want three calculation inputs, got %s", data["inputs"])
			}

			for _, key := range []string{
				"health_factor_formula", "threshold_price_formula", "assumption", "warning",
			} {
				var value string
				if err := json.Unmarshal(data[key], &value); err != nil || value == "" {
					t.Fatalf("missing explanation field %s", key)
				}
			}
		})
	}
}
