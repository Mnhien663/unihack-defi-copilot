package avalanche

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math/big"
	"slices"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

type SwapPreparationSimulator interface {
	SimulateSwap(context.Context, BuildSwapRequest) (SwapSimulationResult, error)
}

// PrepareSwapIntent contains only inputs the caller may propose.
// Quote, minimum output and deadline come from the backend.
type PrepareSwapIntent struct {
	WalletAddress string
	ChainID       uint64
	TokenIn       string
	TokenOut      string
	AmountInRaw   string
	SlippageBps   uint16
}

type PrepareSwapResult struct {
	Status       string                `json:"status"`
	Blockers     []string              `json:"blockers"`
	Requirements SwapRequirements      `json:"requirements"`
	Transaction  *BuiltSwapTransaction `json:"transaction,omitempty"`
}

type SwapPreparer struct {
	quoter    SwapQuoter
	simulator SwapPreparationSimulator
	maxAmount *big.Int
	now       func() time.Time
}

func NewSwapPreparer(
	quoter SwapQuoter,
	simulator SwapPreparationSimulator,
	maxAmountRaw string,
) (*SwapPreparer, error) {
	if quoter == nil || simulator == nil {
		return nil, errors.New("swap quoter and simulator are required")
	}

	maxAmount, err := lfjQuoteInput(maxAmountRaw)
	if err != nil {
		return nil, fmt.Errorf("invalid prepare swap amount cap: %w", err)
	}

	return &SwapPreparer{
		quoter:    quoter,
		simulator: simulator,
		maxAmount: maxAmount,
		now:       time.Now,
	}, nil
}

// sessionWallet must come from trusted application context, not model arguments.
// Matching addresses does not itself authenticate wallet ownership.
func (p *SwapPreparer) PrepareSwap(
	ctx context.Context,
	sessionWallet string,
	intent PrepareSwapIntent,
) (PrepareSwapResult, error) {
	if err := ctx.Err(); err != nil {
		return PrepareSwapResult{}, err
	}
	if intent.ChainID != uint64(FujiChainID) {
		return PrepareSwapResult{}, ErrWrongChain
	}
	if !common.IsHexAddress(sessionWallet) ||
		!common.IsHexAddress(intent.WalletAddress) {
		return PrepareSwapResult{}, ErrInvalidAddress
	}

	owner := common.HexToAddress(sessionWallet)
	if owner == (common.Address{}) ||
		owner != common.HexToAddress(intent.WalletAddress) {
		return PrepareSwapResult{}, errors.New("swap wallet must match the session wallet")
	}

	in, err := prepareSwapAsset(intent.TokenIn)
	if err != nil {
		return PrepareSwapResult{}, err
	}
	out, err := prepareSwapAsset(intent.TokenOut)
	if err != nil {
		return PrepareSwapResult{}, err
	}
	if in.Address == out.Address {
		return PrepareSwapResult{}, ErrUnsupportedSwapRoute
	}

	amount, err := lfjQuoteInput(intent.AmountInRaw)
	if err != nil {
		return PrepareSwapResult{}, err
	}
	if amount.Cmp(p.maxAmount) > 0 {
		return PrepareSwapResult{}, errors.New("swap amount exceeds application cap")
	}
	if intent.SlippageBps > maxQuoteSlippageBps {
		return PrepareSwapResult{}, errors.New("swap slippage exceeds application cap")
	}

	slippage := intent.SlippageBps
	log.Printf("prepare_swap input: tokenIn=%s amountInRaw=%s", in.Address, amount.String())
	quote, err := p.quoter.QuoteExactInput(ctx, SwapQuoteRequest{
		TokenPath:   []Asset{in, out},
		AmountInRaw: amount.String(),
		SlippageBps: &slippage,
	})
	if err != nil {
		return PrepareSwapResult{}, fmt.Errorf("read prepare swap quote: %w", err)
	}

	if len(quote.TokenPath) != 2 ||
		!common.IsHexAddress(quote.TokenPath[0].Address) ||
		!common.IsHexAddress(quote.TokenPath[1].Address) ||
		common.HexToAddress(quote.TokenPath[0].Address) != common.HexToAddress(in.Address) ||
		common.HexToAddress(quote.TokenPath[1].Address) != common.HexToAddress(out.Address) ||
		quote.AmountIn.Raw != amount.String() ||
		quote.SlippageBps != slippage {
		return PrepareSwapResult{}, errors.New("backend quote does not match swap intent")
	}

	now := p.now().UTC()
	if err := ValidateQuoteExpiry(quote, now); err != nil {
		return PrepareSwapResult{}, err
	}

	deadline := quote.ExpiresAt.Unix()
	if deadline <= now.Unix() || deadline <= 0 {
		return PrepareSwapResult{}, ErrQuoteExpired
	}

	simulation, err := p.simulator.SimulateSwap(ctx, BuildSwapRequest{
		OwnerAddress: owner.Hex(),
		Quote:        quote,
		Deadline:     uint64(deadline),
	})
	if err != nil {
		return PrepareSwapResult{}, fmt.Errorf("simulate prepared swap: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return PrepareSwapResult{}, err
	}

	// The calldata deadline is in whole seconds and can be earlier than
	// the quote expiry. Do not return a ready swap at or after either limit.
	now = p.now().UTC()
	if err := ValidateQuoteExpiry(quote, now); err != nil {
		return PrepareSwapResult{}, err
	}
	if now.Unix() >= deadline {
		return PrepareSwapResult{}, ErrQuoteExpired
	}

	result := PrepareSwapResult{
		Status:       simulation.Status,
		Blockers:     append([]string{}, simulation.Requirements.Blockers...),
		Requirements: simulation.Requirements,
	}

	switch simulation.Status {
	case "blocked":
		// Preserve every blocker. Only call approval the next step when
		// it is the sole known blocker; it is not permission to swap.
		if len(result.Blockers) == 1 &&
			slices.Contains(result.Blockers, "ERC20_APPROVAL_REQUIRED") {
			result.Status = "approval_required"
		}
		return result, nil
	case "reverted":
		return result, nil
	case "succeeded":
		if len(result.Blockers) != 0 ||
			simulation.Requirements.GasAffordable == nil ||
			!*simulation.Requirements.GasAffordable {
			return PrepareSwapResult{}, errors.New("inconsistent successful swap simulation")
		}
		result.Status = "ready_for_review"
		result.Transaction = &simulation.Transaction
		return result, nil
	default:
		return PrepareSwapResult{}, errors.New("unknown swap simulation status")
	}
}

func prepareSwapAsset(value string) (Asset, error) {
	if !common.IsHexAddress(value) {
		return Asset{}, ErrInvalidAddress
	}

	address := common.HexToAddress(value)
	var symbol string
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
