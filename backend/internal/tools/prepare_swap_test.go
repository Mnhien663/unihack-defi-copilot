package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"unihack-backend/internal/ai"
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
					intent.SlippageBps != 50 {
					t.Fatalf("unexpected service input: %+v", intent)
				}

				var blockers []string
				if tc.status == "blocked" {
					blockers = []string{"INSUFFICIENT_BUDGET"}
				}

				return avalanche.PrepareSwapResult{
					Status:      tc.status,
					Blockers:    blockers,
					SlippageBps: 50,
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
				`","amount_in_raw":"1000000","slippage_bps":50` + tc.extraField + `}`)

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
				SlippageBps *uint16                 `json:"slippage_bps"`
			}
			if err := json.Unmarshal(got.Arguments, &payload); err != nil {
				t.Fatal(err)
			}
			if payload.SlippageBps == nil || *payload.SlippageBps != 50 {
				t.Fatalf("expected slippage_bps=50 in prepared payload: %s", got.Arguments)
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

type confirmReviewTestStore struct {
	action       *ai.PendingAction
	changed      bool
	confirmCalls int
}

func (s *confirmReviewTestStore) LoadPendingSwapForReview(
	_ context.Context, _, _, _ string, _ uint64, _ string,
) (*ai.PendingAction, error) {
	return s.action, nil
}

func (s *confirmReviewTestStore) ConfirmPendingSwap(
	_ context.Context, _, _, _ string, _ uint64, _ string,
) (bool, error) {
	s.confirmCalls++
	return s.changed, nil
}

type confirmReviewTestRevalidator func(uint16) (bool, error)

func (f confirmReviewTestRevalidator) RevalidateReviewedSwap(
	_ context.Context,
	_ avalanche.UnsignedTx,
	_ avalanche.SwapTxSummary,
	slippageBps uint16,
) (bool, error) {
	return f(slippageBps)
}

func TestConfirmReviewedSwap(t *testing.T) {
	const withSlippage = `{"transaction":{},"summary":{},"slippage_bps":0}`
	const withoutSlippage = `{"transaction":{},"summary":{}}`

	for _, tc := range []struct {
		name           string
		payload        string
		needsReview    bool
		changed        bool
		wantErr        error
		wantRevalidate int
		wantConfirm    int
	}{
		{
			name:    "missing slippage requires fresh review",
			payload: withoutSlippage,
			wantErr: ErrSwapReviewRequired,
		},
		{
			name:           "changed quote does not confirm",
			payload:        withSlippage,
			needsReview:    true,
			wantErr:        ErrSwapReviewRequired,
			wantRevalidate: 1,
		},
		{
			name:           "lost DB transition does not return action",
			payload:        withSlippage,
			wantErr:        ErrPendingSwapNotConfirmable,
			wantRevalidate: 1,
			wantConfirm:    1,
		},
		{
			name:           "successful transition returns stored action",
			payload:        withSlippage,
			changed:        true,
			wantRevalidate: 1,
			wantConfirm:    1,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			action := &ai.PendingAction{
				PendingActionID: "action-test",
				ToolName:        PrepareSwapToolName,
				Arguments:       json.RawMessage(tc.payload),
			}
			store := &confirmReviewTestStore{
				action:  action,
				changed: tc.changed,
			}

			revalidateCalls := 0
			revalidator := confirmReviewTestRevalidator(func(bps uint16) (bool, error) {
				revalidateCalls++
				if bps != 0 {
					t.Fatalf("slippage = %d, want explicit zero from stored payload", bps)
				}
				if store.confirmCalls != 0 {
					t.Fatal("DB confirmation ran before revalidation")
				}
				return tc.needsReview, nil
			})

			got, err := ConfirmReviewedSwap(
				context.Background(), store, revalidator,
				action.PendingActionID, "session-test",
				"0x0000000000000000000000000000000000000001",
				43113, "review-hash-test",
			)

			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("error = %v, want %v", err, tc.wantErr)
			}
			if tc.wantErr != nil {
				if got != nil {
					t.Fatal("failed confirmation must not return an action")
				}
			} else if got != action {
				t.Fatal("confirmation must return the original stored action")
			}

			if revalidateCalls != tc.wantRevalidate || store.confirmCalls != tc.wantConfirm {
				t.Fatalf("revalidate calls = %d, confirm calls = %d; want %d / %d",
					revalidateCalls, store.confirmCalls, tc.wantRevalidate, tc.wantConfirm)
			}
		})
	}
}

type swapSubmissionTestStore struct {
	action      *ai.PendingAction
	status      string
	storedHash  string
	attachCalls int
}

func (s *swapSubmissionTestStore) LoadConfirmedSwapForReceipt(
	_ context.Context, _, _, _ string, _ uint64, _ string,
) (*ai.PendingAction, string, string, error) {
	return s.action, s.status, s.storedHash, nil
}

func (s *swapSubmissionTestStore) AttachVerifiedPendingSwapHash(
	_ context.Context, _, _, _ string, _ uint64, _, hash string,
) (bool, error) {
	s.attachCalls++
	s.storedHash = hash
	s.status = "submitted"
	return true, nil
}

type swapSubmissionReceiptStub func(string) (avalanche.ReceiptResult, error)

func (f swapSubmissionReceiptStub) ReadReceipt(
	_ context.Context, hash string,
) (avalanche.ReceiptResult, error) {
	return f(hash)
}

func TestRecordReviewedSwapSubmission(t *testing.T) {
	const wallet = "0x0000000000000000000000000000000000000001"
	const manager = "0x0000000000000000000000000000000000000002"

	txHash := "0x" + strings.Repeat("a", 64)
	to := manager
	reviewed := avalanche.UnsignedTx{
		ChainID: 43113,
		From:    wallet,
		To:      manager,
		Value:   "0",
		Data:    "0x12345678", // Fixture, không phải calldata swap thật.
	}
	payload, err := json.Marshal(struct {
		Transaction avalanche.UnsignedTx `json:"transaction"`
	}{Transaction: reviewed})
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name       string
		status     string
		storedHash string
		rpcInput   string
		wantStatus string
		wantErr    bool
		wantRPC    int
		wantAttach int
	}{
		{
			name:   "different calldata does not attach",
			status: "confirmed", rpcInput: "0x87654321",
			wantErr: true, wantRPC: 1,
		},
		{
			name:   "matching transaction attaches hash",
			status: "confirmed", rpcInput: reviewed.Data,
			wantStatus: "submitted", wantRPC: 1, wantAttach: 1,
		},
		{
			name:   "same hash retry does not read RPC or attach again",
			status: "submitted", storedHash: txHash,
			wantStatus: "submitted",
		},
		{
			name:   "different hash cannot replace an attached hash",
			status: "submitted", storedHash: "0x" + strings.Repeat("b", 64),
			wantErr: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &swapSubmissionTestStore{
				action:     &ai.PendingAction{Arguments: payload, ToolName: PrepareSwapToolName},
				status:     tc.status,
				storedHash: tc.storedHash,
			}
			rpcCalls := 0
			reader := swapSubmissionReceiptStub(func(hash string) (avalanche.ReceiptResult, error) {
				rpcCalls++
				if hash != txHash {
					t.Fatalf("RPC hash = %q, want %q", hash, txHash)
				}
				return avalanche.ReceiptResult{
					TransactionHash: txHash,
					FromAddress:     wallet,
					ToAddress:       &to,
					Input:           tc.rpcInput,
					ValueRaw:        "0",
					ChainID:         43113,
					Status:          avalanche.TransactionStatusPending,
				}, nil
			})

			status, err := RecordReviewedSwapSubmission(
				context.Background(), store, reader,
				"action-test", "session-test", wallet, 43113, "review-hash-test", txHash,
			)
			if (err != nil) != tc.wantErr || status != tc.wantStatus {
				t.Fatalf("status=%q err=%v; want status=%q, wantErr=%t",
					status, err, tc.wantStatus, tc.wantErr)
			}
			if tc.name == "different hash cannot replace an attached hash" &&
				!errors.Is(err, ErrSwapSubmissionHashMismatch) {
				t.Fatalf("error = %v, want ErrSwapSubmissionHashMismatch", err)
			}
			if rpcCalls != tc.wantRPC || store.attachCalls != tc.wantAttach {
				t.Fatalf("RPC/attach calls = %d/%d, want %d/%d",
					rpcCalls, store.attachCalls, tc.wantRPC, tc.wantAttach)
			}

			wantHash := tc.storedHash
			if tc.wantAttach == 1 {
				wantHash = txHash
			}
			if store.storedHash != wantHash {
				t.Fatalf("stored hash = %q, want %q", store.storedHash, wantHash)
			}
		})
	}
}

type swapReceiptRefreshTestStore struct {
	swapSubmissionTestStore
	finalizeCalls int
	finalizedHash string
}

func (s *swapReceiptRefreshTestStore) FinalizeVerifiedPendingSwapReceipt(
	_ context.Context, _, _, _ string, _ uint64, _, hash, outcome string,
) (bool, error) {
	s.finalizeCalls++
	s.finalizedHash = hash
	s.status = outcome
	return true, nil
}

func TestRefreshReviewedSwapReceipt(t *testing.T) {
	const wallet = "0x0000000000000000000000000000000000000001"
	const manager = "0x0000000000000000000000000000000000000002"
	const calldata = "0x12345678" // Fixture, không phải calldata swap thật.

	txHash := "0x" + strings.Repeat("a", 64)
	blockHash := "0x" + strings.Repeat("b", 64)
	blockNumber := uint64(100)
	to := manager

	payload, err := json.Marshal(struct {
		Transaction avalanche.UnsignedTx `json:"transaction"`
	}{
		Transaction: avalanche.UnsignedTx{
			ChainID: 43113,
			From:    wallet,
			To:      manager,
			Value:   "0",
			Data:    calldata,
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name         string
		rpcStatus    avalanche.TransactionStatus
		input        string
		hasBlock     bool
		wantStatus   string
		wantError    bool
		wantFinalize int
	}{
		{
			name:       "pending stays submitted without writing",
			rpcStatus:  avalanche.TransactionStatusPending,
			input:      calldata,
			wantStatus: "submitted",
		},
		{
			name:         "successful receipt marks mined",
			rpcStatus:    avalanche.TransactionStatusSucceeded,
			input:        calldata,
			hasBlock:     true,
			wantStatus:   "mined",
			wantFinalize: 1,
		},
		{
			name:         "reverted receipt marks failed",
			rpcStatus:    avalanche.TransactionStatusFailed,
			input:        calldata,
			hasBlock:     true,
			wantStatus:   "failed",
			wantFinalize: 1,
		},
		{
			name:      "different calldata does not finalize",
			rpcStatus: avalanche.TransactionStatusSucceeded,
			input:     "0x87654321",
			hasBlock:  true,
			wantError: true,
		},
		{
			name:      "success without block identity does not finalize",
			rpcStatus: avalanche.TransactionStatusSucceeded,
			input:     calldata,
			wantError: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &swapReceiptRefreshTestStore{
				swapSubmissionTestStore: swapSubmissionTestStore{
					action:     &ai.PendingAction{Arguments: payload, ToolName: PrepareSwapToolName},
					status:     "submitted",
					storedHash: txHash,
				},
			}
			reader := swapSubmissionReceiptStub(func(hash string) (avalanche.ReceiptResult, error) {
				if hash != txHash {
					t.Fatalf("RPC hash = %q, want stored hash %q", hash, txHash)
				}

				result := avalanche.ReceiptResult{
					TransactionHash: txHash,
					FromAddress:     wallet,
					ToAddress:       &to,
					Input:           tc.input,
					ValueRaw:        "0",
					ChainID:         43113,
					Status:          tc.rpcStatus,
				}
				if tc.hasBlock {
					result.BlockNumber = &blockNumber
					result.BlockHash = &blockHash
				}
				return result, nil
			})

			got, err := RefreshReviewedSwapReceipt(
				context.Background(), store, reader,
				"action-test", "session-test", wallet, 43113, "review-hash-test",
			)
			if (err != nil) != tc.wantError || got != tc.wantStatus {
				t.Fatalf("status=%q err=%v; want status=%q wantError=%t",
					got, err, tc.wantStatus, tc.wantError)
			}
			if store.finalizeCalls != tc.wantFinalize {
				t.Fatalf("finalize calls = %d, want %d", store.finalizeCalls, tc.wantFinalize)
			}
			if tc.wantFinalize == 0 {
				if store.status != "submitted" {
					t.Fatalf("action changed unexpectedly to %q", store.status)
				}
			} else if store.status != tc.wantStatus || store.finalizedHash != txHash {
				t.Fatalf("persisted status=%q hash=%q", store.status, store.finalizedHash)
			}
		})
	}
}

type depositReviewTestRevalidator func() (bool, error)

func (f depositReviewTestRevalidator) RevalidateReviewedDeposit(
	_ context.Context,
	_ avalanche.UnsignedTx,
	_ avalanche.DepositTxSummary,
) (bool, error) {
	return f()
}

func TestConfirmReviewedDeposit(t *testing.T) {
	for _, tc := range []struct {
		name          string
		enabled       bool
		needsReview   bool
		revalidateErr error
		changed       bool
		wantSuccess   bool
		wantCalls     int
		wantConfirm   int
	}{
		{
			name: "disabled deposit cannot confirm",
		},
		{
			name:        "changed preview requires fresh review",
			enabled:     true,
			needsReview: true,
			wantCalls:   1,
		},
		{
			name:          "revalidation timeout cannot confirm",
			enabled:       true,
			revalidateErr: context.DeadlineExceeded,
			wantCalls:     1,
		},
		{
			name:        "lost transition returns no action",
			enabled:     true,
			wantCalls:   1,
			wantConfirm: 1,
		},
		{
			name:        "successful deposit confirmation",
			enabled:     true,
			changed:     true,
			wantSuccess: true,
			wantCalls:   1,
			wantConfirm: 1,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			action := &ai.PendingAction{
				PendingActionID: "deposit-test",
				ToolName:        PrepareDepositToolName,
				Arguments: json.RawMessage(
					`{"transaction":{},"summary":{}}`,
				),
			}
			store := &confirmReviewTestStore{
				action:  action,
				changed: tc.changed,
			}

			calls := 0
			revalidator := ReviewedActionRevalidator{}
			if tc.enabled {
				revalidator.Deposit = depositReviewTestRevalidator(
					func() (bool, error) {
						calls++
						if store.confirmCalls != 0 {
							t.Fatal("confirmation ran before revalidation")
						}
						return tc.needsReview, tc.revalidateErr
					},
				)
			}

			got, err := ConfirmReviewedSwap(
				context.Background(), store, revalidator,
				action.PendingActionID, "session-test",
				"0x0000000000000000000000000000000000000001",
				43113, "review-hash-test",
			)

			if tc.wantSuccess {
				if err != nil || got != action {
					t.Fatalf("action=%v err=%v", got, err)
				}
			} else if err == nil || got != nil {
				t.Fatalf("failed confirmation returned action=%v err=%v", got, err)
			}

			if tc.needsReview && !errors.Is(err, ErrSwapReviewRequired) {
				t.Fatalf("expected fresh review, got %v", err)
			}
			if tc.revalidateErr != nil &&
				!errors.Is(err, tc.revalidateErr) {
				t.Fatalf("expected %v, got %v", tc.revalidateErr, err)
			}
			if calls != tc.wantCalls ||
				store.confirmCalls != tc.wantConfirm {
				t.Fatalf(
					"revalidate/confirm=%d/%d, want %d/%d",
					calls, store.confirmCalls,
					tc.wantCalls, tc.wantConfirm,
				)
			}
		})
	}
}

type depositPreparationCallbackStub func(
	context.Context,
	string,
	avalanche.PrepareDepositIntent,
) (avalanche.PrepareDepositResult, error)

func (f depositPreparationCallbackStub) PrepareDeposit(
	ctx context.Context,
	wallet string,
	intent avalanche.PrepareDepositIntent,
) (avalanche.PrepareDepositResult, error) {
	return f(ctx, wallet, intent)
}

func TestPrepareDepositCallbackRejectsInvalidArguments(t *testing.T) {
	service := depositPreparationCallbackStub(func(
		context.Context,
		string,
		avalanche.PrepareDepositIntent,
	) (avalanche.PrepareDepositResult, error) {
		t.Fatal("invalid arguments must not reach the service")
		return avalanche.PrepareDepositResult{}, nil
	})

	callback, err := NewPrepareDepositCallback(service)
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name string
		args string
	}{
		{"malformed JSON", `{`},
		{"missing assets", `{"min_shares_raw":"99"}`},
		{"missing minimum", `{"assets_raw":"100"}`},
		{"numeric amount", `{"assets_raw":100,"min_shares_raw":"99"}`},
		{"model calldata", `{"assets_raw":"100","min_shares_raw":"99","calldata":"0xdead"}`},
		{"model wallet", `{"assets_raw":"100","min_shares_raw":"99","wallet_address":"0xdead"}`},
		{"model chain", `{"assets_raw":"100","min_shares_raw":"99","chain_id":43114}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := callback(
				context.Background(),
				"0x0000000000000000000000000000000000000001",
				43113,
				json.RawMessage(tc.args),
			)
			if err == nil || len(got.Arguments) != 0 ||
				!got.ExpiresAt.IsZero() {
				t.Fatalf("expected rejection without payload: %+v, err=%v", got, err)
			}
		})
	}
}

func TestPrepareDepositCallback(t *testing.T) {
	if _, err := NewPrepareDepositCallback(nil); err == nil {
		t.Fatal("nil service must be rejected")
	}

	const wallet = "0x0000000000000000000000000000000000000001"
	const assets = "100000000000000000"
	const minimum = "99000000000000000"

	expiry := time.Now().UTC().Add(5 * time.Minute).Truncate(time.Second)
	upstreamErr := errors.New("deposit service unavailable")

	for _, tc := range []struct {
		name       string
		status     string
		blocker    string
		noTx       bool
		deadline   uint64
		preview    time.Time
		serviceErr error
		wantError  bool
	}{
		{
			name: "ready", status: "ready_for_review",
			deadline: uint64(expiry.Unix()), preview: expiry,
		},
		{
			name: "approval required", status: "approval_required",
			blocker: "ERC20_APPROVAL_REQUIRED",
		},
		{
			name: "blocked", status: "blocked",
			blocker: "INSUFFICIENT_BUDGET",
		},
		{
			name: "reverted", status: "reverted",
			blocker: "SIMULATION_REVERTED",
		},
		{
			name: "service error", serviceErr: upstreamErr,
			wantError: true,
		},
		{
			name: "unknown status", status: "unexpected",
			wantError: true,
		},
		{
			name: "ready without transaction", status: "ready_for_review",
			noTx: true, wantError: true,
		},
		{
			name: "ready with blocker", status: "ready_for_review",
			blocker: "INSUFFICIENT_BUDGET", wantError: true,
		},
		{
			name: "zero deadline", status: "ready_for_review",
			preview: expiry, wantError: true,
		},
		{
			name: "overflow deadline", status: "ready_for_review",
			deadline: uint64(1) << 63, preview: expiry, wantError: true,
		},
		{
			name: "expired", status: "ready_for_review",
			deadline: uint64(time.Now().Add(-time.Minute).Unix()),
			preview:  expiry, wantError: true,
		},
		{
			name: "deadline exceeds preview", status: "ready_for_review",
			deadline: uint64(expiry.Unix()),
			preview:  expiry.Add(-time.Second), wantError: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0

			// Callback fixture only; calldata validation belongs to the preparer.
			built := &avalanche.BuiltDepositTransaction{}
			built.Prepared.Tx = avalanche.UnsignedTx{
				ChainID: 43113,
				From:    wallet,
				To:      "0x0000000000000000000000000000000000000002",
				Value:   "0",
				Data:    "0x1234",
			}
			built.Summary.OwnerAddress = wallet
			built.Summary.Assets.Raw = assets
			built.Summary.MinShares.Raw = minimum
			built.Summary.Deadline = tc.deadline
			built.Summary.PreviewExpiresAt = tc.preview

			service := depositPreparationCallbackStub(func(
				_ context.Context,
				sessionWallet string,
				intent avalanche.PrepareDepositIntent,
			) (avalanche.PrepareDepositResult, error) {
				calls++
				if sessionWallet != wallet ||
					intent.WalletAddress != wallet ||
					intent.ChainID != 43113 ||
					intent.AssetsRaw != assets ||
					intent.MinSharesRaw != minimum {
					t.Fatalf("unexpected trusted context or amounts: %+v", intent)
				}

				result := avalanche.PrepareDepositResult{
					Status:      tc.status,
					Transaction: built,
				}
				if tc.blocker != "" {
					result.Blockers = []string{tc.blocker}
				}
				if tc.noTx {
					result.Transaction = nil
				}
				return result, tc.serviceErr
			})

			callback, err := NewPrepareDepositCallback(service)
			if err != nil {
				t.Fatal(err)
			}

			got, err := callback(
				context.Background(),
				wallet,
				43113,
				json.RawMessage(
					`{"assets_raw":"`+assets+`","min_shares_raw":"`+minimum+`"}`,
				),
			)

			if calls != 1 {
				t.Fatalf("service calls=%d; want 1", calls)
			}
			if tc.wantError {
				if err == nil || len(got.Arguments) != 0 ||
					!got.ExpiresAt.IsZero() {
					t.Fatalf("expected error without payload: %+v, err=%v", got, err)
				}
				if tc.serviceErr != nil && !errors.Is(err, tc.serviceErr) {
					t.Fatalf("service error was not preserved: %v", err)
				}
				return
			}
			if err != nil || got.Status != tc.status {
				t.Fatalf("unexpected result: %+v, err=%v", got, err)
			}

			if tc.status != "ready_for_review" {
				if len(got.Arguments) != 0 || !got.ExpiresAt.IsZero() ||
					len(got.Blockers) != 1 || got.Blockers[0] != tc.blocker {
					t.Fatalf("blocked action must retain blocker without payload: %+v", got)
				}
				return
			}

			var payload struct {
				Transaction avalanche.UnsignedTx       `json:"transaction"`
				Summary     avalanche.DepositTxSummary `json:"summary"`
			}
			if err := json.Unmarshal(got.Arguments, &payload); err != nil {
				t.Fatal(err)
			}
			if payload.Transaction != built.Prepared.Tx ||
				payload.Summary.OwnerAddress != wallet ||
				payload.Summary.Assets.Raw != assets ||
				payload.Summary.MinShares.Raw != minimum ||
				payload.Summary.Deadline != tc.deadline ||
				!payload.Summary.PreviewExpiresAt.Equal(expiry) ||
				!got.ExpiresAt.Equal(expiry) {
				t.Fatalf("prepared payload or exact amounts changed: %s", got.Arguments)
			}
		})
	}
}
