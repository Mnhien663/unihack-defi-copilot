package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net"
	"strings"
	"time"

	"unihack-backend/internal/ai"
	"unihack-backend/internal/avalanche"

	"github.com/ethereum/go-ethereum/common"
)

const (
	GetSwapQuoteToolName = "get_swap_quote"
	swapQuoteTTL         = 30 * time.Second
	maxQuoteSlippageBPS  = 500
)

type GetSwapQuoteTool struct {
	quoter avalanche.SwapQuoter
	tokens map[common.Address]avalanche.Asset
	now    func() time.Time
}

func NewGetSwapQuoteTool(
	quoter avalanche.SwapQuoter,
	cfg avalanche.LFJQuoteConfig,
) (*GetSwapQuoteTool, error) {
	if quoter == nil {
		return nil, errors.New("swap quoter is nil")
	}

	tokens := make(map[common.Address]avalanche.Asset, 2)
	for _, asset := range []avalanche.Asset{cfg.TokenA, cfg.TokenB} {
		if !common.IsHexAddress(asset.Address) ||
			asset.ChainID != uint64(avalanche.FujiChainID) {
			return nil, errors.New("invalid Fuji quote token configuration")
		}

		address := common.HexToAddress(asset.Address)
		if address == (common.Address{}) {
			return nil, errors.New("quote token cannot be the zero address")
		}
		asset.Address = address.Hex()
		tokens[address] = asset
	}
	if len(tokens) != 2 {
		return nil, errors.New("quote requires two distinct configured tokens")
	}

	return &GetSwapQuoteTool{
		quoter: quoter,
		tokens: tokens,
		now:    time.Now,
	}, nil
}

func (t *GetSwapQuoteTool) Definition() ai.ToolDefinition {
	return ai.ToolDefinition{
		Name: GetSwapQuoteToolName,
		Description: "Read an exact-input swap quote for the configured pair " +
			"on Avalanche Fuji. amount_in_raw is a decimal integer in the input " +
			"token's smallest units. Ask for missing amount, token addresses, " +
			"or slippage. Do not guess values. A quote is not a forecast, " +
			"execution guarantee, or transaction.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"chain_id": {"type": "integer", "const": 43113},
				"token_in": {"type": "string", "pattern": "^0x[0-9a-fA-F]{40}$"},
				"token_out": {"type": "string", "pattern": "^0x[0-9a-fA-F]{40}$"},
				"amount_in_raw": {"type": "string", "pattern": "^[0-9]{1,39}$"},
				"slippage_bps": {"type": "integer", "minimum": 0, "maximum": 500}
			},
			"required": ["chain_id", "token_in", "token_out", "amount_in_raw", "slippage_bps"],
			"additionalProperties": false
		}`),
	}
}

func (t *GetSwapQuoteTool) Execute(
	ctx context.Context,
	arguments json.RawMessage,
) (ai.ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return ai.ToolResult{}, err
	}

	var input struct {
		ChainID     uint64 `json:"chain_id"`
		TokenIn     string `json:"token_in"`
		TokenOut    string `json:"token_out"`
		AmountInRaw string `json:"amount_in_raw"`
		SlippageBPS *int   `json:"slippage_bps"`
	}
	if err := decodeStrictJSON(arguments, &input); err != nil {
		return newToolValidationError(
			"INVALID_TOOL_ARGUMENTS",
			"arguments must match the swap quote input schema",
			0,
		), nil
	}

	fail := func(code, message string) (ai.ToolResult, error) {
		return newToolValidationError(code, message, input.ChainID), nil
	}

	if input.ChainID != uint64(avalanche.FujiChainID) {
		return fail("UNSUPPORTED_CHAIN_ID", "chain_id must be 43113")
	}
	if !common.IsHexAddress(input.TokenIn) || !common.IsHexAddress(input.TokenOut) {
		return fail("INVALID_TOKEN_ADDRESS", "token_in and token_out must be valid addresses")
	}

	inAddress := common.HexToAddress(input.TokenIn)
	outAddress := common.HexToAddress(input.TokenOut)
	if inAddress == outAddress {
		return fail("SAME_TOKEN_SWAP", "token_in and token_out must differ")
	}

	inAsset, inAllowed := t.tokens[inAddress]
	outAsset, outAllowed := t.tokens[outAddress]
	if !inAllowed || !outAllowed {
		return fail("UNSUPPORTED_TOKEN", "both tokens must be in the quote allowlist")
	}

	amountIn, ok := parseQuoteUint128(input.AmountInRaw)
	if !ok || amountIn.Sign() == 0 {
		return fail("INVALID_AMOUNT", "amount_in_raw must be a positive uint128")
	}
	if input.SlippageBPS == nil ||
		*input.SlippageBPS < 0 ||
		*input.SlippageBPS > maxQuoteSlippageBPS {
		return fail("INVALID_SLIPPAGE", "slippage_bps is required and must be between 0 and 500")
	}

	startedAt := t.now().UTC()
	readCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	quote, err := t.quoter.QuoteExactInput(readCtx, avalanche.SwapQuoteRequest{
		TokenPath:   []avalanche.Asset{inAsset, outAsset},
		AmountInRaw: amountIn.String(),
	})
	if ctx.Err() != nil {
		return ai.ToolResult{}, ctx.Err()
	}

	var netErr net.Error
	if errors.Is(readCtx.Err(), context.DeadlineExceeded) ||
		errors.Is(err, context.DeadlineExceeded) ||
		(errors.As(err, &netErr) && netErr.Timeout()) {
		return fail("UPSTREAM_TIMEOUT", "swap quoter timed out")
	}
	if errors.Is(err, avalanche.ErrWrongChain) {
		return fail("UNSUPPORTED_CHAIN_ID", "quote RPC endpoint is not Avalanche Fuji")
	}
	if err != nil {
		return fail("UPSTREAM_QUOTE_ERROR", "could not obtain a complete swap quote")
	}

	now := t.now().UTC()
	observedAt := quote.Provenance.ObservedAt.UTC()
	if observedAt.IsZero() || observedAt.After(now) ||
		quote.Provenance.ChainID != input.ChainID ||
		strings.TrimSpace(quote.Provenance.Source) == "" {
		return fail("INVALID_QUOTE_RESPONSE", "quote source, chain, or observation time is invalid")
	}

	// A slow provider response must not extend the quote's lifetime.
	expiresAt := startedAt.Add(swapQuoteTTL)
	if observedExpiry := observedAt.Add(swapQuoteTTL); observedExpiry.Before(expiresAt) {
		expiresAt = observedExpiry
	}
	if !now.Before(expiresAt) {
		return fail("QUOTE_EXPIRED", "quote has expired; request a fresh quote")
	}

	if len(quote.TokenPath) != 2 ||
		quote.TokenPath[0] != inAsset || quote.TokenPath[1] != outAsset ||
		len(quote.Hops) != 1 ||
		quote.AmountIn.Asset != inAsset ||
		quote.AmountOut.Asset != outAsset ||
		quote.Fee.Asset != inAsset ||
		quote.AmountIn.Raw != amountIn.String() {
		return fail("INVALID_QUOTE_RESPONSE", "quote route or token metadata does not match the request")
	}

	expectedOut, outOK := parseQuoteUint128(quote.AmountOut.Raw)
	fee, feeOK := parseQuoteUint128(quote.Fee.Raw)
	if !outOK || expectedOut.Sign() == 0 || !feeOK || fee.Cmp(amountIn) > 0 {
		return fail("INVALID_QUOTE_RESPONSE", "quote output or input-token fee is invalid")
	}

	slippage := *input.SlippageBPS
	minOut := new(big.Int).Mul(expectedOut, big.NewInt(int64(10_000-slippage)))
	minOut.Quo(minOut, big.NewInt(10_000))
	if minOut.Sign() == 0 {
		return fail("MIN_OUT_TOO_SMALL", "minimum output rounds down to zero")
	}

	formatted, err := avalanche.FormatTokenAmount(minOut.String(), outAsset.Decimals)
	if err != nil {
		return ai.ToolResult{}, fmt.Errorf("format swap minimum output: %w", err)
	}

	data, err := json.Marshal(struct {
		avalanche.SwapQuoteResult
		MinOut      avalanche.TokenAmount `json:"min_out"`
		SlippageBPS int                   `json:"slippage_bps"`
		ExpiresAt   time.Time             `json:"expires_at"`
	}{
		SwapQuoteResult: quote,
		MinOut: avalanche.TokenAmount{
			Asset:     outAsset,
			Raw:       minOut.String(),
			Formatted: formatted,
		},
		SlippageBPS: slippage,
		ExpiresAt:   expiresAt,
	})
	if err != nil {
		return ai.ToolResult{}, fmt.Errorf("encode swap quote: %w", err)
	}

	return ai.ToolResult{
		Data:        data,
		Source:      quote.Provenance.Source,
		ChainID:     quote.Provenance.ChainID,
		BlockNumber: quote.Provenance.BlockNumber,
		ObservedAt:  observedAt,
	}, nil
}

func parseQuoteUint128(raw string) (*big.Int, bool) {
	if len(raw) == 0 || len(raw) > 39 {
		return nil, false
	}
	for _, ch := range raw {
		if ch < '0' || ch > '9' {
			return nil, false
		}
	}
	value, ok := new(big.Int).SetString(raw, 10)
	return value, ok && value.BitLen() <= 128
}

var _ ai.Tool = (*GetSwapQuoteTool)(nil)
