package database

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"unihack-backend/internal/ai"

	"github.com/google/uuid"
)

func TestRecordTurnConcurrentSwapIdempotency(t *testing.T) {
	if os.Getenv("DB_INTEGRATION_TEST") != "1" {
		t.Skip("set DB_INTEGRATION_TEST=1 to run PostgreSQL integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	db, err := ConnectFromEnv(ctx)
	if err != nil {
		t.Fatalf("connect to PostgreSQL: %v", err)
	}
	defer db.Close()

	store, err := NewPostgresMemoryStore(db)
	if err != nil {
		t.Fatal(err)
	}

	const wallet = "0x0000000000000000000000000000000000000001"
	const key = "same-swap-retry"
	sessionID := "swap-idempotency-test-" + uuid.NewString()
	fingerprint := strings.Repeat("a", 64)

	if err := store.EnsureSession(ctx, ai.SessionMetadata{
		SessionID:     sessionID,
		WalletAddress: wallet,
		ChainID:       ai.FujiChainID,
		Locale:        "en",
	}); err != nil {
		t.Fatalf("create test session: %v", err)
	}

	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if _, err := db.ExecContext(cleanupCtx,
			"DELETE FROM sessions WHERE session_id = $1", sessionID,
		); err != nil {
			t.Errorf("clean test session: %v", err)
		}
	}()

	makeTurn := func(payload string) ai.TurnRecord {
		requestID := uuid.NewString()
		return ai.TurnRecord{
			SessionID: sessionID,
			Message: ai.Message{
				RequestID: requestID,
				Role:      ai.RoleAssistant,
				Content:   "Review this test fixture.",
			},
			PendingAction: &ai.PendingAction{
				RequestID:            requestID,
				ToolCallID:           "prepare-swap-test",
				ToolName:             "prepare_swap",
				Arguments:            json.RawMessage(payload),
				ChainID:              ai.FujiChainID,
				WalletAddress:        wallet,
				RequiresConfirmation: true,
				IdempotencyKey:       key,
				RequestFingerprint:   fingerprint,
			},
		}
	}

	first := makeTurn(`{"fixture":"first"}`)
	second := makeTurn(`{"fixture":"second"}`)
	firstRequestID := first.Message.RequestID
	secondRequestID := second.Message.RequestID

	start := make(chan struct{})
	results := make(chan error, 2)
	for _, turn := range []ai.TurnRecord{first, second} {
		go func(turn ai.TurnRecord) {
			<-start
			results <- store.RecordTurn(ctx, turn)
		}(turn)
	}
	close(start)

	// Wait for both writes before checking rows or cleaning the session.
	err1 := <-results
	err2 := <-results
	if err1 != nil || err2 != nil {
		t.Fatalf("both writes must return successfully: %v / %v", err1, err2)
	}

	stored, err := store.LookupPendingSwap(ctx, sessionID, key, fingerprint)
	if err != nil || stored == nil {
		t.Fatalf("lookup winning action: action=%+v err=%v", stored, err)
	}
	if stored.PendingActionID == "" || stored.ExpiresAt == "" {
		t.Fatalf("stored action missing ID or expiry: %+v", stored)
	}

	for _, action := range []*ai.PendingAction{first.PendingAction, second.PendingAction} {
		// Compare decoded fixture values because JSONB may change whitespace.
		var got, want struct {
			Fixture string `json:"fixture"`
		}
		if err := json.Unmarshal(action.Arguments, &got); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(stored.Arguments, &want); err != nil {
			t.Fatal(err)
		}
		if action.PendingActionID != stored.PendingActionID ||
			action.RequestID != stored.RequestID ||
			action.NormalizedIntentHash != stored.NormalizedIntentHash ||
			action.ExpiresAt != stored.ExpiresAt ||
			got != want {
			t.Fatalf("retry did not return winning action: got=%+v stored=%+v", action, stored)
		}
	}

	winner := stored.RequestID
	var loser string
	switch winner {
	case firstRequestID:
		loser = secondRequestID
	case secondRequestID:
		loser = firstRequestID
	default:
		t.Fatalf("unexpected winning request ID: %s", winner)
	}

	for _, table := range []string{"messages", "tool_calls", "pending_actions"} {
		assertRequestRowCount(t, ctx, db, table, winner, 1)
		assertRequestRowCount(t, ctx, db, table, loser, 0)
	}

	// Use a distinct payload to exercise the key constraint, not the
	// existing normalized-intent uniqueness constraint.
	conflict := makeTurn(`{"fixture":"different request"}`)
	conflict.PendingAction.RequestFingerprint = strings.Repeat("b", 64)

	err = store.RecordTurn(ctx, conflict)
	if !errors.Is(err, ai.ErrSwapIdempotencyConflict) {
		t.Fatalf("expected idempotency conflict, got %v", err)
	}
	if conflict.PendingAction.PendingActionID != "" {
		t.Fatal("conflicting request must not receive an action ID")
	}
	for _, table := range []string{"messages", "tool_calls", "pending_actions"} {
		assertRequestRowCount(t, ctx, db, table, conflict.Message.RequestID, 0)
	}

	t.Run("reject requires matching scope and only one request wins", func(t *testing.T) {
		reject := func(scopeSessionID string) (bool, error) {
			return store.RejectPendingSwap(
				ctx,
				stored.PendingActionID,
				scopeSessionID,
				wallet,
				ai.FujiChainID,
				stored.NormalizedIntentHash,
			)
		}

		assertStatus := func(want string) {
			t.Helper()

			var got string
			err := db.QueryRowContext(ctx, `
            SELECT status
            FROM pending_actions
            WHERE pending_action_id = $1
        `, stored.PendingActionID).Scan(&got)
			if err != nil {
				t.Fatalf("read pending action status: %v", err)
			}
			if got != want {
				t.Fatalf("status = %q, want %q", got, want)
			}
		}

		changed, err := reject("other-session-" + uuid.NewString())
		if err != nil || changed {
			t.Fatalf("wrong-session reject: changed=%t, err=%v; want false, nil", changed, err)
		}
		assertStatus("pending")

		type rejectResult struct {
			changed bool
			err     error
		}

		start := make(chan struct{})
		results := make(chan rejectResult, 2)

		for i := 0; i < 2; i++ {
			go func() {
				<-start
				changed, err := reject(sessionID)
				results <- rejectResult{changed: changed, err: err}
			}()
		}
		close(start)

		// Đợi cả hai goroutine xong trước khi assert hoặc cleanup.
		first := <-results
		second := <-results

		if first.err != nil || second.err != nil {
			t.Fatalf("concurrent rejects returned errors: %v / %v", first.err, second.err)
		}
		if first.changed == second.changed {
			t.Fatalf("exactly one reject must win: changed=%t / %t", first.changed, second.changed)
		}
		assertStatus("rejected")
	})

	t.Run("confirm and reject race for the same pending action", func(t *testing.T) {
		turn := makeTurn(`{"fixture":"confirm-reject-race"}`)
		action := turn.PendingAction
		action.IdempotencyKey = "confirm-reject-race"

		if err := store.RecordTurn(ctx, turn); err != nil {
			t.Fatalf("create pending action: %v", err)
		}

		loaded, err := store.LoadPendingSwapForReview(
			ctx, action.PendingActionID, sessionID, wallet,
			ai.FujiChainID, action.NormalizedIntentHash,
		)
		if err != nil || loaded == nil {
			t.Fatalf("load matching review: action=%+v err=%v", loaded, err)
		}
		if loaded.PendingActionID != action.PendingActionID {
			t.Fatalf("loaded action ID = %q, want %q",
				loaded.PendingActionID, action.PendingActionID)
		}

		stale, err := store.LoadPendingSwapForReview(
			ctx, action.PendingActionID, sessionID, wallet,
			ai.FujiChainID, strings.Repeat("0", 64),
		)
		if err != nil || stale != nil {
			t.Fatalf("load stale review: action=%+v err=%v; want nil, nil", stale, err)
		}
		owner, err := store.LookupPendingSwapOwner(
			ctx, action.PendingActionID, sessionID,
			ai.FujiChainID, action.NormalizedIntentHash,
		)
		if err != nil || !strings.EqualFold(owner, wallet) {
			t.Fatalf("lookup owner: wallet=%q err=%v; want %q", owner, err, wallet)
		}

		otherOwner, err := store.LookupPendingSwapOwner(
			ctx, action.PendingActionID, "other-session-"+uuid.NewString(),
			ai.FujiChainID, action.NormalizedIntentHash,
		)
		if err != nil || otherOwner != "" {
			t.Fatalf("wrong-session lookup: wallet=%q err=%v; want empty wallet and nil error",
				otherOwner, err)
		}

		type result struct {
			status  string
			changed bool
			err     error
		}

		start := make(chan struct{})
		results := make(chan result, 2)

		go func() {
			<-start
			changed, err := store.ConfirmPendingSwap(
				ctx, action.PendingActionID, sessionID, wallet,
				ai.FujiChainID, action.NormalizedIntentHash,
			)
			results <- result{status: "confirmed", changed: changed, err: err}
		}()
		go func() {
			<-start
			changed, err := store.RejectPendingSwap(
				ctx, action.PendingActionID, sessionID, wallet,
				ai.FujiChainID, action.NormalizedIntentHash,
			)
			results <- result{status: "rejected", changed: changed, err: err}
		}()
		close(start)

		first := <-results
		second := <-results
		if first.err != nil || second.err != nil {
			t.Fatalf("transition errors: %v / %v", first.err, second.err)
		}
		if first.changed == second.changed {
			t.Fatalf("exactly one transition must win: %+v / %+v", first, second)
		}

		winner := first
		if second.changed {
			winner = second
		}

		var status string
		err = db.QueryRowContext(ctx, `
		SELECT status FROM pending_actions WHERE pending_action_id = $1
	`, action.PendingActionID).Scan(&status)
		if err != nil {
			t.Fatalf("read final status: %v", err)
		}
		if status != winner.status {
			t.Fatalf("final status = %q, want %q", status, winner.status)
		}
	})
	t.Run("only one confirmed action can attach the same transaction hash", func(t *testing.T) {
		first := makeTurn(`{"fixture":"attach-hash-first"}`)
		second := makeTurn(`{"fixture":"attach-hash-second"}`)
		first.PendingAction.IdempotencyKey = "attach-hash-first"
		second.PendingAction.IdempotencyKey = "attach-hash-second"

		actions := []*ai.PendingAction{first.PendingAction, second.PendingAction}
		for _, turn := range []ai.TurnRecord{first, second} {
			if err := store.RecordTurn(ctx, turn); err != nil {
				t.Fatalf("create pending action: %v", err)
			}

			action := turn.PendingAction
			changed, err := store.ConfirmPendingSwap(
				ctx, action.PendingActionID, sessionID, wallet,
				ai.FujiChainID, action.NormalizedIntentHash,
			)
			if err != nil || !changed {
				t.Fatalf("confirm fixture: changed=%t, err=%v", changed, err)
			}
		}

		// Two UUIDs without hyphens give 64 hex characters.
		txHash := "0x" + strings.ReplaceAll(
			uuid.NewString()+uuid.NewString(), "-", "",
		)

		type attachResult struct {
			actionID string
			changed  bool
			err      error
		}

		start := make(chan struct{})
		results := make(chan attachResult, 2)

		for _, action := range actions {
			go func(action *ai.PendingAction) {
				<-start
				changed, err := store.AttachVerifiedPendingSwapHash(
					ctx, action.PendingActionID, sessionID, wallet,
					ai.FujiChainID, action.NormalizedIntentHash, txHash,
				)
				results <- attachResult{action.PendingActionID, changed, err}
			}(action)
		}
		close(start)

		// Collect both results before assertions or test cleanup.
		outcomes := []attachResult{<-results, <-results}
		winners := 0

		for _, outcome := range outcomes {
			wantStatus := "confirmed"
			if outcome.changed {
				if outcome.err != nil {
					t.Fatalf("winning attach returned an error: %v", outcome.err)
				}
				winners++
				wantStatus = "submitted"
			} else if !errors.Is(outcome.err, ErrPendingSwapHashAlreadyUsed) {
				t.Fatalf("losing attach error = %v, want duplicate hash error", outcome.err)
			}

			var status string
			var storedHash *string
			if err := db.QueryRowContext(ctx, `
			SELECT status, transaction_hash
			FROM pending_actions
			WHERE pending_action_id = $1
		`, outcome.actionID).Scan(&status, &storedHash); err != nil {
				t.Fatalf("read action after attach: %v", err)
			}

			if status != wantStatus {
				t.Fatalf("status = %q, want %q", status, wantStatus)
			}
			if outcome.changed {
				if storedHash == nil || *storedHash != txHash {
					t.Fatal("winning action must store the transaction hash")
				}
			} else if storedHash != nil {
				t.Fatal("losing action must keep a NULL transaction hash")
			}
			var reviewHash string
			for _, action := range actions {
				if action.PendingActionID == outcome.actionID {
					reviewHash = action.NormalizedIntentHash
					break
				}
			}

			loaded, loadedStatus, loadedHash, err := store.LoadConfirmedSwapForReceipt(
				ctx, outcome.actionID, sessionID, wallet, ai.FujiChainID, reviewHash,
			)
			if err != nil || loaded == nil || loadedStatus != wantStatus {
				t.Fatalf("load receipt action: action=%+v status=%q err=%v",
					loaded, loadedStatus, err)
			}

			wantHash := ""
			if outcome.changed {
				wantHash = txHash
			}
			if loadedHash != wantHash {
				t.Fatalf("loaded transaction hash = %q, want %q", loadedHash, wantHash)
			}
			if outcome.changed {
				wrongHash := "0x" + strings.ReplaceAll(
					uuid.NewString()+uuid.NewString(), "-", "",
				)
				finalized, err := store.FinalizeVerifiedPendingSwapReceipt(
					ctx, outcome.actionID, sessionID, wallet, ai.FujiChainID,
					reviewHash, wrongHash, "mined",
				)
				if err != nil || finalized {
					t.Fatalf("wrong hash finalize: changed=%t err=%v; want false, nil",
						finalized, err)
				}

				finalized, err = store.FinalizeVerifiedPendingSwapReceipt(
					ctx, outcome.actionID, sessionID, wallet, ai.FujiChainID,
					reviewHash, txHash, "mined",
				)
				if err != nil || !finalized {
					t.Fatalf("matching hash finalize: changed=%t err=%v; want true, nil",
						finalized, err)
				}

				// A terminal action must not be overwritten by a later call.
				finalized, err = store.FinalizeVerifiedPendingSwapReceipt(
					ctx, outcome.actionID, sessionID, wallet, ai.FujiChainID,
					reviewHash, txHash, "failed",
				)
				if err != nil || finalized {
					t.Fatalf("terminal action overwrite: changed=%t err=%v; want false, nil",
						finalized, err)
				}

				_, finalStatus, finalHash, err := store.LoadConfirmedSwapForReceipt(
					ctx, outcome.actionID, sessionID, wallet, ai.FujiChainID, reviewHash,
				)
				if err != nil || finalStatus != "mined" || finalHash != txHash {
					t.Fatalf("final state: status=%q hash=%q err=%v", finalStatus, finalHash, err)
				}
			}
		}

		if winners != 1 {
			t.Fatalf("winning attaches = %d, want 1", winners)
		}
	})
}
