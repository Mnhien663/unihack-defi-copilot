package tools

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"unihack-backend/internal/avalanche"
)

type swapPreparationCallbackStub func(
	context.Context,
	string,
	avalanche.PrepareSwapIntent,
) (avalanche.PrepareSwapResult, error)

func (f swapPreparationCallbackStub) PrepareSwap(
	ctx context.Context,
	wallet string,
	intent avalanche.PrepareSwapIntent,
) (avalanche.PrepareSwapResult, error) {
	return f(ctx, wallet, intent)
}

func TestPrepareSwapCallback(t *testing.T) {
	const wallet = "0x0000000000000000000000000000000000000001"
	const manager = "0x0000000000000000000000000000000000000002"
	const tokenIn = "0xAb231A5744C8E6c45481754928cCfFFFD4aa0732"
	const tokenOut = "0xB6076C93701D6a07266c31066B298AeC6dd65c2d"

	expiry := time.Now().UTC().Add(time.Minute).Truncate(time.Second)

	for _, tc := range []struct {
		name        string
		status      string
		deadline    time.Time
		quoteExpiry time.Time
		extraField  string
		wantError   bool
		wantCalls   int
	}{
		{"ready", "ready_for_review", expiry, expiry, "", false, 1},
		{"blocked", "blocked", expiry, expiry, "", false, 1},
		{"expired", "ready_for_review", expiry.Add(-2 * time.Minute), expiry, "", true, 1},
		{"deadline exceeds quote expiry", "ready_for_review", expiry, expiry.Add(-time.Second), "", true, 1},
		{"reject model calldata", "ready_for_review", expiry, expiry, `,"calldata":"0xdead"`, true, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			service := swapPreparationCallbackStub(func(
				_ context.Context,
				sessionWallet string,
				intent avalanche.PrepareSwapIntent,
			) (avalanche.PrepareSwapResult, error) {
				calls++
				if sessionWallet != wallet || intent.WalletAddress != wallet ||
					intent.ChainID != 43113 || intent.TokenIn != tokenIn ||
					intent.TokenOut != tokenOut || intent.AmountInRaw != "1000000" ||
					intent.SlippageBps != 0 {
					t.Fatalf("unexpected service input: %+v", intent)
				}

				var blockers []string
				if tc.status == "blocked" {
					blockers = []string{"INSUFFICIENT_BUDGET"}
				}

				return avalanche.PrepareSwapResult{
					Status:   tc.status,
					Blockers: blockers,
					Transaction: &avalanche.BuiltSwapTransaction{
						Prepared: avalanche.PreparedTransaction{
							Tx: avalanche.UnsignedTx{
								ChainID: 43113,
								From:    wallet,
								To:      manager,
								Value:   "0",
								Data:    "0x1234", // Fixture, not valid swap calldata.
							},
						},
						Summary: avalanche.SwapTxSummary{
							OwnerAddress:   wallet,
							ManagerAddress: manager,
							Deadline:       uint64(tc.deadline.Unix()),
							QuoteExpiresAt: tc.quoteExpiry,
						},
					},
				}, nil
			})

			callback, err := NewPrepareSwapCallback(service)
			if err != nil {
				t.Fatal(err)
			}

			args := json.RawMessage(`{"token_in":"` + tokenIn +
				`","token_out":"` + tokenOut +
				`","amount_in_raw":"1000000","slippage_bps":0` + tc.extraField + `}`)

			got, err := callback(context.Background(), wallet, 43113, args)
			if calls != tc.wantCalls {
				t.Fatalf("service calls = %d; want %d", calls, tc.wantCalls)
			}
			if tc.wantError {
				if err == nil || len(got.Arguments) != 0 || !got.ExpiresAt.IsZero() {
					t.Fatalf("want error without prepared payload; got %+v, err=%v", got, err)
				}
				return
			}
			if err != nil || got.Status != tc.status {
				t.Fatalf("unexpected result: %+v, err=%v", got, err)
			}

			if tc.status == "blocked" {
				if len(got.Arguments) != 0 || !got.ExpiresAt.IsZero() ||
					len(got.Blockers) != 1 || got.Blockers[0] != "INSUFFICIENT_BUDGET" {
					t.Fatalf("blocked result must retain its blocker without calldata: %+v", got)
				}
				return
			}

			var payload struct {
				Transaction avalanche.UnsignedTx    `json:"transaction"`
				Summary     avalanche.SwapTxSummary `json:"summary"`
			}
			if err := json.Unmarshal(got.Arguments, &payload); err != nil {
				t.Fatal(err)
			}

			if payload.Transaction.ChainID != 43113 ||
				payload.Transaction.From != wallet || payload.Transaction.To != manager ||
				payload.Transaction.Value != "0" || payload.Transaction.Data != "0x1234" ||
				payload.Summary.OwnerAddress != wallet ||
				payload.Summary.ManagerAddress != manager ||
				payload.Summary.Deadline != uint64(expiry.Unix()) ||
				!payload.Summary.QuoteExpiresAt.Equal(expiry) ||
				!got.ExpiresAt.Equal(expiry) {
				t.Fatalf("unexpected prepared payload or expiry: %s, %v", got.Arguments, got.ExpiresAt)
			}
		})
	}
}
