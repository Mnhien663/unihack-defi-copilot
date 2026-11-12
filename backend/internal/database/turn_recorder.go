package database

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"unihack-backend/internal/ai"

	"github.com/google/uuid"
)

var ErrTurnRecordInvalid = errors.New("turn record is invalid")

const pendingActionLifetime = 10 * time.Minute

type normalizedIntent struct {
	ToolName      string `json:"tool_name"`
	Arguments     any    `json:"arguments"`
	ChainID       uint64 `json:"chain_id"`
	WalletAddress string `json:"wallet_address"`
}

func buildNormalizedIntent(
	action *ai.PendingAction,
) ([]byte, string, error) {
	var arguments any

	decoder := json.NewDecoder(bytes.NewReader(action.Arguments))
	decoder.UseNumber()

	if err := decoder.Decode(&arguments); err != nil {
		return nil, "", fmt.Errorf(
			"decode pending action arguments: %w",
			err,
		)
	}

	// Reject trailing data or a second JSON value.
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, "", fmt.Errorf(
			"pending action arguments must contain exactly one JSON value",
		)
	}

	intentJSON, err := json.Marshal(
		normalizedIntent{
			ToolName:  strings.TrimSpace(action.ToolName),
			Arguments: arguments,
			ChainID:   action.ChainID,
			WalletAddress: strings.ToLower(
				strings.TrimSpace(action.WalletAddress),
			),
		},
	)
	if err != nil {
		return nil, "", fmt.Errorf(
			"encode normalized intent: %w",
			err,
		)
	}

	hash := sha256.Sum256(intentJSON)

	return intentJSON, fmt.Sprintf("%x", hash), nil
}

func (s *PostgresMemoryStore) RecordTurn(
	ctx context.Context,
	record ai.TurnRecord,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	record.SessionID = strings.TrimSpace(record.SessionID)
	record.Message.RequestID = strings.TrimSpace(
		record.Message.RequestID,
	)
	record.Message.Content = strings.TrimSpace(
		record.Message.Content,
	)

	if record.SessionID == "" ||
		record.Message.RequestID == "" ||
		record.Message.Role != ai.RoleAssistant ||
		record.Message.Content == "" {
		return ErrTurnRecordInvalid
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin turn transaction: %w", err)
	}

	defer func() {
		_ = tx.Rollback()
	}()

	_, err = tx.ExecContext(
		ctx,
		`
			INSERT INTO messages (
				session_id,
				request_id,
				role,
				content,
				tool_call_id
			)
			VALUES ($1, $2, $3, $4, NULL)
		`,
		record.SessionID,
		record.Message.RequestID,
		record.Message.Role,
		record.Message.Content,
	)
	if err != nil {
		return fmt.Errorf("insert assistant message: %w", err)
	}

	for _, execution := range record.ToolExecutions {
		resultJSON, err := json.Marshal(execution.Result)
		if err != nil {
			return fmt.Errorf("encode tool result: %w", err)
		}

		arguments := execution.Call.Arguments
		if len(arguments) == 0 {
			arguments = json.RawMessage(`{}`)
		}

		_, err = tx.ExecContext(
			ctx,
			`
				INSERT INTO tool_calls (
					session_id,
					request_id,
					tool_call_id,
					tool_name,
					arguments,
					result,
					status,
					completed_at
				)
				VALUES (
					$1,
					$2,
					$3,
					$4,
					$5::jsonb,
					$6::jsonb,
					'succeeded',
					NOW()
				)
			`,
			record.SessionID,
			record.Message.RequestID,
			execution.Call.ID,
			execution.Call.Name,
			[]byte(arguments),
			resultJSON,
		)
		if err != nil {
			return fmt.Errorf(
				"insert tool call %q: %w",
				execution.Call.ID,
				err,
			)
		}
	}

	if record.PendingAction != nil {
		action := record.PendingAction

		intentJSON, intentHash, err :=
			buildNormalizedIntent(action)
		if err != nil {
			return err
		}

		actionID := uuid.NewString()
		now := time.Now().UTC()
		expiresAt := now.Add(pendingActionLifetime)

		if action.ExpiresAt != "" {
			preparedExpiry, err := time.Parse(time.RFC3339Nano, action.ExpiresAt)
			if err != nil {
				return fmt.Errorf("invalid prepared action expiry: %w", err)
			}
			if !preparedExpiry.After(now) {
				return fmt.Errorf("prepared action has already expired")
			}
			if preparedExpiry.Before(expiresAt) {
				expiresAt = preparedExpiry.UTC()
			}
		}

		arguments := action.Arguments
		if len(arguments) == 0 {
			arguments = json.RawMessage(`{}`)
		}

		_, err = tx.ExecContext(
			ctx,
			`
				INSERT INTO tool_calls (
					session_id,
					request_id,
					tool_call_id,
					tool_name,
					arguments,
					status
				)
				VALUES (
					$1,
					$2,
					$3,
					$4,
					$5::jsonb,
					'allowed'
				)
			`,
			record.SessionID,
			action.RequestID,
			action.ToolCallID,
			action.ToolName,
			[]byte(arguments),
		)
		if err != nil {
			return fmt.Errorf(
				"insert pending tool call: %w",
				err,
			)
		}

		var storedExpiry time.Time
		err = tx.QueryRowContext(
			ctx,
			`
		INSERT INTO pending_actions (
			pending_action_id,
			session_id,
			request_id,
			tool_call_id,
			wallet_address,
			chain_id,
			tool_name,
			normalized_intent_hash,
			intent,
			status,
			expires_at,
			idempotency_key,
			request_fingerprint
		)
		VALUES (
			$1,
			$2,
			$3,
			$4,
			$5,
			$6,
			$7,
			$8,
			$9::jsonb,
			'pending',
			$10,
			NULLIF($11::text, ''),
			NULLIF($12::text, '')
		)
			ON CONFLICT ON CONSTRAINT pending_actions_session_idempotency_key_unique
DO NOTHING
RETURNING expires_at
	`,
			actionID,
			record.SessionID,
			action.RequestID,
			action.ToolCallID,
			action.WalletAddress,
			action.ChainID,
			action.ToolName,
			intentHash,
			intentJSON,
			expiresAt,
			action.IdempotencyKey,
			action.RequestFingerprint,
		).Scan(&storedExpiry)

		if errors.Is(err, sql.ErrNoRows) {
			if err := tx.Rollback(); err != nil {
				return fmt.Errorf("rollback duplicate pending action turn: %w", err)
			}

			existing, err := s.LookupPendingSwap(
				ctx,
				record.SessionID,
				action.IdempotencyKey,
				action.RequestFingerprint,
			)
			if err != nil {
				return err
			}
			if existing == nil {
				return fmt.Errorf("idempotency conflict but no stored pending swap was found")
			}

			*action = *existing
			return nil
		}
		if err != nil {
			return fmt.Errorf("insert pending action: %w", err)
		}

		action.PendingActionID = actionID
		action.NormalizedIntentHash = intentHash
		action.ExpiresAt = storedExpiry.UTC().Format(time.RFC3339Nano)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit turn transaction: %w", err)
	}

	return nil
}

var _ ai.TurnRecorder = (*PostgresMemoryStore)(nil)
