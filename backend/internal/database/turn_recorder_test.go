package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"testing"
	"time"

	"unihack-backend/internal/ai"

	"github.com/google/uuid"
)

func TestRecordTurnCommitsAndRollsBackAtomically(
	t *testing.T,
) {
	if os.Getenv("DB_INTEGRATION_TEST") != "1" {
		t.Skip("set DB_INTEGRATION_TEST=1 to run PostgreSQL integration test")
	}

	ctx, cancel := context.WithTimeout(
		context.Background(),
		15*time.Second,
	)
	defer cancel()

	db, err := ConnectFromEnv(ctx)
	if err != nil {
		t.Fatalf("connect to PostgreSQL: %v", err)
	}
	defer db.Close()

	store, err := NewPostgresMemoryStore(db)
	if err != nil {
		t.Fatalf("create PostgreSQL memory store: %v", err)
	}

	sessionID := "transaction-" + uuid.NewString()
	walletAddress :=
		"0x0000000000000000000000000000000000000001"

	defer func() {
		if _, err := db.ExecContext(
			context.Background(),
			"DELETE FROM sessions WHERE session_id = $1",
			sessionID,
		); err != nil {
			t.Errorf("clean transaction test session: %v", err)
		}
	}()

	if err := store.EnsureSession(
		ctx,
		ai.SessionMetadata{
			SessionID:     sessionID,
			WalletAddress: walletAddress,
			ChainID:       ai.FujiChainID,
			Locale:        "en",
		},
	); err != nil {
		t.Fatalf("ensure transaction test session: %v", err)
	}

	arguments := json.RawMessage(`{
		"wallet_address":
			"0x0000000000000000000000000000000000000001",
		"chain_id": 43113,
		"amount": "1"
	}`)

	firstRequestID := uuid.NewString()

	firstPendingAction := &ai.PendingAction{
		RequestID:            firstRequestID,
		ToolCallID:           "write-call-001",
		ToolName:             "prepare_demo_transfer",
		Arguments:            arguments,
		ChainID:              ai.FujiChainID,
		WalletAddress:        walletAddress,
		RequiresConfirmation: true,
	}

	err = store.RecordTurn(
		ctx,
		ai.TurnRecord{
			SessionID: sessionID,
			Message: ai.Message{
				RequestID: firstRequestID,
				Role:      ai.RoleAssistant,
				Content:   "Review and confirm this action.",
			},
			PendingAction: firstPendingAction,
		},
	)
	if err != nil {
		t.Fatalf("record first pending turn: %v", err)
	}

	if firstPendingAction.PendingActionID == "" {
		t.Fatal("pending action ID was not populated")
	}

	if firstPendingAction.NormalizedIntentHash == "" {
		t.Fatal("normalized intent hash was not populated")
	}

	if firstPendingAction.ExpiresAt == "" {
		t.Fatal("pending action expiry was not populated")
	}

	// Same normalized intent in the same session violates the
	// idempotency constraint. The second message and tool call must
	// therefore be rolled back together with the failed pending action.
	secondRequestID := uuid.NewString()

	secondPendingAction := &ai.PendingAction{
		RequestID:            secondRequestID,
		ToolCallID:           "write-call-002",
		ToolName:             "prepare_demo_transfer",
		Arguments:            arguments,
		ChainID:              ai.FujiChainID,
		WalletAddress:        walletAddress,
		RequiresConfirmation: true,
	}

	err = store.RecordTurn(
		ctx,
		ai.TurnRecord{
			SessionID: sessionID,
			Message: ai.Message{
				RequestID: secondRequestID,
				Role:      ai.RoleAssistant,
				Content:   "This row must be rolled back.",
			},
			PendingAction: secondPendingAction,
		},
	)
	if err == nil {
		t.Fatal("expected duplicate intent transaction to fail")
	}

	assertRequestRowCount(
		t,
		ctx,
		db,
		"messages",
		firstRequestID,
		1,
	)

	assertRequestRowCount(
		t,
		ctx,
		db,
		"tool_calls",
		firstRequestID,
		1,
	)

	assertRequestRowCount(
		t,
		ctx,
		db,
		"pending_actions",
		firstRequestID,
		1,
	)

	assertRequestRowCount(
		t,
		ctx,
		db,
		"messages",
		secondRequestID,
		0,
	)

	assertRequestRowCount(
		t,
		ctx,
		db,
		"tool_calls",
		secondRequestID,
		0,
	)

	assertRequestRowCount(
		t,
		ctx,
		db,
		"pending_actions",
		secondRequestID,
		0,
	)
}

func assertRequestRowCount(
	t *testing.T,
	ctx context.Context,
	db queryRower,
	tableName string,
	requestID string,
	expected int,
) {
	t.Helper()

	allowedTables := map[string]bool{
		"messages":        true,
		"tool_calls":      true,
		"pending_actions": true,
	}

	if !allowedTables[tableName] {
		t.Fatalf("table %q is not allowed", tableName)
	}

	query := "SELECT COUNT(*) FROM " +
		tableName +
		" WHERE request_id = $1"

	var actual int

	if err := db.QueryRowContext(
		ctx,
		query,
		requestID,
	).Scan(&actual); err != nil {
		t.Fatalf(
			"count rows from %s: %v",
			tableName,
			err,
		)
	}

	if actual != expected {
		t.Fatalf(
			"%s row count for request %s: got %d, want %d",
			tableName,
			requestID,
			actual,
			expected,
		)
	}
}

type queryRower interface {
	QueryRowContext(
		ctx context.Context,
		query string,
		args ...any,
	) *sql.Row
}
