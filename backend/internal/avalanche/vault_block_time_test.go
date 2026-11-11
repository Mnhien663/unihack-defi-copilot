package avalanche

import (
	"context"
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/core/types"
)

type vaultBlockTimeTestRPC struct {
	RPCReader
	header *types.Header
	err    error
	block  *big.Int
}

func (s *vaultBlockTimeTestRPC) HeaderByNumber(
	_ context.Context,
	block *big.Int,
) (*types.Header, error) {
	s.block = new(big.Int).Set(block)
	return s.header, s.err
}

func TestVaultBlockTime(t *testing.T) {
	block := big.NewInt(12345)
	wantTime := time.Date(2026, 11, 11, 13, 0, 0, 0, time.UTC)

	tests := []struct {
		name     string
		header   *types.Header
		err      error
		wantTime bool
	}{
		{
			name: "matching block",
			header: &types.Header{
				Number: big.NewInt(12345),
				Time:   uint64(wantTime.Unix()),
			},
			wantTime: true,
		},
		{
			name: "wrong block",
			header: &types.Header{
				Number: big.NewInt(12346),
				Time:   uint64(wantTime.Unix()),
			},
		},
		{name: "missing header"},
		{name: "header RPC error", err: errors.New("header unavailable")},
		{
			name:   "missing block timestamp",
			header: &types.Header{Number: big.NewInt(12345)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rpc := &vaultBlockTimeTestRPC{header: tt.header, err: tt.err}
			got := readVaultBlockTime(context.Background(), rpc, block)

			if rpc.block == nil || rpc.block.Cmp(block) != 0 {
				t.Fatalf("requested header for block %v; want %s", rpc.block, block)
			}

			if !tt.wantTime {
				if got != nil {
					t.Fatalf("block time = %v; want nil", got)
				}
				return
			}

			if got == nil || !got.Equal(wantTime) {
				t.Fatalf("block time = %v; want %v", got, wantTime)
			}

			// Reading the old block now must not make the data fresh.
			now := wantTime.Add(time.Hour)
			if freshness := AssessVaultFreshness(now, *got, 5*time.Minute); freshness != VaultFreshnessStale {
				t.Fatalf("freshness = %q; want stale", freshness)
			}
		})
	}

	t.Run("client without header support", func(t *testing.T) {
		client := struct{ RPCReader }{}
		if got := readVaultBlockTime(context.Background(), client, block); got != nil {
			t.Fatalf("block time = %v; want nil", got)
		}
	})
}
