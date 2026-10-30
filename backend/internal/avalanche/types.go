package avalanche

import (
	"context"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
)

// RPCReader defines the RPC operations used by chain adapters.
type RPCReader interface {
	ChainID(ctx context.Context) (*big.Int, error)

	BlockNumber(ctx context.Context) (uint64, error)

	BalanceAt(
		ctx context.Context,
		account common.Address,
		blockNumber *big.Int,
	) (*big.Int, error)

	CallContract(
		ctx context.Context,
		msg ethereum.CallMsg,
		blockNumber *big.Int,
	) ([]byte, error)
}

// Asset identifies a token on a specific chain.
// An empty Address means the chain's native currency.
type Asset struct {
	ChainID  uint64 `json:"chain_id"`
	Address  string `json:"address,omitempty"`
	Symbol   string `json:"symbol"`
	Decimals uint8  `json:"decimals"`
}

// TokenAmount stores an integer amount in the token's smallest units.
// Raw must be a non-negative base-10 integer string.
type TokenAmount struct {
	Asset Asset  `json:"asset"`
	Raw   string `json:"raw"`
}

// Provenance records where and when data was obtained.
// BlockNumber is omitted when the source does not provide one.
// ObservedAt is the observation time, not the block timestamp.
type Provenance struct {
	Source      string    `json:"source"`
	SourceType  string    `json:"source_type"`
	ChainID     uint64    `json:"chain_id"`
	BlockNumber *uint64   `json:"block_number,omitempty"`
	ObservedAt  time.Time `json:"observed_at"`
	Mode        string    `json:"mode"`
	IsDemo      bool      `json:"is_demo"`
}

// NativeBalanceResult is the chain adapter's native balance response.
type NativeBalanceResult struct {
	WalletAddress string      `json:"wallet_address"`
	Balance       TokenAmount `json:"balance"`
	Provenance    Provenance  `json:"provenance"`
}

// SwapQuoteRequest asks for an exact-input quote.
// TokenPath includes the input token, optional intermediates,
// and the output token.
type SwapQuoteRequest struct {
	TokenPath   []Asset `json:"token_path"`
	AmountInRaw string  `json:"amount_in_raw"`
}

// SwapQuoteHop identifies the pool selected for one swap step.
type SwapQuoteHop struct {
	PoolAddress string `json:"pool_address"`
	Version     string `json:"version"`
	BinStep     uint16 `json:"bin_step"`
}

// SwapQuoteResult describes a read-only exact-input quote.
// A quote is not a signed transaction or a guarantee of execution.
type SwapQuoteResult struct {
	Protocol      string         `json:"protocol"`
	RouterAddress string         `json:"router_address"`
	TokenPath     []Asset        `json:"token_path"`
	Hops          []SwapQuoteHop `json:"hops"`
	AmountIn      TokenAmount    `json:"amount_in"`
	AmountOut     TokenAmount    `json:"amount_out"`
	Provenance    Provenance     `json:"provenance"`
}

// LendingAssetPosition describes one supplied or borrowed asset.
// Amounts use the underlying token's smallest units.
type LendingAssetPosition struct {
	Supplied          TokenAmount `json:"supplied"`
	VariableDebt      TokenAmount `json:"variable_debt"`
	StableDebt        TokenAmount `json:"stable_debt"`
	CollateralEnabled bool        `json:"collateral_enabled"`
}

// LendingPositionResult describes a wallet's lending positions.
type LendingPositionResult struct {
	Protocol        string                 `json:"protocol"`
	PoolAddress     string                 `json:"pool_address"`
	WalletAddress   string                 `json:"wallet_address"`
	Positions       []LendingAssetPosition `json:"positions"`
	HealthFactorRaw *string                `json:"health_factor_raw,omitempty"`
	Provenance      Provenance             `json:"provenance"`
}

// VaultPositionResult describes a wallet's share-based vault position.
// Assets represent the underlying amount corresponding to Shares;
// they do not guarantee that the full amount is immediately withdrawable.
type VaultPositionResult struct {
	Protocol      string      `json:"protocol"`
	VaultAddress  string      `json:"vault_address"`
	WalletAddress string      `json:"wallet_address"`
	Shares        TokenAmount `json:"shares"`
	Assets        TokenAmount `json:"assets"`
	Provenance    Provenance  `json:"provenance"`
}

// UnsignedTx contains transaction fields for wallet review and signing.
// Value is the native currency amount in Wei, as a decimal integer string.
// Data is hex-encoded calldata, including the 0x prefix.
type UnsignedTx struct {
	ChainID uint64 `json:"chain_id"`
	From    string `json:"from"`
	To      string `json:"to"`
	Value   string `json:"value"`
	Data    string `json:"data"`
}

// PreparedTransaction describes an unsigned transaction and its purpose.
// Creating this result must not sign or broadcast a transaction.
type PreparedTransaction struct {
	Action     string     `json:"action"`
	Protocol   string     `json:"protocol"`
	Tx         UnsignedTx `json:"tx"`
	Provenance Provenance `json:"provenance"`
}

// ChainReader provides native balance reads.
type ChainReader interface {
	ReadNativeBalance(
		ctx context.Context,
		walletAddress string,
	) (NativeBalanceResult, error)
}

// SwapQuoter provides read-only exact-input quotes.
type SwapQuoter interface {
	QuoteExactInput(
		ctx context.Context,
		request SwapQuoteRequest,
	) (SwapQuoteResult, error)
}

// LendingPositionReader provides wallet lending positions.
type LendingPositionReader interface {
	ReadLendingPosition(
		ctx context.Context,
		walletAddress string,
	) (LendingPositionResult, error)
}

// VaultPositionReader provides wallet positions in a share-based vault.
type VaultPositionReader interface {
	ReadVaultPosition(
		ctx context.Context,
		vaultAddress string,
		walletAddress string,
	) (VaultPositionResult, error)
}
