package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"unihack-backend/internal/ai"
)

// LookupPendingSwap returns nil, nil when the key has not been used.
// The caller must validate the session's wallet and chain before lookup.
func (s *PostgresMemoryStore) LookupPendingSwap(
	ctx context.Context,
	sessionID string,
	key string,
	fingerprint string,
) (*ai.PendingAction, error) {
	var action ai.PendingAction
	var storedFingerprint string
	var status string
	var expiresAt time.Time
	var unexpired bool
	var arguments []byte

	err := s.db.QueryRowContext(ctx, `
		SELECT pending_action_id,
		       request_id,
		       tool_call_id,
		       tool_name,
		       wallet_address,
		       chain_id,
		       normalized_intent_hash,
		       intent -> 'arguments',
		       request_fingerprint,
		       status,
		       expires_at,
		       expires_at > clock_timestamp()
		FROM pending_actions
		WHERE session_id = $1 AND idempotency_key = $2
	`, sessionID, key).Scan(
		&action.PendingActionID,
		&action.RequestID,
		&action.ToolCallID,
		&action.ToolName,
		&action.WalletAddress,
		&action.ChainID,
		&action.NormalizedIntentHash,
		&arguments,
		&storedFingerprint,
		&status,
		&expiresAt,
		&unexpired,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("lookup pending swap: %w", err)
	}
	if storedFingerprint != fingerprint {
		return nil, ai.ErrSwapIdempotencyConflict
	}
	if action.ToolName != "prepare_swap" || status != "pending" || !unexpired {
		return nil, ai.ErrSwapPendingUnavailable
	}

	var payload map[string]json.RawMessage
	if err := json.Unmarshal(arguments, &payload); err != nil || payload == nil {
		return nil, errors.New("stored pending swap arguments are not a JSON object")
	}

	action.Arguments = json.RawMessage(arguments)
	action.ExpiresAt = expiresAt.UTC().Format(time.RFC3339Nano)
	action.RequiresConfirmation = true

	return &action, nil
}
