package database

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestConnectFromEnvIntegration(t *testing.T) {
	if os.Getenv("DB_INTEGRATION_TEST") != "1" {
		t.Skip("set DB_INTEGRATION_TEST=1 to run PostgreSQL integration test")
	}

	ctx, cancel := context.WithTimeout(
		context.Background(),
		10*time.Second,
	)
	defer cancel()

	db, err := ConnectFromEnv(ctx)
	if err != nil {
		t.Fatalf("connect to PostgreSQL: %v", err)
	}
	defer db.Close()

	var value int

	if err := db.QueryRowContext(
		ctx,
		"SELECT 1",
	).Scan(&value); err != nil {
		t.Fatalf("execute PostgreSQL health query: %v", err)
	}

	if value != 1 {
		t.Fatalf("unexpected health query result: got %d, want 1", value)
	}
}
