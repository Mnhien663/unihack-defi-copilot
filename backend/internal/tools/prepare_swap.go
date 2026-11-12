package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
		}{
			Transaction: built.Prepared.Tx,
			Summary:     built.Summary,
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
