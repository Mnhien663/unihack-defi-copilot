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
	"github.com/ethereum/go-ethereum/core/types"
)

// AaveAccountPosition contains aggregates across all account reserves.
// Base amounts use BaseCurrencyUnitRaw, not token decimals.
type AaveAccountPosition struct {
	TotalCollateralBaseRaw         string `json:"total_collateral_base_raw"`
	TotalDebtBaseRaw               string `json:"total_debt_base_raw"`
	AvailableBorrowsBaseRaw        string `json:"available_borrows_base_raw"`
	CurrentLiquidationThresholdBps uint64 `json:"current_liquidation_threshold_bps"`
	LTVBps                         uint64 `json:"ltv_bps"`
	HealthFactorRaw                string `json:"health_factor_raw"`
	HasDebt                        bool   `json:"has_debt"`
}

// AaveReservePosition covers one explicitly selected underlying asset.
// It does not represent all reserves held by the wallet.
type AaveReservePosition struct {
	Asset                          Asset  `json:"asset"`
	SuppliedRaw                    string `json:"supplied_raw"`
	StableDebtRaw                  string `json:"stable_debt_raw"`
	VariableDebtRaw                string `json:"variable_debt_raw"`
	UserCollateralEnabled          bool   `json:"user_collateral_enabled"`
	ReserveCollateralEnabled       bool   `json:"reserve_collateral_enabled"`
	ReserveLTVBps                  uint64 `json:"reserve_ltv_bps"`
	ReserveLiquidationThresholdBps uint64 `json:"reserve_liquidation_threshold_bps"`
	BorrowingEnabled               bool   `json:"borrowing_enabled"`
	IsActive                       bool   `json:"is_active"`
	IsFrozen                       bool   `json:"is_frozen"`
}

// OraclePriceObservation preserves source data without claiming freshness.
type OraclePriceObservation struct {
	OracleAddress       string  `json:"oracle_address"`
	SourceAddress       string  `json:"source_address"`
	BaseCurrency        string  `json:"base_currency"`
	BaseCurrencyUnitRaw string  `json:"base_currency_unit_raw"`
	PriceRaw            string  `json:"price_raw"`
	SourceDecimals      *uint8  `json:"source_decimals"`
	SourceAnswerRaw     *string `json:"source_answer_raw"`
	RoundIDRaw          *string `json:"round_id_raw"`
	AnsweredInRoundRaw  *string `json:"answered_in_round_raw"`
	UpdatedAt           *uint64 `json:"updated_at"`
	AgeSeconds          *uint64 `json:"age_seconds"`
	FreshnessStatus     string  `json:"freshness_status"`
}

// AavePositionResult combines account aggregates and one reserve.
// Oracle price applies to Reserve.Asset only.
type AavePositionResult struct {
	WalletAddress       string                 `json:"wallet_address"`
	PoolAddress         string                 `json:"pool_address"`
	DataProviderAddress string                 `json:"data_provider_address"`
	BlockTimestamp      uint64                 `json:"block_timestamp"`
	Account             AaveAccountPosition    `json:"account"`
	Reserve             AaveReservePosition    `json:"reserve"`
	Oracle              OraclePriceObservation `json:"oracle"`
	Provenance          Provenance             `json:"provenance"`
}

type AavePositionReader interface {
	ReadPosition(
		ctx context.Context,
		walletAddress string,
	) (AavePositionResult, error)
}

const (
	aaveFujiAddressesProvider = "0x07D04EfAAA0Ac69D19d107795aF247C42Eb50F1C"
	aaveFujiPool              = "0x8B9b2AF4afB389b4a70A474dfD4AdCD4a302bb40"
	aaveFujiDataProvider      = "0xC65cbd1e309Bf0e841Ee6f6E786480598e6a4014"
	aaveFujiUSDC              = "0x5425890298aed601595a70AB815c96711a31Bc65"
)

const (
	OracleFreshnessUnknown = "unknown"
	OracleFreshnessFresh   = "fresh"
	OracleFreshnessStale   = "stale"
)

var (
	ErrPositionSourceMismatch = errors.New(
		"position source does not match configured market",
	)
	ErrOraclePriceUnavailable = errors.New(
		"oracle price is unavailable or invalid",
	)
	ErrOraclePriceStale = errors.New("oracle price exceeds configured age limit")
)

// PositionRPCClient adds block timestamps to existing RPC reads.
// A successfully initialized ethclient.Client supports these methods.
type PositionRPCClient interface {
	RPCReader

	HeaderByNumber(
		ctx context.Context,
		number *big.Int,
	) (*types.Header, error)
}

type AavePositionConfig struct {
	// Zero means no age policy has been configured.
	// In that case, freshness must remain unknown.
	MaxOracleAgeSeconds uint64
}

type RPCAavePositionReader struct {
	client            PositionRPCClient
	addressesProvider common.Address
	pool              common.Address
	dataProvider      common.Address
	asset             common.Address
	maxOracleAge      uint64
	abi               abi.ABI
}

func NewAavePositionReader(
	client PositionRPCClient,
	config AavePositionConfig,
) (*RPCAavePositionReader, error) {
	if client == nil {
		return nil, errors.New("position RPC client is nil")
	}

	parsed, err := abi.JSON(strings.NewReader(aavePositionABIJSON))
	if err != nil {
		return nil, fmt.Errorf("parse position ABI: %w", err)
	}

	return &RPCAavePositionReader{
		client:            client,
		addressesProvider: common.HexToAddress(aaveFujiAddressesProvider),
		pool:              common.HexToAddress(aaveFujiPool),
		dataProvider:      common.HexToAddress(aaveFujiDataProvider),
		asset:             common.HexToAddress(aaveFujiUSDC),
		maxOracleAge:      config.MaxOracleAgeSeconds,
		abi:               parsed,
	}, nil
}

const aavePositionABIJSON = `[
	{
		"type":"function","name":"getPool","stateMutability":"view",
		"inputs":[],"outputs":[{"type":"address"}]
	},
	{
		"type":"function","name":"getPriceOracle","stateMutability":"view",
		"inputs":[],"outputs":[{"type":"address"}]
	},
	{
		"type":"function","name":"ADDRESSES_PROVIDER","stateMutability":"view",
		"inputs":[],"outputs":[{"type":"address"}]
	},
	{
		"type":"function","name":"BASE_CURRENCY","stateMutability":"view",
		"inputs":[],"outputs":[{"type":"address"}]
	},
	{
		"type":"function","name":"BASE_CURRENCY_UNIT","stateMutability":"view",
		"inputs":[],"outputs":[{"type":"uint256"}]
	},
	{
		"type":"function","name":"getAssetPrice","stateMutability":"view",
		"inputs":[{"name":"asset","type":"address"}],
		"outputs":[{"type":"uint256"}]
	},
	{
		"type":"function","name":"getSourceOfAsset","stateMutability":"view",
		"inputs":[{"name":"asset","type":"address"}],
		"outputs":[{"type":"address"}]
	},
	{
		"type":"function","name":"decimals","stateMutability":"view",
		"inputs":[],"outputs":[{"type":"uint8"}]
	},
	{
		"type":"function","name":"latestRoundData","stateMutability":"view",
		"inputs":[],
		"outputs":[
			{"type":"uint80"},{"type":"int256"},{"type":"uint256"},
			{"type":"uint256"},{"type":"uint80"}
		]
	},
	{
		"type":"function","name":"getUserAccountData","stateMutability":"view",
		"inputs":[{"name":"user","type":"address"}],
		"outputs":[
			{"type":"uint256"},{"type":"uint256"},{"type":"uint256"},
			{"type":"uint256"},{"type":"uint256"},{"type":"uint256"}
		]
	},
	{
		"type":"function","name":"getReserveConfigurationData",
		"stateMutability":"view",
		"inputs":[{"name":"asset","type":"address"}],
		"outputs":[
			{"type":"uint256"},{"type":"uint256"},{"type":"uint256"},
			{"type":"uint256"},{"type":"uint256"},{"type":"bool"},
			{"type":"bool"},{"type":"bool"},{"type":"bool"},{"type":"bool"}
		]
	},
	{
		"type":"function","name":"getUserReserveData","stateMutability":"view",
		"inputs":[
			{"name":"asset","type":"address"},{"name":"user","type":"address"}
		],
		"outputs":[
			{"type":"uint256"},{"type":"uint256"},{"type":"uint256"},
			{"type":"uint256"},{"type":"uint256"},{"type":"uint256"},
			{"type":"uint256"},{"type":"uint40"},{"type":"bool"}
		]
	}
]`

func (r *RPCAavePositionReader) callPositionMethod(
	ctx context.Context,
	contractABI abi.ABI,
	address common.Address,
	block *big.Int,
	method string,
	args ...interface{},
) ([]interface{}, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if block == nil {
		return nil, errors.New("position read requires a pinned block")
	}

	definition, exists := contractABI.Methods[method]
	if !exists {
		return nil, fmt.Errorf("unknown position method: %s", method)
	}

	data, err := contractABI.Pack(method, args...)
	if err != nil {
		return nil, fmt.Errorf("encode position %s: %w", method, err)
	}

	raw, err := r.client.CallContract(
		ctx,
		ethereum.CallMsg{To: &address, Data: data},
		block,
	)
	if err != nil {
		return nil, fmt.Errorf("call position %s: %w", method, err)
	}

	// Every output in this minimal ABI is a static one-word value.
	expectedBytes := len(definition.Outputs) * 32
	if len(raw) != expectedBytes {
		return nil, fmt.Errorf(
			"position %s returned %d bytes; expected %d",
			method, len(raw), expectedBytes,
		)
	}

	values, err := contractABI.Unpack(method, raw)
	if err != nil {
		return nil, fmt.Errorf("decode position %s: %w", method, err)
	}
	return values, nil
}

func (r *RPCAavePositionReader) readPositionBalances(
	ctx context.Context,
	wallet common.Address,
	block *big.Int,
) (AaveAccountPosition, AaveReservePosition, error) {
	var account AaveAccountPosition
	var reserve AaveReservePosition

	accountValues, err := r.callPositionMethod(
		ctx, r.abi, r.pool, block, "getUserAccountData", wallet,
	)
	if err != nil {
		return account, reserve, err
	}

	// These six ABI outputs are all uint256.
	accountNumbers := make([]*big.Int, 6)
	for i, value := range accountValues {
		number, ok := value.(*big.Int)
		if !ok || number == nil || number.Sign() < 0 ||
			number.BitLen() > 256 {
			return account, reserve, errors.New(
				"invalid account amount",
			)
		}
		accountNumbers[i] = number
	}

	if accountNumbers[3].Cmp(big.NewInt(10_000)) > 0 ||
		accountNumbers[4].Cmp(big.NewInt(10_000)) > 0 {
		return account, reserve, errors.New(
			"account threshold or LTV exceeds 10000 bps",
		)
	}

	account = AaveAccountPosition{
		TotalCollateralBaseRaw:         accountNumbers[0].String(),
		TotalDebtBaseRaw:               accountNumbers[1].String(),
		AvailableBorrowsBaseRaw:        accountNumbers[2].String(),
		CurrentLiquidationThresholdBps: accountNumbers[3].Uint64(),
		LTVBps:                         accountNumbers[4].Uint64(),
		HealthFactorRaw:                accountNumbers[5].String(),
		HasDebt:                        accountNumbers[1].Sign() > 0,
	}

	config, err := r.callPositionMethod(
		ctx, r.abi, r.dataProvider, block,
		"getReserveConfigurationData", r.asset,
	)
	if err != nil {
		return AaveAccountPosition{}, reserve, err
	}

	configNumbers := make([]*big.Int, 5)
	for i := range configNumbers {
		number, ok := config[i].(*big.Int)
		if !ok || number == nil || number.Sign() < 0 {
			return AaveAccountPosition{}, reserve, errors.New(
				"invalid reserve configuration amount",
			)
		}
		configNumbers[i] = number
	}

	// The initial adapter supports only the configured six-decimal USDC.
	if configNumbers[0].Cmp(big.NewInt(6)) != 0 {
		return AaveAccountPosition{}, reserve, fmt.Errorf(
			"%w: unexpected USDC decimals",
			ErrPositionSourceMismatch,
		)
	}
	if configNumbers[1].Cmp(big.NewInt(10_000)) > 0 ||
		configNumbers[2].Cmp(big.NewInt(10_000)) > 0 ||
		configNumbers[1].Cmp(configNumbers[2]) > 0 {
		return AaveAccountPosition{}, reserve, errors.New(
			"invalid reserve LTV or liquidation threshold",
		)
	}

	userValues, err := r.callPositionMethod(
		ctx, r.abi, r.dataProvider, block,
		"getUserReserveData", r.asset, wallet,
	)
	if err != nil {
		return AaveAccountPosition{}, reserve, err
	}

	balances := make([]*big.Int, 3)
	for i := range balances {
		number, ok := userValues[i].(*big.Int)
		if !ok || number == nil || number.Sign() < 0 ||
			number.BitLen() > 256 {
			return AaveAccountPosition{}, reserve, errors.New(
				"invalid reserve balance or debt",
			)
		}
		balances[i] = number
	}

	// ABI decoding above guarantees these output positions are bool.
	reserve = AaveReservePosition{
		Asset: Asset{
			ChainID:  uint64(FujiChainID),
			Address:  r.asset.Hex(),
			Symbol:   "USDC",
			Decimals: uint8(configNumbers[0].Uint64()),
		},
		SuppliedRaw:                    balances[0].String(),
		StableDebtRaw:                  balances[1].String(),
		VariableDebtRaw:                balances[2].String(),
		UserCollateralEnabled:          userValues[8].(bool),
		ReserveCollateralEnabled:       config[5].(bool),
		ReserveLTVBps:                  configNumbers[1].Uint64(),
		ReserveLiquidationThresholdBps: configNumbers[2].Uint64(),
		BorrowingEnabled:               config[6].(bool),
		IsActive:                       config[8].(bool),
		IsFrozen:                       config[9].(bool),
	}

	return account, reserve, nil
}

func (r *RPCAavePositionReader) readPositionOracle(
	ctx context.Context,
	oracle common.Address,
	block *big.Int,
	blockTimestamp uint64,
) (OraclePriceObservation, error) {
	var result OraclePriceObservation

	call := func(address common.Address, method string, args ...interface{}) (
		[]interface{}, error,
	) {
		return r.callPositionMethod(
			ctx, r.abi, address, block, method, args...,
		)
	}

	baseValues, err := call(oracle, "BASE_CURRENCY")
	if err != nil {
		return result, err
	}
	base := baseValues[0].(common.Address)

	// This initial adapter supports the verified USD-based market.
	if base != (common.Address{}) {
		return result, fmt.Errorf(
			"%w: expected USD base currency",
			ErrPositionSourceMismatch,
		)
	}

	unitValues, err := call(oracle, "BASE_CURRENCY_UNIT")
	if err != nil {
		return result, err
	}
	unit := unitValues[0].(*big.Int)
	if unit.Sign() <= 0 {
		return result, ErrOraclePriceUnavailable
	}

	priceValues, err := call(oracle, "getAssetPrice", r.asset)
	if err != nil {
		return result, err
	}
	price := priceValues[0].(*big.Int)
	if price.Sign() <= 0 {
		return result, ErrOraclePriceUnavailable
	}

	sourceValues, err := call(oracle, "getSourceOfAsset", r.asset)
	if err != nil {
		return result, err
	}
	source := sourceValues[0].(common.Address)
	if source == (common.Address{}) {
		return result, fmt.Errorf(
			"%w: asset has no direct price source",
			ErrOraclePriceUnavailable,
		)
	}

	decimalValues, err := call(source, "decimals")
	if err != nil {
		return result, fmt.Errorf("read oracle source decimals: %w", err)
	}
	decimals := decimalValues[0].(uint8)

	sourceUnit := new(big.Int).Exp(
		big.NewInt(10), big.NewInt(int64(decimals)), nil,
	)
	if unit.Cmp(sourceUnit) != 0 {
		return result, fmt.Errorf(
			"%w: oracle and source price scales differ",
			ErrOraclePriceUnavailable,
		)
	}

	roundValues, err := call(source, "latestRoundData")
	if err != nil {
		return result, fmt.Errorf("read oracle source round: %w", err)
	}

	roundID := roundValues[0].(*big.Int)
	answer := roundValues[1].(*big.Int)
	startedAt := roundValues[2].(*big.Int)
	updatedAt := roundValues[3].(*big.Int)
	answeredInRound := roundValues[4].(*big.Int)

	if roundID.Sign() <= 0 ||
		answer.Sign() <= 0 ||
		answer.Cmp(price) != 0 ||
		answeredInRound.Cmp(roundID) < 0 {
		return result, fmt.Errorf(
			"%w: inconsistent source answer or round",
			ErrOraclePriceUnavailable,
		)
	}

	if !startedAt.IsUint64() || !updatedAt.IsUint64() ||
		startedAt.Sign() == 0 || updatedAt.Sign() == 0 ||
		startedAt.Cmp(updatedAt) > 0 ||
		updatedAt.Uint64() > blockTimestamp {
		return result, fmt.Errorf(
			"%w: invalid source timestamps",
			ErrOraclePriceUnavailable,
		)
	}

	updated := updatedAt.Uint64()
	age := blockTimestamp - updated
	freshness := OracleFreshnessUnknown

	if r.maxOracleAge > 0 {
		if age > r.maxOracleAge {
			return result, fmt.Errorf(
				"%w: age=%d seconds, limit=%d",
				ErrOraclePriceStale, age, r.maxOracleAge,
			)
		}
		// Fresh only under the configured application age policy.
		freshness = OracleFreshnessFresh
	}

	answerRaw := answer.String()
	roundRaw := roundID.String()
	answeredRaw := answeredInRound.String()

	return OraclePriceObservation{
		OracleAddress:       oracle.Hex(),
		SourceAddress:       source.Hex(),
		BaseCurrency:        base.Hex(),
		BaseCurrencyUnitRaw: unit.String(),
		PriceRaw:            price.String(),
		SourceDecimals:      &decimals,
		SourceAnswerRaw:     &answerRaw,
		RoundIDRaw:          &roundRaw,
		AnsweredInRoundRaw:  &answeredRaw,
		UpdatedAt:           &updated,
		AgeSeconds:          &age,
		FreshnessStatus:     freshness,
	}, nil
}

func (r *RPCAavePositionReader) ReadPosition(
	ctx context.Context,
	walletAddress string,
) (AavePositionResult, error) {
	if err := ctx.Err(); err != nil {
		return AavePositionResult{}, err
	}

	walletAddress = strings.TrimSpace(walletAddress)
	if !common.IsHexAddress(walletAddress) {
		return AavePositionResult{}, fmt.Errorf(
			"%w: position wallet",
			ErrInvalidAddress,
		)
	}
	wallet := common.HexToAddress(walletAddress)
	if wallet == (common.Address{}) {
		return AavePositionResult{}, fmt.Errorf(
			"%w: position wallet cannot be zero",
			ErrInvalidAddress,
		)
	}

	readCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	chainID, err := r.client.ChainID(readCtx)
	if err != nil {
		return AavePositionResult{}, fmt.Errorf(
			"verify position chain: %w", err,
		)
	}
	if chainID == nil || chainID.Cmp(big.NewInt(FujiChainID)) != 0 {
		return AavePositionResult{}, ErrWrongChain
	}

	block, err := r.client.BlockNumber(readCtx)
	if err != nil {
		return AavePositionResult{}, fmt.Errorf(
			"get position block: %w", err,
		)
	}
	blockNumber := new(big.Int).SetUint64(block)

	header, err := r.client.HeaderByNumber(readCtx, blockNumber)
	if err != nil {
		return AavePositionResult{}, fmt.Errorf(
			"read position block header: %w", err,
		)
	}
	if header == nil || header.Number == nil ||
		header.Number.Cmp(blockNumber) != 0 || header.Time == 0 {
		return AavePositionResult{}, errors.New(
			"RPC returned an invalid position block header",
		)
	}

	readAddress := func(
		target common.Address,
		method string,
	) (common.Address, error) {
		values, err := r.callPositionMethod(
			readCtx, r.abi, target, blockNumber, method,
		)
		if err != nil {
			return common.Address{}, err
		}

		address, ok := values[0].(common.Address)
		if !ok || address == (common.Address{}) {
			return common.Address{}, fmt.Errorf(
				"%w: invalid address from %s",
				ErrPositionSourceMismatch, method,
			)
		}
		return address, nil
	}

	pool, err := readAddress(r.addressesProvider, "getPool")
	if err != nil {
		return AavePositionResult{}, err
	}
	if pool != r.pool {
		return AavePositionResult{}, fmt.Errorf(
			"%w: pool changed",
			ErrPositionSourceMismatch,
		)
	}

	linkedProvider, err := readAddress(
		r.dataProvider, "ADDRESSES_PROVIDER",
	)
	if err != nil {
		return AavePositionResult{}, err
	}
	if linkedProvider != r.addressesProvider {
		return AavePositionResult{}, fmt.Errorf(
			"%w: data provider belongs to another market",
			ErrPositionSourceMismatch,
		)
	}

	oracleAddress, err := readAddress(
		r.addressesProvider, "getPriceOracle",
	)
	if err != nil {
		return AavePositionResult{}, err
	}

	account, reserve, err := r.readPositionBalances(
		readCtx, wallet, blockNumber,
	)
	if err != nil {
		return AavePositionResult{}, err
	}

	oracle, err := r.readPositionOracle(
		readCtx, oracleAddress, blockNumber, header.Time,
	)
	if err != nil {
		return AavePositionResult{}, err
	}

	// Reject a reorg observed during the multi-call read.
	finalHeader, err := r.client.HeaderByNumber(readCtx, blockNumber)
	if err != nil {
		return AavePositionResult{}, fmt.Errorf(
			"recheck position block header: %w", err,
		)
	}
	if finalHeader == nil || finalHeader.Number == nil ||
		finalHeader.Number.Cmp(blockNumber) != 0 ||
		finalHeader.Hash() != header.Hash() {
		return AavePositionResult{}, errors.New(
			"position block changed during read; retry required",
		)
	}

	if err := readCtx.Err(); err != nil {
		return AavePositionResult{}, err
	}

	return AavePositionResult{
		WalletAddress:       wallet.Hex(),
		PoolAddress:         r.pool.Hex(),
		DataProviderAddress: r.dataProvider.Hex(),
		BlockTimestamp:      header.Time,
		Account:             account,
		Reserve:             reserve,
		Oracle:              oracle,
		Provenance: Provenance{
			Source:      "Aave V3 Fuji",
			SourceType:  "rpc",
			ChainID:     uint64(FujiChainID),
			BlockNumber: &block,
			ObservedAt:  time.Now().UTC(),
			Mode:        "live",
			IsDemo:      false,
		},
	}, nil
}

var _ AavePositionReader = (*RPCAavePositionReader)(nil)
