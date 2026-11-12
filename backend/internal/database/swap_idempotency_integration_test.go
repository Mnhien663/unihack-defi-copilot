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
}
