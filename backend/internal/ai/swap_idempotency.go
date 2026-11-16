package ai

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
)

type PendingSwapLookup interface {
	LookupPendingSwap(
		ctx context.Context,
		sessionID string,
		key string,
		fingerprint string,
	) (*PendingAction, error)
}

var ErrInvalidIdempotencyKey = errors.New(
	"Idempotency-Key must contain 1 to 128 ASCII letters, digits, underscores or hyphens",
)

var (
	ErrSwapIdempotencyConflict = errors.New("idempotency key was used for a different request")
	ErrSwapPendingUnavailable  = errors.New("previous swap action is expired or no longer pending")
)

var swapIdempotencyKeyPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

// swapRequestFingerprint uses the accepted chat input, not model output
// or generated transaction fields. Session scope is enforced by the DB key.
func swapRequestFingerprint(input AgentInput) (string, error) {
	if !swapIdempotencyKeyPattern.MatchString(input.IdempotencyKey) {
		return "", ErrInvalidIdempotencyKey
	}

	payload := struct {
		Version       string `json:"version"`
		WalletAddress string `json:"wallet_address"`
		ChainID       uint64 `json:"chain_id"`
		Message       string `json:"message"`
		Locale        string `json:"locale"`
	}{
		Version:       "prepare_swap_chat_v1",
		WalletAddress: strings.ToLower(strings.TrimSpace(input.WalletAddress)),
		ChainID:       input.ChainID,
		Message:       input.Message,
		Locale:        input.Locale,
	}

	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}
