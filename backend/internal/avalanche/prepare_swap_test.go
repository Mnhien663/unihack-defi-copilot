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

func TestRevalidateReviewedSwap(t *testing.T) {
	const wallet = "0x0000000000000000000000000000000000000001"
	const slippageBps uint16 = 50

	now := time.Date(2026, 11, 11, 15, 0, 0, 0, time.UTC)
	oldDeadline := uint64(now.Add(10 * time.Second).Unix())
	freshExpiry := now.Add(30 * time.Second)

	reviewedTx := UnsignedTx{
		ChainID: uint64(FujiChainID),
		From:    wallet,
		To:      "0x0000000000000000000000000000000000000002",
		Value:   "0",
		Data:    "0x1234", // Fixture, không phải calldata thật.
	}
	summary := SwapTxSummary{
		OwnerAddress: wallet,
		TokenIn:      Asset{Address: lfjFujiTokenX},
		TokenOut:     Asset{Address: lfjFujiTokenY},
		AmountIn:     TokenAmount{Raw: "1000000"},
		MinAmountOut: TokenAmount{Raw: "994900"},
		Deadline:     oldDeadline,
	}

	for _, tc := range []struct {
		name           string
		freshMinOut    string
		simulatedData  string
		wantReview     bool
		wantError      bool
		wantSimulation int
	}{
		{
			name:        "changed minimum requires review without simulation",
			freshMinOut: "994800",
			wantReview:  true,
		},
		{
			name:           "same transaction keeps original deadline",
			freshMinOut:    "994900",
			simulatedData:  "0x1234",
			wantSimulation: 1,
		},
		{
			name:           "different calldata is rejected",
			freshMinOut:    "994900",
			simulatedData:  "0x5678",
			wantError:      true,
			wantSimulation: 1,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			simCalls := 0
			quoter := prepareSwapQuoteStub(func(
				_ context.Context, req SwapQuoteRequest,
			) (SwapQuoteResult, error) {
				if req.AmountInRaw != "1000000" ||
					req.SlippageBps == nil || *req.SlippageBps != slippageBps {
					t.Fatalf("unexpected re-quote request: %+v", req)
				}
				return SwapQuoteResult{
					TokenPath:    req.TokenPath,
					AmountIn:     TokenAmount{Raw: req.AmountInRaw},
					MinAmountOut: TokenAmount{Raw: tc.freshMinOut},
					SlippageBps:  slippageBps,
					ExpiresAt:    freshExpiry,
				}, nil
			})

			simulator := prepareSwapSimulatorStub(func(
				_ context.Context, req BuildSwapRequest,
			) (SwapSimulationResult, error) {
				simCalls++
				if req.Deadline != oldDeadline {
					t.Fatalf("simulation deadline = %d, want original %d",
						req.Deadline, oldDeadline)
				}
				if req.OwnerAddress != wallet || req.Quote.MinAmountOut.Raw != "994900" {
					t.Fatalf("unexpected simulation request: %+v", req)
				}

				affordable := true
				tx := reviewedTx
				tx.Data = tc.simulatedData
				return SwapSimulationResult{
					Status: "succeeded",
					Requirements: SwapRequirements{
						GasAffordable: &affordable,
					},
					Transaction: BuiltSwapTransaction{
						Prepared: PreparedTransaction{Tx: tx},
					},
				}, nil
			})

			preparer, err := NewSwapPreparer(quoter, simulator, "10000000")
			if err != nil {
				t.Fatal(err)
			}
			preparer.now = func() time.Time { return now }

			needsReview, err := preparer.RevalidateReviewedSwap(
				context.Background(), reviewedTx, summary, slippageBps,
			)
			if (err != nil) != tc.wantError || needsReview != tc.wantReview {
				t.Fatalf("needsReview=%t, err=%v; want needsReview=%t, wantError=%t",
					needsReview, err, tc.wantReview, tc.wantError)
			}
			if simCalls != tc.wantSimulation {
				t.Fatalf("simulator calls = %d, want %d", simCalls, tc.wantSimulation)
			}
		})
	}
}
