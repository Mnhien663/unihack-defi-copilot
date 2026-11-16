package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"unihack-backend/internal/ai"

	"unihack-backend/internal/avalanche"
)

const PrepareSwapToolName = "prepare_swap"

type SwapPreparationService interface {
	PrepareSwap(
		context.Context,
		string,
		avalanche.PrepareSwapIntent,
	) (avalanche.PrepareSwapResult, error)
}

// PrepareSwapFromArguments is called by the agent's confirmation path,
// not by the read-tool execution loop.
func PrepareSwapFromArguments(
	ctx context.Context,
	service SwapPreparationService,
	sessionWallet string,
	chainID uint64,
	raw json.RawMessage,
) (avalanche.PrepareSwapResult, error) {
	if err := ctx.Err(); err != nil {
		return avalanche.PrepareSwapResult{}, err
	}
	if service == nil {
		return avalanche.PrepareSwapResult{}, errors.New("swap preparation service is not configured")
	}

	var args struct {
		TokenIn     string  `json:"token_in"`
		TokenOut    string  `json:"token_out"`
		AmountInRaw string  `json:"amount_in_raw"`
		SlippageBps *uint16 `json:"slippage_bps"`
	}
	if err := decodeStrictJSON(raw, &args); err != nil {
		return avalanche.PrepareSwapResult{}, fmt.Errorf("decode prepare_swap arguments: %w", err)
	}
	if args.TokenIn == "" || args.TokenOut == "" ||
		args.AmountInRaw == "" || args.SlippageBps == nil {
		return avalanche.PrepareSwapResult{}, errors.New(
			"prepare_swap requires token_in, token_out, amount_in_raw and slippage_bps",
		)
	}

	return service.PrepareSwap(ctx, sessionWallet, avalanche.PrepareSwapIntent{
		WalletAddress: sessionWallet,
		ChainID:       chainID,
		TokenIn:       args.TokenIn,
		TokenOut:      args.TokenOut,
		AmountInRaw:   args.AmountInRaw,
		SlippageBps:   *args.SlippageBps,
	})
}

// NewPrepareSwapCallback bridges the swap service and the agent.
// It returns a payload for review, never signs or submits a transaction.
func NewPrepareSwapCallback(
	service SwapPreparationService,
) (ai.SwapPreparationFunc, error) {
	if service == nil {
		return nil, errors.New("swap preparation service is not configured")
	}

	return func(
		ctx context.Context,
		wallet string,
		chainID uint64,
		arguments json.RawMessage,
	) (ai.SwapPreparation, error) {
		result, err := PrepareSwapFromArguments(
			ctx, service, wallet, chainID, arguments,
		)
		if err != nil {
			return ai.SwapPreparation{}, err
		}

		prepared := ai.SwapPreparation{
			Status:   result.Status,
			Blockers: result.Blockers,
		}

		switch result.Status {
		case "approval_required", "blocked", "reverted":
			// Never expose transaction bytes for a non-ready swap.
			return prepared, nil

		case "ready_for_review":
			if result.Transaction == nil || len(result.Blockers) != 0 {
				return ai.SwapPreparation{}, errors.New("inconsistent ready swap")
			}

		default:
			return ai.SwapPreparation{}, errors.New("unknown swap preparation status")
		}

		built := result.Transaction
		deadline := built.Summary.Deadline
		if deadline == 0 || deadline > uint64(1<<63-1) {
			return ai.SwapPreparation{}, errors.New("invalid prepared swap deadline")
		}

		expiry := time.Unix(int64(deadline), 0).UTC()
		if !expiry.After(time.Now()) ||
			expiry.After(built.Summary.QuoteExpiresAt) {
			return ai.SwapPreparation{}, errors.New("prepared swap deadline is expired or exceeds quote expiry")
		}

		payload, err := json.Marshal(struct {
			Transaction avalanche.UnsignedTx    `json:"transaction"`
			Summary     avalanche.SwapTxSummary `json:"summary"`
			SlippageBps uint16                  `json:"slippage_bps"`
		}{
			Transaction: built.Prepared.Tx,
			Summary:     built.Summary,
			SlippageBps: result.SlippageBps,
		})
		if err != nil {
			return ai.SwapPreparation{}, fmt.Errorf("encode prepared swap: %w", err)
		}

		prepared.Arguments = payload
		prepared.ExpiresAt = expiry
		return prepared, nil
	}, nil
}

// PrepareSwapProposal exposes the schema to the LLM.
// The agent handles preparation through its confirmation callback.
type PrepareSwapProposal struct{}

func (PrepareSwapProposal) Definition() ai.ToolDefinition {
	return ai.ToolDefinition{
		Name: PrepareSwapToolName,
		Description: "Prepare an unsigned exact-input swap on Avalanche Fuji for user review. " +
			"Do not sign or submit it. amount_in_raw is in the token's smallest units.",
		InputSchema: json.RawMessage(`{
            "type": "object",
            "properties": {
                "token_in": {"type": "string", "description": "Input token contract address"},
                "token_out": {"type": "string", "description": "Output token contract address"},
                "amount_in_raw": {"type": "string", "description": "Exact input in smallest token units"},
                "slippage_bps": {"type": "integer", "description": "Slippage tolerance in basis points"}
            },
            "required": ["token_in", "token_out", "amount_in_raw", "slippage_bps"],
            "additionalProperties": false
        }`),
	}
}

func (PrepareSwapProposal) Execute(
	_ context.Context,
	_ json.RawMessage,
) (ai.ToolResult, error) {
	return ai.ToolResult{}, errors.New("prepare_swap must use the agent confirmation path")
}

type PendingSwapConfirmStore interface {
	LoadPendingSwapForReview(
		ctx context.Context,
		actionID, sessionID, authenticatedWallet string,
		chainID uint64,
		reviewedIntentHash string,
	) (*ai.PendingAction, error)

	ConfirmPendingSwap(
		ctx context.Context,
		actionID, sessionID, authenticatedWallet string,
		chainID uint64,
		reviewedIntentHash string,
	) (bool, error)
}

type ReviewedSwapRevalidator interface {
	RevalidateReviewedSwap(
		context.Context,
		avalanche.UnsignedTx,
		avalanche.SwapTxSummary,
		uint16,
	) (bool, error)
}

var (
	ErrPendingSwapNotConfirmable = errors.New("pending swap is not confirmable")
	ErrSwapReviewRequired        = errors.New("prepare and review a fresh swap")
)

// ConfirmReviewedSwap requires a server-authenticated wallet and authorized
// session. It returns the stored action only after the DB transition succeeds.
func ConfirmReviewedSwap(
	ctx context.Context,
	store PendingSwapConfirmStore,
	revalidator ReviewedSwapRevalidator,
	actionID, sessionID, authenticatedWallet string,
	chainID uint64,
	reviewedIntentHash string,
) (*ai.PendingAction, error) {
	action, err := store.LoadPendingSwapForReview(
		ctx, actionID, sessionID, authenticatedWallet, chainID, reviewedIntentHash,
	)
	if err != nil {
		return nil, fmt.Errorf("load reviewed swap: %w", err)
	}
	if action == nil {
		return nil, ErrPendingSwapNotConfirmable
	}

	var payload struct {
		Transaction avalanche.UnsignedTx    `json:"transaction"`
		Summary     avalanche.SwapTxSummary `json:"summary"`
		SlippageBps *uint16                 `json:"slippage_bps"`
	}
	if err := json.Unmarshal(action.Arguments, &payload); err != nil {
		return nil, fmt.Errorf("decode stored swap review: %w", err)
	}
	if payload.SlippageBps == nil {
		// Old actions must be prepared again. Zero bps is valid when present.
		return nil, ErrSwapReviewRequired
	}

	needsReview, err := revalidator.RevalidateReviewedSwap(
		ctx, payload.Transaction, payload.Summary, *payload.SlippageBps,
	)
	if err != nil {
		return nil, fmt.Errorf("revalidate reviewed swap: %w", err)
	}
	if needsReview {
		return nil, ErrSwapReviewRequired
	}

	changed, err := store.ConfirmPendingSwap(
		ctx, actionID, sessionID, authenticatedWallet, chainID, reviewedIntentHash,
	)
	if err != nil {
		return nil, fmt.Errorf("confirm reviewed swap: %w", err)
	}
	if !changed {
		// The action may have expired or another decision may have won.
		return nil, ErrPendingSwapNotConfirmable
	}

	return action, nil
}

type PendingSwapReceiptStore interface {
	LoadConfirmedSwapForReceipt(
		ctx context.Context,
		actionID, sessionID, authenticatedWallet string,
		chainID uint64,
		reviewedIntentHash string,
	) (*ai.PendingAction, string, string, error)

	AttachVerifiedPendingSwapHash(
		ctx context.Context,
		actionID, sessionID, authenticatedWallet string,
		chainID uint64,
		reviewedIntentHash, transactionHash string,
	) (bool, error)
}

var (
	ErrSwapSubmissionUnavailable  = errors.New("swap action is unavailable for submission")
	ErrSwapSubmissionHashMismatch = errors.New("swap action already has a different transaction hash")
)

// RecordReviewedSwapSubmission requires a server-authenticated wallet.
// It records submission only; it never signs, broadcasts, or marks an action mined.
func RecordReviewedSwapSubmission(
	ctx context.Context,
	store PendingSwapReceiptStore,
	reader avalanche.ReceiptReader,
	actionID, sessionID, authenticatedWallet string,
	chainID uint64,
	reviewedIntentHash, transactionHash string,
) (string, error) {
	transactionHash = strings.ToLower(transactionHash)

	action, status, storedHash, err := store.LoadConfirmedSwapForReceipt(
		ctx, actionID, sessionID, authenticatedWallet, chainID, reviewedIntentHash,
	)
	if err != nil {
		return "", fmt.Errorf("load swap submission: %w", err)
	}
	if action == nil {
		return "", ErrSwapSubmissionUnavailable
	}

	if storedHash != "" {
		if !strings.EqualFold(storedHash, transactionHash) {
			return "", ErrSwapSubmissionHashMismatch
		}
		// A retry with the already-verified hash does not write again.
		return status, nil
	}
	if status != "confirmed" {
		return "", ErrSwapSubmissionUnavailable
	}

	var payload struct {
		Transaction avalanche.UnsignedTx `json:"transaction"`
	}
	if err := json.Unmarshal(action.Arguments, &payload); err != nil {
		return "", fmt.Errorf("decode confirmed transaction: %w", err)
	}

	observed, err := reader.ReadReceipt(ctx, transactionHash)
	if err != nil {
		return "", fmt.Errorf("read submitted transaction: %w", err)
	}
	if err := avalanche.VerifyReviewedSwapTransaction(
		transactionHash, payload.Transaction, observed,
	); err != nil {
		return "", fmt.Errorf("verify submitted transaction: %w", err)
	}

	changed, err := store.AttachVerifiedPendingSwapHash(
		ctx, actionID, sessionID, authenticatedWallet, chainID,
		reviewedIntentHash, transactionHash,
	)
	if err != nil {
		return "", fmt.Errorf("record swap submission: %w", err)
	}
	if changed {
		return "submitted", nil
	}

	// A concurrent request may have attached the same hash first.
	action, status, storedHash, err = store.LoadConfirmedSwapForReceipt(
		ctx, actionID, sessionID, authenticatedWallet, chainID, reviewedIntentHash,
	)
	if err != nil {
		return "", fmt.Errorf("reload swap submission: %w", err)
	}
	if action != nil && storedHash != "" {
		if !strings.EqualFold(storedHash, transactionHash) {
			return "", ErrSwapSubmissionHashMismatch
		}
		return status, nil
	}
	return "", ErrSwapSubmissionUnavailable
}

type PendingSwapReceiptFinalizer interface {
	PendingSwapReceiptStore

	FinalizeVerifiedPendingSwapReceipt(
		ctx context.Context,
		actionID, sessionID, authenticatedWallet string,
		chainID uint64,
		reviewedIntentHash, transactionHash, outcome string,
	) (bool, error)
}

// RefreshReviewedSwapReceipt uses only the hash already stored for the action.
// The caller must authenticate the wallet before calling this service.
func RefreshReviewedSwapReceipt(
	ctx context.Context,
	store PendingSwapReceiptFinalizer,
	reader avalanche.ReceiptReader,
	actionID, sessionID, authenticatedWallet string,
	chainID uint64,
	reviewedIntentHash string,
) (string, error) {
	load := func() (*ai.PendingAction, string, string, error) {
		return store.LoadConfirmedSwapForReceipt(
			ctx, actionID, sessionID, authenticatedWallet,
			chainID, reviewedIntentHash,
		)
	}

	action, status, storedHash, err := load()
	if err != nil {
		return "", fmt.Errorf("load swap for receipt refresh: %w", err)
	}
	if action == nil || storedHash == "" {
		return "", ErrSwapSubmissionUnavailable
	}
	if status == "mined" || status == "failed" {
		return status, nil
	}
	if status != "submitted" {
		return "", ErrSwapSubmissionUnavailable
	}

	var payload struct {
		Transaction avalanche.UnsignedTx `json:"transaction"`
	}
	if err := json.Unmarshal(action.Arguments, &payload); err != nil {
		return "", fmt.Errorf("decode confirmed transaction: %w", err)
	}

	observed, err := reader.ReadReceipt(ctx, storedHash)
	if err != nil {
		return "", fmt.Errorf("refresh swap receipt: %w", err)
	}
	if err := avalanche.VerifyReviewedSwapTransaction(
		storedHash, payload.Transaction, observed,
	); err != nil {
		return "", fmt.Errorf("verify swap receipt transaction: %w", err)
	}

	var outcome string
	switch observed.Status {
	case avalanche.TransactionStatusPending:
		return "submitted", nil
	case avalanche.TransactionStatusSucceeded:
		outcome = "mined"
	case avalanche.TransactionStatusFailed:
		outcome = "failed"
	default:
		return "", errors.New("unknown swap receipt status")
	}

	if observed.BlockNumber == nil ||
		observed.BlockHash == nil || *observed.BlockHash == "" {
		return "", errors.New("final swap receipt is missing block identity")
	}

	changed, err := store.FinalizeVerifiedPendingSwapReceipt(
		ctx, actionID, sessionID, authenticatedWallet, chainID,
		reviewedIntentHash, storedHash, outcome,
	)
	if err != nil {
		return "", fmt.Errorf("record swap receipt outcome: %w", err)
	}
	if changed {
		return outcome, nil
	}

	// Another request may have finalized the same action first.
	current, currentStatus, currentHash, err := load()
	if err != nil {
		return "", fmt.Errorf("reload swap receipt outcome: %w", err)
	}
	if current != nil && strings.EqualFold(currentHash, storedHash) &&
		(currentStatus == "mined" || currentStatus == "failed") {
		return currentStatus, nil
	}

	return "", ErrSwapSubmissionUnavailable
}
