package avalanche

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadRPCSnapshot(t *testing.T) {
	snapshot, err := LoadRPCSnapshot(
		"testdata/fuji_rpc_snapshot_2026-10-28.json",
	)
	if err != nil {
		t.Fatal(err)
	}

	if snapshot.ChainID != uint64(FujiChainID) {
		t.Error("unexpected chain ID")
	}
	if snapshot.BlockNumber == nil ||
		*snapshot.BlockNumber != 60155048 {
		t.Fatal("snapshot block number was not preserved")
	}

	wantTime := time.Date(2026, 10, 28, 15, 11, 21, 0, time.UTC)
	if !snapshot.ObservedAt.Equal(wantTime) {
		t.Errorf("observation time = %v, want %v",
			snapshot.ObservedAt, wantTime)
	}

	if snapshot.OwnerAddress !=
		"0x4DFb0d91C07A7C706F65F974953cE8b111D27Ee5" {
		t.Error("unexpected owner address")
	}
	if snapshot.TokenAddress !=
		"0xcCCb83Fbe349fAebDE74Ce633D25B282E6FDd21A" {
		t.Error("unexpected token address")
	}
	if snapshot.SpenderAddress != snapshot.OwnerAddress {
		t.Error("unexpected spender address")
	}

	result := snapshot.AdapterResult
	if result.NativeBalanceRaw != "99999999831919520" {
		t.Error("native balance was not preserved")
	}
	if result.TokenBalanceRaw != "0" {
		t.Error("token balance was not preserved")
	}
	if result.AllowanceRaw != "0" {
		t.Error("allowance was not preserved")
	}
	if result.Decimals == nil || *result.Decimals != 18 {
		t.Error("unexpected token decimals")
	}

	p := snapshot.Provenance
	if p.Source != "Avalanche Fuji RPC" ||
		p.SourceType != "rpc" ||
		p.ChainID != uint64(FujiChainID) ||
		p.Mode != "snapshot" ||
		!p.IsDemo {
		t.Fatalf("unexpected snapshot provenance: %+v", p)
	}
	if p.BlockNumber == nil || *p.BlockNumber != 60155048 {
		t.Error("provenance block number was not preserved")
	}
	if !p.ObservedAt.Equal(wantTime) {
		t.Error("provenance observation time was not preserved")
	}
}

func TestLoadRPCSnapshotRejectsInvalidData(t *testing.T) {
	fixture, err := os.ReadFile(
		"testdata/fuji_rpc_snapshot_2026-10-28.json",
	)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		change  func(map[string]any)
		wantErr string
	}{
		{
			name: "wrong chain",
			change: func(data map[string]any) {
				data["chain_id"] = 43114
			},
			wantErr: "unexpected chain ID",
		},
		{
			name: "missing block",
			change: func(data map[string]any) {
				delete(data, "block_number")
			},
			wantErr: "missing block number",
		},
		{
			name: "missing observation time",
			change: func(data map[string]any) {
				delete(data, "observed_at")
			},
			wantErr: "missing observation time",
		},
		{
			name: "invalid token address",
			change: func(data map[string]any) {
				data["token_address"] = "invalid"
			},
			wantErr: "invalid token address",
		},
		{
			name: "missing decimals",
			change: func(data map[string]any) {
				result := data["adapter_result"].(map[string]any)
				delete(result, "decimals")
			},
			wantErr: "missing or invalid decimals",
		},
		{
			name: "decimals exceeds limit",
			change: func(data map[string]any) {
				result := data["adapter_result"].(map[string]any)
				result["decimals"] = 256
			},
			wantErr: "missing or invalid decimals",
		},
		{
			name: "negative balance",
			change: func(data map[string]any) {
				result := data["adapter_result"].(map[string]any)
				result["native_balance_raw"] = "-1"
			},
			wantErr: "invalid native balance raw amount",
		},
		{
			name: "fractional balance",
			change: func(data map[string]any) {
				result := data["adapter_result"].(map[string]any)
				result["token_balance_raw"] = "1.5"
			},
			wantErr: "invalid token balance raw amount",
		},
		{
			name: "uint256 overflow",
			change: func(data map[string]any) {
				result := data["adapter_result"].(map[string]any)
				result["allowance_raw"] =
					"115792089237316195423570985008687907853269984665640564039457584007913129639936"
			},
			wantErr: "exceeds uint256",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var data map[string]any
			if err := json.Unmarshal(fixture, &data); err != nil {
				t.Fatal(err)
			}
			tt.change(data)

			body, err := json.Marshal(data)
			if err != nil {
				t.Fatal(err)
			}

			path := filepath.Join(t.TempDir(), "snapshot.json")
			if err := os.WriteFile(path, body, 0600); err != nil {
				t.Fatal(err)
			}

			snapshot, err := LoadRPCSnapshot(path)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error containing %q, got %v",
					tt.wantErr, err)
			}
			if snapshot != nil {
				t.Error("expected nil snapshot on error")
			}
		})
	}
}
