package tools

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"unihack-backend/internal/avalanche"
)

type lendingPositionReaderStub func(
	context.Context,
	string,
) (avalanche.LendingPositionResult, error)

func (f lendingPositionReaderStub) ReadLendingPosition(
	ctx context.Context,
	wallet string,
) (avalanche.LendingPositionResult, error) {
	return f(ctx, wallet)
}

func TestGetLendingPositionToolContract(t *testing.T) {
	const wallet = "0x0000000000000000000000000000000000000001"
	const token = "0x0000000000000000000000000000000000000002"
	const hfRaw = "4300000000000000000"

	block := uint64(12345)
	observedAt := time.Date(2026, 11, 11, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name      string
		chainID   uint64
		hasDebt   bool
		readerErr error
		wantCode  string
		wantCalls int
	}{
		{name: "empty wallet", chainID: 43113, wantCalls: 1},
		{name: "wallet with debt", chainID: 43113, hasDebt: true, wantCalls: 1},
		{name: "wrong chain", chainID: 1, wantCode: "UNSUPPORTED_CHAIN_ID"},
		{
			name:      "upstream timeout",
			chainID:   43113,
			readerErr: context.DeadlineExceeded,
			wantCode:  "UPSTREAM_TIMEOUT",
			wantCalls: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			reader := lendingPositionReaderStub(func(
				_ context.Context,
				gotWallet string,
			) (avalanche.LendingPositionResult, error) {
				calls++
				if gotWallet != wallet {
					t.Fatalf("reader wallet = %q; want %q", gotWallet, wallet)
				}
				if tt.readerErr != nil {
					return avalanche.LendingPositionResult{}, tt.readerErr
				}

				snapshot := avalanche.LendingPositionResult{
					Protocol:      "Aave V3",
					PoolAddress:   "0x0000000000000000000000000000000000000010",
					WalletAddress: wallet,
					Provenance: avalanche.Provenance{
						Source:      "aave_test_fixture",
						ChainID:     43113,
						BlockNumber: &block,
						ObservedAt:  observedAt,
						IsDemo:      true,
					},
				}

				if tt.hasDebt {
					asset := avalanche.Asset{ChainID: 43113, Address: token, Decimals: 6}
					hf := hfRaw
					snapshot.HealthFactorRaw = &hf
					snapshot.Positions = []avalanche.LendingAssetPosition{{
						Supplied: avalanche.TokenAmount{
							Asset: asset, Raw: "2500000", Formatted: "2.5",
						},
						StableDebt: avalanche.TokenAmount{
							Asset: asset, Raw: "0", Formatted: "0",
						},
						VariableDebt: avalanche.TokenAmount{
							Asset: asset, Raw: "500000", Formatted: "0.5",
						},
						CollateralEnabled: true,
					}}
				}

				return snapshot, nil
			})

			tool, err := NewGetLendingPositionTool(reader)
			if err != nil {
				t.Fatal(err)
			}

			args, err := json.Marshal(map[string]any{
				"wallet_address": wallet,
				"chain_id":       tt.chainID,
			})
			if err != nil {
				t.Fatal(err)
			}

			result, err := tool.Execute(context.Background(), args)
			if err != nil {
				t.Fatal(err)
			}
			if calls != tt.wantCalls {
				t.Fatalf("reader calls = %d; want %d", calls, tt.wantCalls)
			}

			if tt.wantCode != "" {
				if result.Error == nil || result.Error.Code != tt.wantCode {
					t.Fatalf("want tool error %s, got %+v", tt.wantCode, result.Error)
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

			var data avalanche.LendingPositionResult
			if err := json.Unmarshal(result.Data, &data); err != nil {
				t.Fatal(err)
			}

			if !tt.hasDebt {
				// An empty JSON array decodes to a non-nil empty slice.
				if data.Positions == nil || len(data.Positions) != 0 ||
					data.HealthFactorRaw != nil {
					t.Fatalf("want positions=[] with no HF, got %s", result.Data)
				}
				return
			}

			if len(data.Positions) != 1 ||
				data.Positions[0].VariableDebt.Raw != "500000" ||
				!data.Positions[0].CollateralEnabled ||
				data.HealthFactorRaw == nil ||
				*data.HealthFactorRaw != hfRaw {
				t.Fatalf("unexpected debt or protocol HF: %s", result.Data)
			}
		})
	}
}
