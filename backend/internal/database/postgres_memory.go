package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"unihack-backend/internal/ai"
)

var (
	ErrNilDatabase              = errors.New("database is nil")
	ErrSessionMetadataInvalid   = errors.New("session metadata is invalid")
	ErrSessionOwnershipMismatch = errors.New(
		"session belongs to another wallet or chain",
	)
	ErrMessageInvalid = errors.New("message is invalid")
)

type PostgresMemoryStore struct {
	db *sql.DB
}

func NewPostgresMemoryStore(
	db *sql.DB,
) (*PostgresMemoryStore, error) {
	if db == nil {
		return nil, ErrNilDatabase
	}

	return &PostgresMemoryStore{db: db}, nil
}

func (s *PostgresMemoryStore) EnsureSession(
	ctx context.Context,
	metadata ai.SessionMetadata,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	metadata.SessionID = strings.TrimSpace(metadata.SessionID)
	metadata.WalletAddress = strings.TrimSpace(metadata.WalletAddress)
	metadata.Locale = strings.ToLower(strings.TrimSpace(metadata.Locale))

	if metadata.SessionID == "" ||
		metadata.WalletAddress == "" ||
		metadata.ChainID == 0 ||
		(metadata.Locale != "vi" && metadata.Locale != "en") {
		return ErrSessionMetadataInvalid
	}

	result, err := s.db.ExecContext(
		ctx,
		`
			INSERT INTO sessions (
				session_id,
				wallet_address,
				chain_id,
				locale
			)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (session_id)
			DO UPDATE SET
				locale = EXCLUDED.locale,
				updated_at = NOW()
			WHERE
				sessions.wallet_address = EXCLUDED.wallet_address
				AND sessions.chain_id = EXCLUDED.chain_id
		`,
		metadata.SessionID,
		metadata.WalletAddress,
		metadata.ChainID,
		metadata.Locale,
	)
	if err != nil {
		return fmt.Errorf("upsert chat session: %w", err)
	}

	affectedRows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read session upsert result: %w", err)
	}

	if affectedRows != 1 {
		return ErrSessionOwnershipMismatch
	}

	return nil
}

func (s *PostgresMemoryStore) Load(
	ctx context.Context,
	sessionID string,
) ([]ai.Message, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, ai.ErrEmptySessionID
	}

	rows, err := s.db.QueryContext(
		ctx,
		`
			SELECT
				request_id::text,
				role,
				content,
				COALESCE(tool_call_id, '')
			FROM messages
			WHERE session_id = $1
			ORDER BY created_at ASC, message_id ASC
		`,
		sessionID,
	)
	if err != nil {
		return nil, fmt.Errorf("query chat history: %w", err)
	}
	defer rows.Close()

	messages := make([]ai.Message, 0)

	for rows.Next() {
		var (
			message ai.Message
			role    string
		)

		if err := rows.Scan(
			&message.RequestID,
			&role,
			&message.Content,
			&message.ToolCallID,
		); err != nil {
			return nil, fmt.Errorf("scan chat message: %w", err)
		}

		message.Role = ai.Role(role)
		messages = append(messages, message)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate chat history: %w", err)
	}

	return messages, nil
}

func (s *PostgresMemoryStore) Append(
	ctx context.Context,
	sessionID string,
	message ai.Message,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	sessionID = strings.TrimSpace(sessionID)
	message.RequestID = strings.TrimSpace(message.RequestID)
	message.Content = strings.TrimSpace(message.Content)
	message.ToolCallID = strings.TrimSpace(message.ToolCallID)

	if sessionID == "" {
		return ai.ErrEmptySessionID
	}

	if message.RequestID == "" ||
		message.Content == "" ||
		(message.Role != ai.RoleUser &&
			message.Role != ai.RoleAssistant) {
		return ErrMessageInvalid
	}

	_, err := s.db.ExecContext(
		ctx,
		`
			INSERT INTO messages (
				session_id,
				request_id,
				role,
				content,
				tool_call_id
			)
			VALUES ($1, $2, $3, $4, NULLIF($5, ''))
		`,
		sessionID,
		message.RequestID,
		message.Role,
		message.Content,
		message.ToolCallID,
	)
	if err != nil {
		return fmt.Errorf("insert chat message: %w", err)
	}

	return nil
}

var _ ai.MemoryStore = (*PostgresMemoryStore)(nil)
var _ ai.SessionInitializer = (*PostgresMemoryStore)(nil)
