package database

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
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

	if err := json.Unmarshal(
		action.Arguments,
		&arguments,
	); err != nil {
		return nil, "", fmt.Errorf(
			"decode pending action arguments: %w",
			err,
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
		expiresAt := time.Now().UTC().Add(
			pendingActionLifetime,
		)

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

		_, err = tx.ExecContext(
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
					expires_at
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
					$10
				)
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
		)
		if err != nil {
			return fmt.Errorf(
				"insert pending action: %w",
				err,
			)
		}

		action.PendingActionID = actionID
		action.NormalizedIntentHash = intentHash
		action.ExpiresAt = expiresAt.Format(time.RFC3339)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit turn transaction: %w", err)
	}

	return nil
}

var _ ai.TurnRecorder = (*PostgresMemoryStore)(nil)
