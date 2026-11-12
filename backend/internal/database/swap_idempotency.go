package database

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
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

// RejectPendingSwap requires a wallet and session authorized by the caller.
func (s *PostgresMemoryStore) RejectPendingSwap(
	ctx context.Context,
	actionID string,
	sessionID string,
	authenticatedWallet string,
	chainID uint64,
	reviewedIntentHash string,
) (bool, error) {
	return s.transitionPendingSwap(
		ctx, actionID, sessionID, authenticatedWallet,
		chainID, reviewedIntentHash, "rejected",
	)
}

// ConfirmPendingSwap persists the decision only. The caller must
// authenticate the wallet and revalidate the reviewed transaction first.
func (s *PostgresMemoryStore) ConfirmPendingSwap(
	ctx context.Context,
	actionID string,
	sessionID string,
	authenticatedWallet string,
	chainID uint64,
	reviewedIntentHash string,
) (bool, error) {
	return s.transitionPendingSwap(
		ctx, actionID, sessionID, authenticatedWallet,
		chainID, reviewedIntentHash, "confirmed",
	)
}

func (s *PostgresMemoryStore) transitionPendingSwap(
	ctx context.Context,
	actionID string,
	sessionID string,
	authenticatedWallet string,
	chainID uint64,
	reviewedIntentHash string,
	nextStatus string,
) (bool, error) {
	var updatedID string

	err := s.db.QueryRowContext(ctx, `
		UPDATE pending_actions
		SET status = $6,
		    updated_at = clock_timestamp()
		WHERE pending_action_id = $1
		  AND session_id = $2
		  AND lower(wallet_address) = lower($3)
		  AND chain_id = $4
		  AND normalized_intent_hash = $5
		  AND tool_name = 'prepare_swap'
		  AND status = 'pending'
		  AND expires_at > clock_timestamp()
		RETURNING pending_action_id
	`, actionID, sessionID, authenticatedWallet, chainID,
		reviewedIntentHash, nextStatus).Scan(&updatedID)

	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("transition pending swap to %s: %w", nextStatus, err)
	}

	return true, nil
}

// LoadPendingSwapForReview loads the exact review snapshot without locking it.
// The caller must authenticate the wallet and authorize the session first.
// A successful read does not replace the conditional update after simulation.
func (s *PostgresMemoryStore) LoadPendingSwapForReview(
	ctx context.Context,
	actionID string,
	sessionID string,
	authenticatedWallet string,
	chainID uint64,
	reviewedIntentHash string,
) (*ai.PendingAction, error) {
	var action ai.PendingAction
	var arguments []byte
	var expiresAt time.Time

	err := s.db.QueryRowContext(ctx, `
		SELECT pending_action_id, request_id, tool_call_id, tool_name,
		       wallet_address, chain_id, normalized_intent_hash,
		       intent -> 'arguments', expires_at
		FROM pending_actions
		WHERE pending_action_id = $1
		  AND session_id = $2
		  AND lower(wallet_address) = lower($3)
		  AND chain_id = $4
		  AND normalized_intent_hash = $5
		  AND tool_name = 'prepare_swap'
		  AND status = 'pending'
		  AND expires_at > clock_timestamp()
	`, actionID, sessionID, authenticatedWallet, chainID, reviewedIntentHash).Scan(
		&action.PendingActionID,
		&action.RequestID,
		&action.ToolCallID,
		&action.ToolName,
		&action.WalletAddress,
		&action.ChainID,
		&action.NormalizedIntentHash,
		&arguments,
		&expiresAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load pending swap for review: %w", err)
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

// LookupPendingSwapOwner returns the wallet recorded for this review.
// It does not authenticate the caller or authorize a state change.
func (s *PostgresMemoryStore) LookupPendingSwapOwner(
	ctx context.Context,
	actionID string,
	sessionID string,
	chainID uint64,
	reviewedIntentHash string,
) (string, error) {
	var wallet string

	err := s.db.QueryRowContext(ctx, `
		SELECT wallet_address
		FROM pending_actions
		WHERE pending_action_id = $1
		  AND session_id = $2
		  AND chain_id = $3
		  AND normalized_intent_hash = $4
		  AND tool_name = 'prepare_swap'
		  AND status = 'pending'
		  AND expires_at > clock_timestamp()
	`, actionID, sessionID, chainID, reviewedIntentHash).Scan(&wallet)

	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("lookup pending swap owner: %w", err)
	}

	return wallet, nil
}

var ErrPendingSwapHashAlreadyUsed = errors.New(
	"transaction hash is already associated with a pending action",
)

// AttachVerifiedPendingSwapHash persists a hash only after the caller
// authenticates the owner and verifies the transaction against the review.
func (s *PostgresMemoryStore) AttachVerifiedPendingSwapHash(
	ctx context.Context,
	actionID, sessionID, authenticatedWallet string,
	chainID uint64,
	reviewedIntentHash, transactionHash string,
) (bool, error) {
	transactionHash = strings.ToLower(transactionHash)
	if len(transactionHash) != 66 || !strings.HasPrefix(transactionHash, "0x") {
		return false, errors.New("invalid transaction hash")
	}
	if _, err := hex.DecodeString(transactionHash[2:]); err != nil {
		return false, errors.New("invalid transaction hash")
	}

	var updatedID string
	err := s.db.QueryRowContext(ctx, `
		UPDATE pending_actions
		SET transaction_hash = $6,
		    status = 'submitted',
		    updated_at = clock_timestamp()
		WHERE pending_action_id = $1
		  AND session_id = $2
		  AND lower(wallet_address) = lower($3)
		  AND chain_id = $4
		  AND normalized_intent_hash = $5
		  AND tool_name = 'prepare_swap'
		  AND status = 'confirmed'
		  AND transaction_hash IS NULL
		RETURNING pending_action_id
	`, actionID, sessionID, authenticatedWallet, chainID,
		reviewedIntentHash, transactionHash).Scan(&updatedID)

	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		var pgErr interface{ SQLState() string }
		if errors.As(err, &pgErr) && pgErr.SQLState() == "23505" {
			return false, ErrPendingSwapHashAlreadyUsed
		}
		return false, fmt.Errorf("attach verified swap transaction hash: %w", err)
	}

	return true, nil
}

// LoadConfirmedSwapForReceipt returns the approved payload and any
// attached transaction hash. The caller must authenticate the wallet first.
func (s *PostgresMemoryStore) LoadConfirmedSwapForReceipt(
	ctx context.Context,
	actionID, sessionID, authenticatedWallet string,
	chainID uint64,
	reviewedIntentHash string,
) (action *ai.PendingAction, status string, storedHash string, err error) {
	var stored ai.PendingAction
	var arguments []byte
	var expiresAt time.Time

	err = s.db.QueryRowContext(ctx, `
		SELECT pending_action_id, request_id, tool_call_id, tool_name,
		       wallet_address, chain_id, normalized_intent_hash,
		       intent -> 'arguments', expires_at, status,
		       COALESCE(transaction_hash, '')
		FROM pending_actions
		WHERE pending_action_id = $1
		  AND session_id = $2
		  AND lower(wallet_address) = lower($3)
		  AND chain_id = $4
		  AND normalized_intent_hash = $5
		  AND tool_name = 'prepare_swap'
		  AND status IN ('confirmed', 'submitted', 'mined', 'failed')
	`, actionID, sessionID, authenticatedWallet, chainID, reviewedIntentHash).Scan(
		&stored.PendingActionID,
		&stored.RequestID,
		&stored.ToolCallID,
		&stored.ToolName,
		&stored.WalletAddress,
		&stored.ChainID,
		&stored.NormalizedIntentHash,
		&arguments,
		&expiresAt,
		&status,
		&storedHash,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "", "", nil
	}
	if err != nil {
		return nil, "", "", fmt.Errorf("load confirmed swap for receipt: %w", err)
	}

	var payload map[string]json.RawMessage
	if err := json.Unmarshal(arguments, &payload); err != nil || payload == nil {
		return nil, "", "", errors.New("stored swap arguments are not a JSON object")
	}

	stored.Arguments = json.RawMessage(arguments)
	stored.ExpiresAt = expiresAt.UTC().Format(time.RFC3339Nano)
	return &stored, status, storedHash, nil
}

// FinalizeVerifiedPendingSwapReceipt records a verified receipt outcome.
// The caller must verify the receipt and transaction against the stored review.
func (s *PostgresMemoryStore) FinalizeVerifiedPendingSwapReceipt(
	ctx context.Context,
	actionID, sessionID, authenticatedWallet string,
	chainID uint64,
	reviewedIntentHash, transactionHash, outcome string,
) (bool, error) {
	if outcome != "mined" && outcome != "failed" {
		return false, errors.New("invalid final swap receipt outcome")
	}

	var updatedID string
	err := s.db.QueryRowContext(ctx, `
		UPDATE pending_actions
		SET status = $7,
		    updated_at = clock_timestamp()
		WHERE pending_action_id = $1
		  AND session_id = $2
		  AND lower(wallet_address) = lower($3)
		  AND chain_id = $4
		  AND normalized_intent_hash = $5
		  AND tool_name = 'prepare_swap'
		  AND status = 'submitted'
		  AND lower(transaction_hash) = lower($6)
		RETURNING pending_action_id
	`, actionID, sessionID, authenticatedWallet, chainID,
		reviewedIntentHash, transactionHash, outcome).Scan(&updatedID)

	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("finalize verified swap receipt: %w", err)
	}

	return true, nil
}
