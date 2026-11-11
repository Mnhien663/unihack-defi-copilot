package avalanche

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
)

const (
	lfjFujiRouter                  = "0x18556DA13313f3532c54711497A8FedAC273220E"
	lfjFujiPair                    = "0xc3b7307184381f65B398d385a71EF0490239a2b3"
	lfjFujiTokenX                  = "0xAb231A5744C8E6c45481754928cCfFFFD4aa0732"
	lfjFujiTokenY                  = "0xB6076C93701D6a07266c31066B298AeC6dd65c2d"
	defaultQuoteSlippageBps uint16 = 50
	maxQuoteSlippageBps     uint16 = 500
	quoteValidity                  = 60 * time.Second
)

var (
	ErrUnsupportedSwapRoute  = errors.New("unsupported swap route")
	ErrInsufficientLiquidity = errors.New(
		"insufficient liquidity for full input amount",
	)
	ErrQuoteExpired = errors.New("quote has expired")
)

const lfjQuoteABIJSON = `[
	{
		"type":"function",
		"name":"getSwapOut",
		"stateMutability":"view",
		"inputs":[
			{"name":"pair","type":"address"},
			{"name":"amountIn","type":"uint128"},
			{"name":"swapForY","type":"bool"}
		],
		"outputs":[
			{"name":"amountInLeft","type":"uint128"},
			{"name":"amountOut","type":"uint128"},
			{"name":"fee","type":"uint128"}
		]
	},
	{
		"type":"function",
		"name":"getTokenX",
		"stateMutability":"view",
		"inputs":[],
		"outputs":[{"name":"","type":"address"}]
	},
	{
		"type":"function",
		"name":"getTokenY",
		"stateMutability":"view",
		"inputs":[],
		"outputs":[{"name":"","type":"address"}]
	}
]`

// LFJQuoteReader reads a configured direct Fuji pair.
// It does not discover routes, approve tokens or submit transactions.
type LFJQuoteReader struct {
	client RPCReader
	abi    abi.ABI
	router common.Address
	pair   common.Address
	tokenX common.Address
	tokenY common.Address
}

func NewLFJQuoteReader(client RPCReader) (*LFJQuoteReader, error) {
	if client == nil {
		return nil, errors.New("quote RPC client is nil")
	}

	parsed, err := abi.JSON(strings.NewReader(lfjQuoteABIJSON))
	if err != nil {
		return nil, err
	}

	return &LFJQuoteReader{
		client: client,
		abi:    parsed,
		router: common.HexToAddress(lfjFujiRouter),
		pair:   common.HexToAddress(lfjFujiPair),
		tokenX: common.HexToAddress(lfjFujiTokenX),
		tokenY: common.HexToAddress(lfjFujiTokenY),
	}, nil
}

func (r *LFJQuoteReader) QuoteExactInput(
	ctx context.Context,
	request SwapQuoteRequest,
) (SwapQuoteResult, error) {
	if err := ctx.Err(); err != nil {
		return SwapQuoteResult{}, err
	}

	slippageBps := defaultQuoteSlippageBps
	if request.SlippageBps != nil {
		slippageBps = *request.SlippageBps
	}
	if slippageBps > maxQuoteSlippageBps {
		return SwapQuoteResult{}, errors.New(
			"slippage must not exceed 500 bps",
		)
	}

	// Expiry starts before RPC reads, so read time consumes validity.
	quotedAt := time.Now().UTC()
	expiresAt := quotedAt.Add(quoteValidity)

	if len(request.TokenPath) != 2 {
		return SwapQuoteResult{}, ErrUnsupportedSwapRoute
	}

	input := request.TokenPath[0]
	output := request.TokenPath[1]

	for _, asset := range request.TokenPath {
		if asset.ChainID != uint64(FujiChainID) {
			return SwapQuoteResult{}, ErrWrongChain
		}
		if !common.IsHexAddress(asset.Address) {
			return SwapQuoteResult{}, fmt.Errorf(
				"%w: quote token", ErrInvalidAddress,
			)
		}
	}

	tokenIn := common.HexToAddress(input.Address)
	tokenOut := common.HexToAddress(output.Address)

	swapForY := tokenIn == r.tokenX && tokenOut == r.tokenY
	reverse := tokenIn == r.tokenY && tokenOut == r.tokenX
	if !swapForY && !reverse {
		return SwapQuoteResult{}, ErrUnsupportedSwapRoute
	}

	amountIn, err := ParseRawAmount(request.AmountInRaw)
	if err != nil {
		return SwapQuoteResult{}, fmt.Errorf("parse quote input: %w", err)
	}
	if amountIn.Sign() <= 0 || amountIn.BitLen() > 128 {
		return SwapQuoteResult{}, errors.New(
			"quote input must be positive and fit uint128",
		)
	}

	readCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	chainID, err := r.client.ChainID(readCtx)
	if err != nil {
		return SwapQuoteResult{}, fmt.Errorf("verify quote chain: %w", err)
	}
	if chainID == nil || chainID.Cmp(big.NewInt(FujiChainID)) != 0 {
		return SwapQuoteResult{}, ErrWrongChain
	}

	block, err := r.client.BlockNumber(readCtx)
	if err != nil {
		return SwapQuoteResult{}, fmt.Errorf("get quote block: %w", err)
	}
	blockNumber := new(big.Int).SetUint64(block)

	for _, check := range []struct {
		method string
		want   common.Address
	}{
		{"getTokenX", r.tokenX},
		{"getTokenY", r.tokenY},
	} {
		values, err := r.callQuoteMethod(
			readCtx, r.pair, blockNumber, check.method,
		)
		if err != nil {
			return SwapQuoteResult{}, err
		}
		if len(values) != 1 {
			return SwapQuoteResult{}, errors.New("invalid pair token response")
		}
		address, ok := values[0].(common.Address)
		if !ok || address != check.want {
			return SwapQuoteResult{}, errors.New(
				"pair token does not match configured route",
			)
		}
	}

	decimalsIn, err := GetERC20Decimals(
		readCtx, r.client, tokenIn.Hex(), blockNumber,
	)
	if err != nil {
		return SwapQuoteResult{}, fmt.Errorf("read input decimals: %w", err)
	}
	decimalsOut, err := GetERC20Decimals(
		readCtx, r.client, tokenOut.Hex(), blockNumber,
	)
	if err != nil {
		return SwapQuoteResult{}, fmt.Errorf("read output decimals: %w", err)
	}

	if input.Decimals != decimalsIn || output.Decimals != decimalsOut {
		return SwapQuoteResult{}, errors.New(
			"requested token decimals do not match on-chain metadata",
		)
	}

	values, err := r.callQuoteMethod(
		readCtx,
		r.router,
		blockNumber,
		"getSwapOut",
		r.pair,
		amountIn,
		swapForY,
	)
	if err != nil {
		return SwapQuoteResult{}, err
	}
	if len(values) != 3 {
		return SwapQuoteResult{}, errors.New("invalid router quote response")
	}

	left, leftOK := values[0].(*big.Int)
	amountOut, outOK := values[1].(*big.Int)
	fee, feeOK := values[2].(*big.Int)
	if !leftOK || !outOK || !feeOK ||
		left == nil || amountOut == nil || fee == nil {
		return SwapQuoteResult{}, errors.New("invalid quote amount types")
	}

	for _, value := range []*big.Int{left, amountOut, fee} {
		if value.Sign() < 0 || value.BitLen() > 128 {
			return SwapQuoteResult{}, errors.New("quote amount exceeds uint128")
		}
	}
	if left.Cmp(amountIn) > 0 || fee.Cmp(amountIn) > 0 {
		return SwapQuoteResult{}, errors.New("inconsistent router quote amounts")
	}
	if left.Sign() != 0 || amountOut.Sign() == 0 {
		return SwapQuoteResult{}, ErrInsufficientLiquidity
	}

	formattedIn, err := FormatTokenAmount(amountIn.String(), decimalsIn)
	if err != nil {
		return SwapQuoteResult{}, err
	}
	formattedOut, err := FormatTokenAmount(amountOut.String(), decimalsOut)
	if err != nil {
		return SwapQuoteResult{}, err
	}

	// Symbols come from the configured route, not caller-provided labels.
	assetIn := Asset{
		ChainID:  uint64(FujiChainID),
		Address:  tokenIn.Hex(),
		Symbol:   "USDT",
		Decimals: decimalsIn,
	}
	assetOut := Asset{
		ChainID:  uint64(FujiChainID),
		Address:  tokenOut.Hex(),
		Symbol:   "USDC",
		Decimals: decimalsOut,
	}
	if reverse {
		assetIn.Symbol, assetOut.Symbol = "USDC", "USDT"
	}

	// Integer division floors the result; never use floating point.
	minOut := new(big.Int).Mul(
		new(big.Int).Set(amountOut),
		big.NewInt(int64(10_000-slippageBps)),
	)
	minOut.Quo(minOut, big.NewInt(10_000))

	if minOut.Sign() == 0 {
		return SwapQuoteResult{}, errors.New(
			"minimum output rounds down to zero",
		)
	}

	formattedMinOut, err := FormatTokenAmount(
		minOut.String(), decimalsOut,
	)
	if err != nil {
		return SwapQuoteResult{}, err
	}

	formattedFee, err := FormatTokenAmount(fee.String(), decimalsIn)
	if err != nil {
		return SwapQuoteResult{}, err
	}

	if err := readCtx.Err(); err != nil {
		return SwapQuoteResult{}, err
	}
	if !time.Now().UTC().Before(expiresAt) {
		return SwapQuoteResult{}, ErrQuoteExpired
	}

	return SwapQuoteResult{
		Protocol:      "LFJ V2.2",
		RouterAddress: r.router.Hex(),
		TokenPath:     []Asset{assetIn, assetOut},
		Hops: []SwapQuoteHop{
			{
				PoolAddress: r.pair.Hex(),
				Version:     "V2.2",
				BinStep:     1,
			},
		},
		AmountIn: TokenAmount{
			Asset: assetIn, Raw: amountIn.String(), Formatted: formattedIn,
		},
		AmountOut: TokenAmount{
			Asset: assetOut, Raw: amountOut.String(), Formatted: formattedOut,
		},
		Provenance: Provenance{
			Source:      "LFJ Fuji LBRouter",
			SourceType:  "rpc",
			ChainID:     uint64(FujiChainID),
			BlockNumber: &block,
			ObservedAt:  time.Now().UTC(),
			Mode:        "live",
			IsDemo:      false,
		},
		MinAmountOut: TokenAmount{
			Asset:     assetOut,
			Raw:       minOut.String(),
			Formatted: formattedMinOut,
		},
		Fee: TokenAmount{
			Asset:     assetIn,
			Raw:       fee.String(),
			Formatted: formattedFee,
		},
		SlippageBps: slippageBps,
		ExpiresAt:   expiresAt,
	}, nil
}

func (r *LFJQuoteReader) callQuoteMethod(
	ctx context.Context,
	address common.Address,
	block *big.Int,
	method string,
	args ...interface{},
) ([]interface{}, error) {
	data, err := r.abi.Pack(method, args...)
	if err != nil {
		return nil, fmt.Errorf("encode %s: %w", method, err)
	}

	raw, err := r.client.CallContract(
		ctx,
		ethereum.CallMsg{To: &address, Data: data},
		block,
	)
	if err != nil {
		return nil, fmt.Errorf("call %s: %w", method, err)
	}

	expectedBytes := 32
	if method == "getSwapOut" {
		expectedBytes = 96
	}
	if len(raw) != expectedBytes {
		return nil, fmt.Errorf(
			"%s returned %d bytes; expected %d",
			method, len(raw), expectedBytes,
		)
	}

	values, err := r.abi.Unpack(method, raw)
	if err != nil {
		return nil, fmt.Errorf("decode %s: %w", method, err)
	}
	return values, nil
}

var _ SwapQuoter = (*LFJQuoteReader)(nil)

// ValidateQuoteExpiry checks freshness only.
// It does not validate transaction calldata or guarantee execution.
func ValidateQuoteExpiry(
	quote SwapQuoteResult,
	now time.Time,
) error {
	if quote.ExpiresAt.IsZero() {
		return errors.New("quote expiry is missing")
	}
	if !now.Before(quote.ExpiresAt) {
		return ErrQuoteExpired
	}
	return nil
}
