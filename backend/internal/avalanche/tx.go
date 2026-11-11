package avalanche

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

const fujiExecutionManager = "0xA7c9881eb43902Ca668bb864668eAA3795f2283B"

//go:embed abi/AllowanceManager.json
var executionManagerABIJSON string

// BuildSwapRequest must be constructed from backend-owned intent data.
// OwnerAddress must come from the application's validated wallet context.
type BuildSwapRequest struct {
	OwnerAddress string          `json:"owner_address"`
	Quote        SwapQuoteResult `json:"quote"`
	Deadline     uint64          `json:"deadline"`
}

// SwapTxSummary describes the same fields encoded into manager calldata.
// Recipient is fixed to OwnerAddress by executeSwap.
type SwapTxSummary struct {
	OwnerAddress     string      `json:"owner_address"`
	RecipientAddress string      `json:"recipient_address"`
	ManagerAddress   string      `json:"manager_address"`
	TokenIn          Asset       `json:"token_in"`
	TokenOut         Asset       `json:"token_out"`
	AmountIn         TokenAmount `json:"amount_in"`
	MinAmountOut     TokenAmount `json:"min_amount_out"`
	Deadline         uint64      `json:"deadline"`
	QuoteExpiresAt   time.Time   `json:"quote_expires_at"`
}

// BuiltSwapTransaction contains bytes and their human-readable meaning.
// It is not a simulation result or permission to sign.
type BuiltSwapTransaction struct {
	Prepared PreparedTransaction `json:"prepared"`
	Summary  SwapTxSummary       `json:"summary"`
}

type SwapTxBuilder struct {
	manager common.Address
	abi     abi.ABI
}

func NewSwapTxBuilder() (*SwapTxBuilder, error) {
	parsed, err := abi.JSON(strings.NewReader(executionManagerABIJSON))
	if err != nil {
		return nil, fmt.Errorf("parse execution manager ABI: %w", err)
	}

	method, exists := parsed.Methods["executeSwap"]
	if !exists ||
		method.Sig != "executeSwap(address,address,uint256,uint256,uint256)" {
		return nil, errors.New("unexpected manager executeSwap ABI")
	}

	return &SwapTxBuilder{
		manager: common.HexToAddress(fujiExecutionManager),
		abi:     parsed,
	}, nil
}

func (b *SwapTxBuilder) BuildSwapTx(
	request BuildSwapRequest,
	now time.Time,
) (BuiltSwapTransaction, error) {
	var empty BuiltSwapTransaction
	quote := request.Quote

	if now.IsZero() {
		return empty, errors.New("current time is required")
	}
	now = now.UTC()

	if !common.IsHexAddress(request.OwnerAddress) {
		return empty, ErrInvalidAddress
	}
	owner := common.HexToAddress(request.OwnerAddress)
	if owner == (common.Address{}) ||
		owner == b.manager ||
		owner == common.HexToAddress(lfjFujiRouter) ||
		owner == common.HexToAddress(lfjFujiPair) {
		return empty, errors.New("invalid swap owner")
	}

	if err := ValidateQuoteExpiry(quote, now); err != nil {
		return empty, err
	}

	p := quote.Provenance
	if p.ChainID != uint64(FujiChainID) {
		return empty, ErrWrongChain
	}
	if p.SourceType != "rpc" || p.Mode != "live" || p.IsDemo ||
		strings.TrimSpace(p.Source) == "" ||
		p.BlockNumber == nil || *p.BlockNumber == 0 ||
		p.ObservedAt.IsZero() || p.ObservedAt.After(now) {
		return empty, errors.New("invalid live quote provenance")
	}
	if !quote.ExpiresAt.After(p.ObservedAt) ||
		quote.ExpiresAt.After(p.ObservedAt.Add(60*time.Second)) {
		return empty, errors.New("invalid quote lifetime")
	}

	if len(quote.TokenPath) != 2 || len(quote.Hops) != 1 {
		return empty, ErrUnsupportedSwapRoute
	}
	if !common.IsHexAddress(quote.RouterAddress) ||
		common.HexToAddress(quote.RouterAddress) !=
			common.HexToAddress(lfjFujiRouter) {
		return empty, ErrUnsupportedSwapRoute
	}
	hop := quote.Hops[0]
	if !common.IsHexAddress(hop.PoolAddress) ||
		common.HexToAddress(hop.PoolAddress) !=
			common.HexToAddress(lfjFujiPair) ||
		hop.Version != "V2.2" || hop.BinStep != 1 {
		return empty, ErrUnsupportedSwapRoute
	}

	// Normalize token identity and use configured display metadata.
	normalizeAsset := func(asset Asset) (Asset, error) {
		if asset.ChainID != uint64(FujiChainID) {
			return Asset{}, ErrWrongChain
		}
		if !common.IsHexAddress(asset.Address) {
			return Asset{}, ErrInvalidAddress
		}
		if asset.Decimals != 6 {
			return Asset{}, errors.New("unexpected LFJ token decimals")
		}

		address := common.HexToAddress(asset.Address)
		symbol := ""
		switch address {
		case common.HexToAddress(lfjFujiTokenX):
			symbol = "USDT"
		case common.HexToAddress(lfjFujiTokenY):
			symbol = "USDC"
		default:
			return Asset{}, ErrUnsupportedSwapRoute
		}

		return Asset{
			ChainID:  uint64(FujiChainID),
			Address:  address.Hex(),
			Symbol:   symbol,
			Decimals: 6,
		}, nil
	}

	tokenIn, err := normalizeAsset(quote.TokenPath[0])
	if err != nil {
		return empty, err
	}
	tokenOut, err := normalizeAsset(quote.TokenPath[1])
	if err != nil {
		return empty, err
	}
	if tokenIn.Address == tokenOut.Address {
		return empty, ErrUnsupportedSwapRoute
	}

	for _, check := range []struct {
		asset Asset
		want  Asset
	}{
		{quote.AmountIn.Asset, tokenIn},
		{quote.AmountOut.Asset, tokenOut},
		{quote.MinAmountOut.Asset, tokenOut},
		{quote.Fee.Asset, tokenIn},
	} {
		actual, err := normalizeAsset(check.asset)
		if err != nil {
			return empty, err
		}
		if actual != check.want {
			return empty, errors.New("quote amount asset mismatch")
		}
	}

	amountIn, err := lfjQuoteInput(quote.AmountIn.Raw)
	if err != nil {
		return empty, err
	}
	amountOut, err := lfjQuoteInput(quote.AmountOut.Raw)
	if err != nil {
		return empty, err
	}
	minOut, err := lfjQuoteInput(quote.MinAmountOut.Raw)
	if err != nil {
		return empty, err
	}
	fee, err := ParseRawAmount(quote.Fee.Raw)
	if err != nil || fee.BitLen() > 128 || fee.Cmp(amountIn) > 0 {
		return empty, errors.New("invalid quote fee")
	}
	if quote.SlippageBps > maxQuoteSlippageBps {
		return empty, errors.New("quote slippage exceeds application cap")
	}

	expectedMin := new(big.Int).Mul(
		new(big.Int).Set(amountOut),
		big.NewInt(int64(10_000-quote.SlippageBps)),
	)
	expectedMin.Quo(expectedMin, big.NewInt(10_000))
	if expectedMin.Cmp(minOut) != 0 {
		return empty, errors.New("quote minimum does not match slippage")
	}

	// Do not extend the quote's deadline while creating the transaction.
	if now.Unix() < 0 ||
		request.Deadline <= uint64(now.Unix()) ||
		quote.ExpiresAt.Unix() < 0 ||
		request.Deadline > uint64(quote.ExpiresAt.Unix()) {
		return empty, errors.New("deadline must be future and within quote expiry")
	}
	deadline := new(big.Int).SetUint64(request.Deadline)

	data, err := b.abi.Pack(
		"executeSwap",
		common.HexToAddress(tokenIn.Address),
		common.HexToAddress(tokenOut.Address),
		amountIn,
		minOut,
		deadline,
	)
	if err != nil {
		return empty, fmt.Errorf("encode executeSwap: %w", err)
	}

	method := b.abi.Methods["executeSwap"]
	if len(data) != 4+5*32 || !bytes.Equal(data[:4], method.ID) {
		return empty, errors.New("unexpected executeSwap calldata")
	}
	decoded, err := method.Inputs.Unpack(data[4:])
	if err != nil {
		return empty, fmt.Errorf("decode executeSwap: %w", err)
	}
	if decoded[0].(common.Address) != common.HexToAddress(tokenIn.Address) ||
		decoded[1].(common.Address) != common.HexToAddress(tokenOut.Address) ||
		decoded[2].(*big.Int).Cmp(amountIn) != 0 ||
		decoded[3].(*big.Int).Cmp(minOut) != 0 ||
		decoded[4].(*big.Int).Cmp(deadline) != 0 {
		return empty, errors.New("calldata differs from swap intent")
	}

	// Recompute display amounts rather than trusting formatted strings.
	inputAmount, err := lfjTokenAmount(tokenIn, amountIn)
	if err != nil {
		return empty, err
	}
	minimumAmount, err := lfjTokenAmount(tokenOut, minOut)
	if err != nil {
		return empty, err
	}

	return BuiltSwapTransaction{
		Prepared: PreparedTransaction{
			Action:   "swap",
			Protocol: "LFJ V2.2 via AllowanceManager",
			Tx: UnsignedTx{
				ChainID: uint64(FujiChainID),
				From:    owner.Hex(),
				To:      b.manager.Hex(),
				Value:   "0",
				Data:    hexutil.Encode(data),
			},
			// This is quote provenance, not a fresh simulation observation.
			Provenance: p,
		},
		Summary: SwapTxSummary{
			OwnerAddress:     owner.Hex(),
			RecipientAddress: owner.Hex(),
			ManagerAddress:   b.manager.Hex(),
			TokenIn:          tokenIn,
			TokenOut:         tokenOut,
			AmountIn:         inputAmount,
			MinAmountOut:     minimumAmount,
			Deadline:         request.Deadline,
			QuoteExpiresAt:   quote.ExpiresAt,
		},
	}, nil
}
