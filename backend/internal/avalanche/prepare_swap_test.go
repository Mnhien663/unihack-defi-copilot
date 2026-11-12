package avalanche

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"
)

type prepareSwapQuoteStub func(
	context.Context, SwapQuoteRequest,
) (SwapQuoteResult, error)

func (f prepareSwapQuoteStub) QuoteExactInput(
	ctx context.Context, req SwapQuoteRequest,
) (SwapQuoteResult, error) {
	return f(ctx, req)
}

type prepareSwapSimulatorStub func(
	context.Context, BuildSwapRequest,
) (SwapSimulationResult, error)

func (f prepareSwapSimulatorStub) SimulateSwap(
	ctx context.Context, req BuildSwapRequest,
) (SwapSimulationResult, error) {
	return f(ctx, req)
}

func TestSwapPreparerPrepareSwap(t *testing.T) {
	const wallet = "0x0000000000000000000000000000000000000001"
	const otherWallet = "0x0000000000000000000000000000000000000002"

	start := time.Date(2026, 11, 11, 15, 0, 0, 0, time.UTC)
	expiry := start.Add(30 * time.Second)

	tests := []struct {
		name             string
		status           string
		blockers         []string
		changeIntent     func(*PrepareSwapIntent)
		expireBeforeSim  bool
		expireDuringSim  bool
		wantStatus       string
		wantError        bool
		wantExpiredError bool
		wantQuoteCalls   int
		wantSimCalls     int
	}{
		{
			name: "ready for review", status: "succeeded",
			wantStatus: "ready_for_review", wantQuoteCalls: 1, wantSimCalls: 1,
		},
		{
			name: "approval only", status: "blocked",
			blockers:   []string{"ERC20_APPROVAL_REQUIRED"},
			wantStatus: "approval_required", wantQuoteCalls: 1, wantSimCalls: 1,
		},
		{
			name: "approval and budget missing", status: "blocked",
			blockers:   []string{"ERC20_APPROVAL_REQUIRED", "INSUFFICIENT_BUDGET"},
			wantStatus: "blocked", wantQuoteCalls: 1, wantSimCalls: 1,
		},
		{
			name: "simulation reverted", status: "reverted",
			wantStatus: "reverted", wantQuoteCalls: 1, wantSimCalls: 1,
		},
		{
			name: "expired before simulation", expireBeforeSim: true,
			wantError: true, wantExpiredError: true, wantQuoteCalls: 1,
		},
		{
			name: "expires during simulation", status: "succeeded",
			expireDuringSim: true, wantError: true, wantExpiredError: true,
			wantQuoteCalls: 1, wantSimCalls: 1,
		},
		{
			name: "wallet mismatch",
			changeIntent: func(intent *PrepareSwapIntent) {
				intent.WalletAddress = otherWallet
			},
			wantError: true,
		},
		{
			name: "amount above configured cap",
			changeIntent: func(intent *PrepareSwapIntent) {
				intent.AmountInRaw = "10000001"
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			now := start
			quoteCalls := 0
			simCalls := 0

			quoter := prepareSwapQuoteStub(func(
				_ context.Context, req SwapQuoteRequest,
			) (SwapQuoteResult, error) {
				quoteCalls++
				if len(req.TokenPath) != 2 ||
					req.TokenPath[0].Symbol != "USDT" ||
					req.TokenPath[1].Symbol != "USDC" ||
					req.AmountInRaw != "1000000" ||
					req.SlippageBps == nil || *req.SlippageBps != 50 {
					t.Fatalf("unexpected quote request: %+v", req)
				}

				if tt.expireBeforeSim {
					now = expiry
				}

				return SwapQuoteResult{
					TokenPath:   req.TokenPath,
					AmountIn:    TokenAmount{Raw: req.AmountInRaw},
					SlippageBps: *req.SlippageBps,
					ExpiresAt:   expiry,
				}, nil
			})

			simulator := prepareSwapSimulatorStub(func(
				_ context.Context, req BuildSwapRequest,
			) (SwapSimulationResult, error) {
				simCalls++
				if req.OwnerAddress != wallet ||
					req.Deadline != uint64(expiry.Unix()) ||
					!req.Quote.ExpiresAt.Equal(expiry) {
					t.Fatalf("unexpected simulation request: %+v", req)
				}

				if tt.expireDuringSim {
					now = expiry
				}

				affordable := true
				return SwapSimulationResult{
					Status: tt.status,
					Requirements: SwapRequirements{
						Blockers:      tt.blockers,
						GasAffordable: &affordable,
					},
					// Include a transaction even for blocked/reverted results
					// to verify the preparer does not pass it through.
					Transaction: BuiltSwapTransaction{
						Prepared: PreparedTransaction{
							Tx: UnsignedTx{
								ChainID: uint64(FujiChainID),
								From:    wallet,
								Value:   "0",
								Data:    "0x1234", // Placeholder, not valid swap calldata.
							},
						},
					},
				}, nil
			})

			preparer, err := NewSwapPreparer(quoter, simulator, "10000000")
			if err != nil {
				t.Fatal(err)
			}
			preparer.now = func() time.Time { return now }

			intent := PrepareSwapIntent{
				WalletAddress: wallet,
				ChainID:       uint64(FujiChainID),
				TokenIn:       lfjFujiTokenX,
				TokenOut:      lfjFujiTokenY,
				AmountInRaw:   "1000000",
				SlippageBps:   50,
			}
			if tt.changeIntent != nil {
				tt.changeIntent(&intent)
			}

			got, err := preparer.PrepareSwap(context.Background(), wallet, intent)
			if quoteCalls != tt.wantQuoteCalls || simCalls != tt.wantSimCalls {
				t.Fatalf("quote/simulation calls = %d/%d; want %d/%d",
					quoteCalls, simCalls, tt.wantQuoteCalls, tt.wantSimCalls)
			}

			if tt.wantError {
				if err == nil || got.Transaction != nil {
					t.Fatalf("expected error and no transaction; got %+v, err=%v", got, err)
				}
				if tt.wantExpiredError && !errors.Is(err, ErrQuoteExpired) {
					t.Fatalf("want ErrQuoteExpired; got %v", err)
				}
				return
			}

			if err != nil {
				t.Fatal(err)
			}
			if got.Status != tt.wantStatus || !slices.Equal(got.Blockers, tt.blockers) {
				t.Fatalf("unexpected status or blockers: %+v", got)
			}

			wantTx := tt.wantStatus == "ready_for_review"
			if (got.Transaction != nil) != wantTx {
				t.Fatalf("transaction present = %t; want %t", got.Transaction != nil, wantTx)
			}
			if wantTx && got.Transaction.Prepared.Tx.Data != "0x1234" {
				t.Fatal("expected the simulated transaction to be preserved")
			}
		})
	}
}
