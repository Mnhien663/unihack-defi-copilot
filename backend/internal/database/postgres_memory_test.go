package database

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"unihack-backend/internal/ai"

	"github.com/google/uuid"
)

func TestPostgresMemoryStorePersistsAcrossReconnect(
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

	sessionID := "integration-" + uuid.NewString()
	requestID := uuid.NewString()

	metadata := ai.SessionMetadata{
		SessionID:     sessionID,
		WalletAddress: "0x0000000000000000000000000000000000000001",
		ChainID:       ai.FujiChainID,
		Locale:        "en",
	}

	db, err := ConnectFromEnv(ctx)
	if err != nil {
		t.Fatalf("connect to PostgreSQL: %v", err)
	}

	store, err := NewPostgresMemoryStore(db)
	if err != nil {
		db.Close()
		t.Fatalf("create PostgreSQL memory store: %v", err)
	}

	if err := store.EnsureSession(ctx, metadata); err != nil {
		db.Close()
		t.Fatalf("ensure session: %v", err)
	}

	messages := []ai.Message{
		{
			RequestID: requestID,
			Role:      ai.RoleUser,
			Content:   "Show my AVAX balance",
		},
		{
			RequestID: requestID,
			Role:      ai.RoleAssistant,
			Content:   "Your balance is available from the tool result.",
		},
	}

	for _, message := range messages {
		if err := store.Append(
			ctx,
			sessionID,
			message,
		); err != nil {
			db.Close()
			t.Fatalf("append message: %v", err)
		}
	}

	conflictingMetadata := metadata
	conflictingMetadata.WalletAddress =
		"0x0000000000000000000000000000000000000002"

	err = store.EnsureSession(ctx, conflictingMetadata)
	if !errors.Is(err, ErrSessionOwnershipMismatch) {
		db.Close()
		t.Fatalf(
			"expected session ownership mismatch, got %v",
			err,
		)
	}

	// Simulate backend shutdown.
	if err := db.Close(); err != nil {
		t.Fatalf("close first database pool: %v", err)
	}

	// Simulate backend restart.
	reconnectedDB, err := ConnectFromEnv(ctx)
	if err != nil {
		t.Fatalf("reconnect to PostgreSQL: %v", err)
	}
	defer reconnectedDB.Close()

	defer func() {
		if _, err := reconnectedDB.ExecContext(
			context.Background(),
			"DELETE FROM sessions WHERE session_id = $1",
			sessionID,
		); err != nil {
			t.Errorf("clean integration session: %v", err)
		}
	}()

	reconnectedStore, err := NewPostgresMemoryStore(reconnectedDB)
	if err != nil {
		t.Fatalf("create reconnected memory store: %v", err)
	}

	loaded, err := reconnectedStore.Load(ctx, sessionID)
	if err != nil {
		t.Fatalf("load persisted history: %v", err)
	}

	if len(loaded) != len(messages) {
		t.Fatalf(
			"unexpected message count: got %d, want %d",
			len(loaded),
			len(messages),
		)
	}

	for index := range messages {
		if loaded[index].RequestID != messages[index].RequestID {
			t.Errorf(
				"message %d request ID: got %q, want %q",
				index,
				loaded[index].RequestID,
				messages[index].RequestID,
			)
		}

		if loaded[index].Role != messages[index].Role {
			t.Errorf(
				"message %d role: got %q, want %q",
				index,
				loaded[index].Role,
				messages[index].Role,
			)
		}

		if loaded[index].Content != messages[index].Content {
			t.Errorf(
				"message %d content: got %q, want %q",
				index,
				loaded[index].Content,
				messages[index].Content,
			)
		}
	}
}
