package ai

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

type swapPrepareTestRecorder struct {
	*InMemoryStore
	pendingWrites       int
	saved               *PendingAction
	saveErr             error
	existingPendingSwap *PendingAction
}

func (r *swapPrepareTestRecorder) RecordTurn(_ context.Context, record TurnRecord) error {
	if record.PendingAction == nil {
		return nil
	}

	r.pendingWrites++
	if r.saveErr != nil {
		return r.saveErr
	}

	copy := *record.PendingAction
	copy.Arguments = append(json.RawMessage(nil), copy.Arguments...)
	r.saved = &copy
	return nil
}

func TestAgentPrepareSwapPendingAction(t *testing.T) {
	saveErr := errors.New("recorder write failed")
	proposed := json.RawMessage(`{"amount_in_raw":"1000000"}`)
	prepared := json.RawMessage(`{"transaction":{"data":"0x1234"},"summary":"fixture only"}`)
	expiry := time.Now().UTC().Add(time.Minute).Truncate(time.Second)

	for _, tc := range []struct {
		name         string
		status       string
		disabled     bool
		saveErr      error
		wantWrites   int
		wantCallback int
		wantPending  bool
	}{
		{name: "missing callback", disabled: true},
		{name: "approval required", status: "approval_required", wantCallback: 1},
		{name: "blocked", status: "blocked", wantCallback: 1},
		{name: "reverted", status: "reverted", wantCallback: 1},
		{
			name: "ready and saved", status: "ready_for_review",
			wantWrites: 1, wantCallback: 1, wantPending: true,
		},
		{
			name:        "retry returns existing pending action",
			wantPending: true,
		},
		{
			name: "ready but save fails", status: "ready_for_review",
			saveErr: saveErr, wantWrites: 1, wantCallback: 1,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := &swapPrepareTestRecorder{
				InMemoryStore: NewInMemoryStore(),
				saveErr:       tc.saveErr,
			}

			if tc.name == "retry returns existing pending action" {
				recorder.existingPendingSwap = &PendingAction{
					PendingActionID:      "existing-swap-action",
					ToolName:             "prepare_swap",
					RequiresConfirmation: true,
				}
			}

			registry := &mockToolRegistry{}
			policy := NewStaticPolicyEngine(nil, []string{"prepare_swap"})
			llm := NewMockLLMClient(MockLLMStep{
				Response: LLMResponse{
					ToolCalls: []ToolCall{{
						ID:        "swap-test-call",
						Name:      "prepare_swap",
						Arguments: proposed,
					}},
				},
			})

			callbacks := 0
			callback := func(
				_ context.Context,
				wallet string,
				chainID uint64,
				args json.RawMessage,
			) (SwapPreparation, error) {
				callbacks++
				if !strings.EqualFold(wallet, testWalletAddress) ||
					chainID != uint64(FujiChainID) ||
					string(args) != string(proposed) {
					t.Fatalf("unexpected preparation inputs: %s %d %s", wallet, chainID, args)
				}

				// Include a payload even in blocked cases to prove it
				// is not returned or saved as a pending action.
				return SwapPreparation{
					Status:    tc.status,
					Arguments: prepared,
					ExpiresAt: expiry,
				}, nil
			}

			var agent *Agent
			var err error
			if tc.disabled {
				agent, err = NewAgent(llm, registry, recorder, policy)
			} else {
				agent, err = NewAgentWithSwapPreparation(
					llm, registry, recorder, policy, callback,
				)
			}
			if err != nil {
				t.Fatal(err)
			}

			got, err := agent.Run(context.Background(), AgentInput{
				RequestID:      "request-swap-test",
				IdempotencyKey: "swap-test-key",
				SessionID:      "session-swap-test",
				WalletAddress:  testWalletAddress,
				ChainID:        FujiChainID,
				Message:        "Prepare an exact-input swap on Avalanche Fuji.",
				Locale:         "en",
			})

			if callbacks != tc.wantCallback || recorder.pendingWrites != tc.wantWrites {
				t.Fatalf("callbacks=%d, pending writes=%d, Run error=%v", callbacks, recorder.pendingWrites, err)
			}
			if registry.callCount != 0 {
				t.Fatal("prepare_swap must not execute through the read-tool registry")
			}
			if tc.disabled {
				if err == nil || !strings.Contains(err.Error(), "swap preparation is not enabled") {
					t.Fatalf("expected disabled preparation error, got %v", err)
				}
			} else if tc.saveErr != nil {
				if !errors.Is(err, tc.saveErr) {
					t.Fatalf("expected recorder error, got %v", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}

			if (got.PendingAction != nil) != tc.wantPending {
				t.Fatalf("pending action = %+v; want present=%t", got.PendingAction, tc.wantPending)
			}
			if recorder.existingPendingSwap != nil {
				if got.PendingAction.PendingActionID != recorder.existingPendingSwap.PendingActionID {
					t.Fatalf("retry returned action ID %q, want %q",
						got.PendingAction.PendingActionID,
						recorder.existingPendingSwap.PendingActionID,
					)
				}
				if calls := len(llm.RecordedRequests()); calls != 0 {
					t.Fatalf("retry called the LLM %d time(s), want 0", calls)
				}
				return
			}
			if !tc.wantPending {
				if recorder.saved != nil || got.RequiresConfirmation ||
					strings.Contains(got.Message, "0x1234") {
					t.Fatal("non-ready swap must not expose or persist a prepared action")
				}
				return
			}

			if !got.RequiresConfirmation || recorder.saved == nil {
				t.Fatal("ready swap must be saved and require confirmation")
			}
			if string(recorder.saved.Arguments) != string(prepared) ||
				string(got.PendingAction.Arguments) != string(prepared) {
				t.Fatal("pending action must use the backend payload, not model arguments")
			}
			if recorder.saved.ExpiresAt != expiry.Format(time.RFC3339Nano) {
				t.Fatalf("unexpected saved expiry: %s", recorder.saved.ExpiresAt)
			}
		})
	}
}

func (r *swapPrepareTestRecorder) LookupPendingSwap(
	_ context.Context,
	_, _, _ string,
) (*PendingAction, error) {
	return r.existingPendingSwap, nil
}
