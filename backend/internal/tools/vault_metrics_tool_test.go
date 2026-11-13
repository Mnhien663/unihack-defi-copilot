package tools

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"unihack-backend/internal/avalanche"
)

type vaultMetricsReaderStub func(
	context.Context,
	avalanche.VaultReadRequest,
) (avalanche.VaultMetricsResult, error)

func (f vaultMetricsReaderStub) ReadVault(
	ctx context.Context,
	request avalanche.VaultReadRequest,
) (avalanche.VaultMetricsResult, error) {
	return f(ctx, request)
}

func TestGetVaultMetricsToolContract(t *testing.T) {
	const vault = "0x896C05deae3d6e35021B12DD361401736Bb8aAda"
	const underlying = "0xcCCb83Fbe349fAebDE74Ce633D25B282E6FDd21A"
	const wallet = "0x0000000000000000000000000000000000000001"
	const source = "DemoVault Fuji"
	const maxAge = 5 * time.Minute

	now := time.Date(2026, 11, 11, 13, 0, 0, 0, time.UTC)
	freshTime := now.Add(-time.Minute)
	staleTime := now.Add(-maxAge)
	block := uint64(12345)

	tests := []struct {
		name         string
		blockTime    *time.Time
		decimals     uint8
		wantFresh    string
		wantWarnings int
		wantCode     string
		readerErr    error
	}{
		{name: "fresh", blockTime: &freshTime, decimals: 18, wantFresh: "fresh"},
		{
			name: "stale at boundary despite recent observation", blockTime: &staleTime,
			decimals: 18, wantFresh: "stale", wantWarnings: 1,
		},
		{
			name: "missing block time", decimals: 18,
			wantFresh: "unknown", wantWarnings: 1,
		},
		{
			name: "wrong underlying decimals", blockTime: &freshTime,
			decimals: 6, wantCode: "INVALID_VAULT_RESPONSE",
		},
		{
			name:      "RPC unavailable returns no metrics",
			readerErr: errors.New("RPC unavailable"),
			wantCode:  "UPSTREAM_VAULT_ERROR",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			reader := vaultMetricsReaderStub(func(
				_ context.Context,
				request avalanche.VaultReadRequest,
			) (avalanche.VaultMetricsResult, error) {
				calls++
				if request.VaultAddress != vault ||
					request.WalletAddress != wallet ||
					request.DepositAssetsRaw != "0" {
					t.Fatalf("unexpected reader request: %+v", request)
				}
				if tt.readerErr != nil {
					return avalanche.VaultMetricsResult{}, tt.readerErr
				}

				return avalanche.VaultMetricsResult{
					Position: avalanche.VaultPositionResult{
						VaultAddress:  vault,
						WalletAddress: wallet,
						Provenance: avalanche.Provenance{
							Source:      source,
							SourceType:  "rpc",
							ChainID:     43113,
							BlockNumber: &block,
							ObservedAt:  now,
							Mode:        "live",
							IsDemo:      true,
						},
					},
					TotalAssets: avalanche.TokenAmount{
						Asset: avalanche.Asset{
							ChainID: 43113, Address: underlying,
							Symbol: "DDEMO", Decimals: tt.decimals,
						},
						Raw: "2500000000000000000", Formatted: "2.5",
					},
					TotalShares: avalanche.TokenAmount{
						Asset: avalanche.Asset{
							ChainID: 43113, Address: vault,
							Symbol: "dvDDEMO", Decimals: 18,
						},
						Raw: "2000000000000000000", Formatted: "2",
					},
					BlockTime: tt.blockTime,
				}, nil
			})

			tool, err := NewGetVaultMetricsTool(reader, maxAge)
			if err != nil {
				t.Fatal(err)
			}
			tool.now = func() time.Time { return now }

			args := json.RawMessage(`{
				"wallet_address":"` + wallet + `",
				"chain_id":43113,
				"vault_address":"` + vault + `"
			}`)

			result, err := tool.Execute(context.Background(), args)
			if err != nil {
				t.Fatal(err)
			}
			if calls != 1 {
				t.Fatalf("reader calls = %d; want 1", calls)
			}
			if tt.wantCode != "" {
				if result.Error == nil || result.Error.Code != tt.wantCode {
					t.Fatalf("want %s, got %+v", tt.wantCode, result.Error)
				}

				if tt.readerErr != nil {
					if len(result.Data) != 0 && string(result.Data) != "null" {
						t.Fatalf("RPC error must not return metrics data: %s", result.Data)
					}
					if result.BlockNumber != nil || result.Source == source {
						t.Fatalf("RPC error must not report successful vault provenance: %+v", result)
					}
				}
				return
			}
			if result.Error != nil {
				t.Fatalf("unexpected tool error: %+v", result.Error)
			}
			if result.Source != source ||
				result.ChainID != 43113 ||
				result.BlockNumber == nil ||
				*result.BlockNumber != block ||
				!result.ObservedAt.Equal(now) {
				t.Fatalf("unexpected tool provenance: %+v", result)
			}

			type metric struct {
				Amount         avalanche.TokenAmount `json:"amount"`
				SourceContract string                `json:"source_contract"`
				SourceBlock    uint64                `json:"source_block"`
				SourceTime     *time.Time            `json:"source_time"`
				RawField       string                `json:"raw_field"`
			}
			var data struct {
				TotalAssets   metric               `json:"total_assets"`
				TotalShares   metric               `json:"total_shares"`
				Freshness     string               `json:"freshness"`
				Warnings      []string             `json:"warnings"`
				Provenance    avalanche.Provenance `json:"provenance"`
				APR           json.RawMessage      `json:"apr"`
				APY           json.RawMessage      `json:"apy"`
				TVLUSD        json.RawMessage      `json:"tvl_usd"`
				YieldStrategy json.RawMessage      `json:"yield_strategy"`
				Position      json.RawMessage      `json:"position"`
				PreviewShares json.RawMessage      `json:"preview_shares"`
			}
			if err := json.Unmarshal(result.Data, &data); err != nil {
				t.Fatal(err)
			}
			if data.Freshness != tt.wantFresh || len(data.Warnings) != tt.wantWarnings {
				t.Fatalf("unexpected freshness or warnings: %s", result.Data)
			}
			if data.Provenance.Mode != "live" || !data.Provenance.IsDemo {
				t.Fatal("live RPC data from a demo vault must preserve both flags")
			}

			checkMetric := func(got metric, field, symbol, raw string) {
				t.Helper()
				if got.RawField != field ||
					got.SourceContract != vault ||
					got.SourceBlock != block ||
					got.Amount.Asset.Symbol != symbol ||
					got.Amount.Raw != raw {
					t.Fatalf("unexpected metric %s: %+v", field, got)
				}
				if tt.blockTime == nil {
					if got.SourceTime != nil {
						t.Fatalf("%s substituted observation time for missing block time", field)
					}
				} else if got.SourceTime == nil || !got.SourceTime.Equal(*tt.blockTime) {
					t.Fatalf("%s has wrong source time: %v", field, got.SourceTime)
				}
			}

			checkMetric(data.TotalAssets, "totalAssets()", "DDEMO", "2500000000000000000")
			checkMetric(data.TotalShares, "totalSupply()", "dvDDEMO", "2000000000000000000")

			if string(data.APR) != "null" || string(data.APY) != "null" ||
				string(data.TVLUSD) != "null" || string(data.YieldStrategy) != "null" {
				t.Fatalf("unavailable metrics must be explicit nulls: %s", result.Data)
			}
			if data.Position != nil || data.PreviewShares != nil {
				t.Fatal("vault metrics must not expose wallet position or deposit preview")
			}
		})
	}
}
