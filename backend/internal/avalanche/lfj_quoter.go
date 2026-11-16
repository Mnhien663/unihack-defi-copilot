package avalanche

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

// LFJQuoteConfig is supplied by the application, never by the model.
// This adapter supports one configured V2.2 pool in either direction.
type LFJQuoteConfig struct {
	RouterAddress string
	PoolAddress   string
	TokenA        Asset
	TokenB        Asset
	BinStep       uint16
}

type LFJQuoter struct {
	rpc    *lfjQuoteRPC
	router common.Address
	pool   common.Address
	tokens map[common.Address]Asset
	bin    uint16
}

func NewLFJQuoter(client RPCReader, cfg LFJQuoteConfig) (*LFJQuoter, error) {
	router, err := lfjQuoteAddress(cfg.RouterAddress)
	if err != nil {
		return nil, fmt.Errorf("invalid LFJ router: %w", err)
	}

	pool, err := lfjQuoteAddress(cfg.PoolAddress)
	if err != nil {
		return nil, fmt.Errorf("invalid LFJ pool: %w", err)
	}

	if router == pool || cfg.BinStep == 0 {
		return nil, errors.New("invalid LFJ pool configuration")
	}

	tokens := make(map[common.Address]Asset, 2)
	for _, asset := range []Asset{cfg.TokenA, cfg.TokenB} {
		address, err := lfjQuoteAddress(asset.Address)
		if err != nil {
			return nil, fmt.Errorf("invalid LFJ allowlisted token: %w", err)
		}
		if asset.ChainID != uint64(FujiChainID) {
			return nil, errors.New("LFJ token must be configured for Fuji")
		}
		if _, exists := tokens[address]; exists {
			return nil, errors.New("LFJ configured tokens must differ")
		}

		asset.Address = address.Hex()
		tokens[address] = asset
	}

	rpc, err := newLFJQuoteRPC(client)
	if err != nil {
		return nil, err
	}

	return &LFJQuoter{
		rpc:    rpc,
		router: router,
		pool:   pool,
		tokens: tokens,
		bin:    cfg.BinStep,
	}, nil
}

func lfjQuoteAddress(value string) (common.Address, error) {
	if !common.IsHexAddress(value) {
		return common.Address{}, ErrInvalidAddress
	}

	address := common.HexToAddress(value)
	if address == (common.Address{}) {
		return common.Address{}, ErrInvalidAddress
	}

	return address, nil
}

func lfjQuoteInput(raw string) (*big.Int, error) {
	// uint128 has at most 39 decimal digits.
	if len(raw) == 0 || len(raw) > 39 {
		return nil, errors.New("amount_in_raw must be a positive uint128")
	}
	for _, ch := range raw {
		if ch < '0' || ch > '9' {
			return nil, errors.New("amount_in_raw must contain only decimal digits")
		}
	}

	amount, ok := new(big.Int).SetString(raw, 10)
	if !ok || amount.Sign() <= 0 || amount.BitLen() > 128 {
		return nil, errors.New("amount_in_raw must be a positive uint128")
	}
	return amount, nil
}

func (q *LFJQuoter) QuoteExactInput(
	ctx context.Context,
	request SwapQuoteRequest,
) (SwapQuoteResult, error) {
	startedAt := time.Now().UTC()
	if err := ctx.Err(); err != nil {
		return SwapQuoteResult{}, err
	}

	amountIn, err := lfjQuoteInput(request.AmountInRaw)
	if err != nil {
		return SwapQuoteResult{}, err
	}
	if len(request.TokenPath) != 2 {
		return SwapQuoteResult{}, errors.New("LFJ quote requires exactly two tokens")
	}

	var addresses [2]common.Address
	var assets [2]Asset
	for i, supplied := range request.TokenPath {
		address, err := lfjQuoteAddress(supplied.Address)
		if err != nil {
			return SwapQuoteResult{}, fmt.Errorf("invalid token at index %d: %w", i, err)
		}

		configured, allowed := q.tokens[address]
		if !allowed {
			return SwapQuoteResult{}, fmt.Errorf("token at index %d is not allowlisted", i)
		}
		if supplied.ChainID != configured.ChainID ||
			supplied.Decimals != configured.Decimals {
			return SwapQuoteResult{}, fmt.Errorf("token metadata mismatch at index %d", i)
		}

		addresses[i] = address
		assets[i] = configured
	}
	if addresses[0] == addresses[1] {
		return SwapQuoteResult{}, errors.New("swap input and output tokens must differ")
	}

	readCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	chainID, err := q.rpc.client.ChainID(readCtx)
	if err != nil {
		return SwapQuoteResult{}, fmt.Errorf("verify LFJ RPC chain: %w", err)
	}
	if chainID == nil || chainID.Cmp(big.NewInt(FujiChainID)) != 0 {
		return SwapQuoteResult{}, ErrWrongChain
	}

	height, err := q.rpc.client.BlockNumber(readCtx)
	if err != nil {
		return SwapQuoteResult{}, fmt.Errorf("read LFJ quote block: %w", err)
	}
	block := new(big.Int).SetUint64(height)

	x, err := q.rpc.call(readCtx, q.pool, block, "getTokenX")
	if err != nil {
		return SwapQuoteResult{}, err
	}
	y, err := q.rpc.call(readCtx, q.pool, block, "getTokenY")
	if err != nil {
		return SwapQuoteResult{}, err
	}

	tokenX := x[0].(common.Address)
	tokenY := y[0].(common.Address)

	var swapForY bool
	switch {
	case addresses[0] == tokenX && addresses[1] == tokenY:
		swapForY = true
	case addresses[0] == tokenY && addresses[1] == tokenX:
		swapForY = false
	default:
		return SwapQuoteResult{}, errors.New("LFJ pool tokens do not match the configured pair")
	}

	for _, asset := range assets {
		decimals, err := GetERC20Decimals(readCtx, q.rpc.client, asset.Address, block)
		if err != nil {
			return SwapQuoteResult{}, fmt.Errorf("verify LFJ token decimals: %w", err)
		}
		if decimals != asset.Decimals {
			return SwapQuoteResult{}, fmt.Errorf("on-chain decimals mismatch for %s", asset.Address)
		}
	}

	values, err := q.rpc.call(
		readCtx, q.router, block, "getSwapOut", q.pool, amountIn, swapForY,
	)
	if err != nil {
		return SwapQuoteResult{}, err
	}

	left := values[0].(*big.Int)
	out := values[1].(*big.Int)
	fee := values[2].(*big.Int)

	if left.Sign() != 0 {
		return SwapQuoteResult{}, errors.New("LFJ quote did not consume the full input amount")
	}
	if out.Sign() <= 0 || out.BitLen() > 128 ||
		fee.Sign() < 0 || fee.Cmp(amountIn) > 0 {
		return SwapQuoteResult{}, errors.New("invalid LFJ quote amount or fee")
	}

	inAmount, err := lfjTokenAmount(assets[0], amountIn)
	if err != nil {
		return SwapQuoteResult{}, err
	}
	outAmount, err := lfjTokenAmount(assets[1], out)
	if err != nil {
		return SwapQuoteResult{}, err
	}
	feeAmount, err := lfjTokenAmount(assets[0], fee)
	if err != nil {
		return SwapQuoteResult{}, err
	}

	quote := SwapQuoteResult{
		Protocol:      "LFJ",
		RouterAddress: q.router.Hex(),
		TokenPath:     []Asset{assets[0], assets[1]},
		Hops: []SwapQuoteHop{
			{PoolAddress: q.pool.Hex(), Version: "V2.2", BinStep: q.bin},
		},
		AmountIn:  inAmount,
		AmountOut: outAmount,
		Fee:       feeAmount,
		Provenance: Provenance{
			Source:      "LFJ V2.2 via Avalanche RPC",
			SourceType:  "rpc",
			ChainID:     uint64(FujiChainID),
			BlockNumber: &height,
			ObservedAt:  time.Now().UTC(),
			Mode:        "live",
		},
	}
	if request.SlippageBps == nil {
		return quote, nil
	}

	slippage := *request.SlippageBps
	if slippage > maxQuoteSlippageBps {
		return SwapQuoteResult{}, fmt.Errorf("swap slippage exceeds application cap")
	}

	expectedOut, ok := new(big.Int).SetString(quote.AmountOut.Raw, 10)
	if !ok || expectedOut.Sign() <= 0 {
		return SwapQuoteResult{}, fmt.Errorf("invalid quoted output amount")
	}

	minOut := new(big.Int).Mul(expectedOut, big.NewInt(10_000-int64(slippage)))
	minOut.Quo(minOut, big.NewInt(10_000))
	if minOut.Sign() <= 0 {
		return SwapQuoteResult{}, fmt.Errorf("minimum output rounds down to zero")
	}

	formatted, err := FormatTokenAmount(minOut.String(), quote.AmountOut.Asset.Decimals)
	if err != nil {
		return SwapQuoteResult{}, fmt.Errorf("format swap minimum output: %w", err)
	}

	const prepareQuoteTTL = 30 * time.Second
	observedAt := quote.Provenance.ObservedAt
	if observedAt.IsZero() {
		return SwapQuoteResult{}, fmt.Errorf("quote observation time is missing")
	}
	expiresAt := startedAt.Add(prepareQuoteTTL)
	if observedExpiry := observedAt.Add(prepareQuoteTTL); observedExpiry.Before(expiresAt) {
		expiresAt = observedExpiry
	}
	if !time.Now().UTC().Before(expiresAt) {
		return SwapQuoteResult{}, ErrQuoteExpired
	}

	quote.MinAmountOut = TokenAmount{
		Asset:     quote.AmountOut.Asset,
		Raw:       minOut.String(),
		Formatted: formatted,
	}
	quote.SlippageBps = slippage
	quote.ExpiresAt = expiresAt

	return quote, nil
}

func lfjTokenAmount(asset Asset, amount *big.Int) (TokenAmount, error) {
	raw := amount.String()
	formatted, err := FormatTokenAmount(raw, asset.Decimals)
	if err != nil {
		return TokenAmount{}, fmt.Errorf("format LFJ amount: %w", err)
	}
	return TokenAmount{Asset: asset, Raw: raw, Formatted: formatted}, nil
}

var _ SwapQuoter = (*LFJQuoter)(nil)
