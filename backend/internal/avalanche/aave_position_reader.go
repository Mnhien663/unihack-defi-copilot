package avalanche

import (
	"context"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
)

const aavePositionABI = `[
	{"type":"function","name":"getReservesList","inputs":[],
	 "outputs":[{"type":"address[]"}]},
	{"type":"function","name":"getUserAccountData","inputs":[{"type":"address"}],
	 "outputs":[{"type":"uint256"},{"type":"uint256"},{"type":"uint256"},
	            {"type":"uint256"},{"type":"uint256"},{"type":"uint256"}]}
]`

type AavePositionReader struct {
	client       RPCReader
	pool         common.Address
	dataProvider common.Address
	poolABI      abi.ABI
}

func NewAavePositionReader(
	client RPCReader,
	poolAddress string,
	dataProviderAddress string,
) (*AavePositionReader, error) {
	if client == nil {
		return nil, fmt.Errorf("Aave RPC client is nil")
	}

	poolAddress = strings.TrimSpace(poolAddress)
	dataProviderAddress = strings.TrimSpace(dataProviderAddress)
	if !common.IsHexAddress(poolAddress) || !common.IsHexAddress(dataProviderAddress) {
		return nil, fmt.Errorf("invalid Aave Pool or Data Provider address")
	}

	pool := common.HexToAddress(poolAddress)
	provider := common.HexToAddress(dataProviderAddress)
	if pool == (common.Address{}) || provider == (common.Address{}) || pool == provider {
		return nil, fmt.Errorf("invalid Aave Pool or Data Provider configuration")
	}

	poolABI, err := abi.JSON(strings.NewReader(aavePositionABI))
	if err != nil {
		return nil, fmt.Errorf("parse Aave Pool ABI: %w", err)
	}

	return &AavePositionReader{
		client:       client,
		pool:         pool,
		dataProvider: provider,
		poolABI:      poolABI,
	}, nil
}

func (r *AavePositionReader) callPool(
	ctx context.Context,
	block *big.Int,
	method string,
	args ...any,
) ([]any, error) {
	data, err := r.poolABI.Pack(method, args...)
	if err != nil {
		return nil, fmt.Errorf("encode Aave %s: %w", method, err)
	}

	raw, err := r.client.CallContract(ctx, ethereum.CallMsg{
		To:   &r.pool,
		Data: data,
	}, block)
	if err != nil {
		return nil, fmt.Errorf("call Aave %s: %w", method, err)
	}
	if method == "getUserAccountData" && len(raw) != 6*32 {
		return nil, fmt.Errorf("Aave account data returned %d bytes; want 192", len(raw))
	}

	values, err := r.poolABI.Unpack(method, raw)
	if err != nil {
		return nil, fmt.Errorf("decode Aave %s: %w", method, err)
	}
	return values, nil
}

func (r *AavePositionReader) ReadLendingPosition(
	ctx context.Context,
	walletAddress string,
) (LendingPositionResult, error) {
	walletAddress = strings.TrimSpace(walletAddress)
	if !common.IsHexAddress(walletAddress) {
		return LendingPositionResult{}, ErrInvalidAddress
	}
	wallet := common.HexToAddress(walletAddress)
	if wallet == (common.Address{}) {
		return LendingPositionResult{}, ErrInvalidAddress
	}

	readCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	chainID, err := r.client.ChainID(readCtx)
	if err != nil {
		return LendingPositionResult{}, fmt.Errorf("verify Aave RPC chain: %w", err)
	}
	if chainID == nil || chainID.Cmp(big.NewInt(FujiChainID)) != 0 {
		return LendingPositionResult{}, ErrWrongChain
	}

	height, err := r.client.BlockNumber(readCtx)
	if err != nil {
		return LendingPositionResult{}, fmt.Errorf("read Aave block: %w", err)
	}
	return r.readLendingPositionAtBlock(readCtx, wallet, height)
}

func (r *AavePositionReader) readLendingPositionAtBlock(
	readCtx context.Context,
	wallet common.Address,
	height uint64,
) (LendingPositionResult, error) {
	block := new(big.Int).SetUint64(height)

	account, err := r.callPool(readCtx, block, "getUserAccountData", wallet)
	if err != nil {
		return LendingPositionResult{}, err
	}
	totalDebtBase := account[1].(*big.Int)

	list, err := r.callPool(readCtx, block, "getReservesList")
	if err != nil {
		return LendingPositionResult{}, err
	}

	reserves := list[0].([]common.Address)
	userReserves, err := readAaveUserReserves(
		readCtx, r.client, r.dataProvider, wallet, block, reserves,
	)
	if err != nil {
		return LendingPositionResult{}, err
	}

	positions := make([]LendingAssetPosition, 0, len(userReserves))
	var hasReserveDebt bool

	for _, reserve := range userReserves {
		hasDebt := reserve.StableDebtRaw != "0" || reserve.VariableDebtRaw != "0"
		hasReserveDebt = hasReserveDebt || hasDebt
		if reserve.SuppliedRaw == "0" && !hasDebt {
			continue
		}

		address := reserve.Asset.Hex()
		decimals, err := GetERC20Decimals(readCtx, r.client, address, block)
		if err != nil {
			return LendingPositionResult{}, fmt.Errorf("read Aave token decimals: %w", err)
		}

		asset := Asset{
			ChainID:  uint64(FujiChainID),
			Address:  address,
			Decimals: decimals,
		}

		supplied, err := aavePositionAmount(asset, reserve.SuppliedRaw)
		if err != nil {
			return LendingPositionResult{}, err
		}
		stable, err := aavePositionAmount(asset, reserve.StableDebtRaw)
		if err != nil {
			return LendingPositionResult{}, err
		}
		variable, err := aavePositionAmount(asset, reserve.VariableDebtRaw)
		if err != nil {
			return LendingPositionResult{}, err
		}

		positions = append(positions, LendingAssetPosition{
			Supplied:          supplied,
			StableDebt:        stable,
			VariableDebt:      variable,
			CollateralEnabled: reserve.CollateralEnabled,
		})
	}

	if totalDebtBase.Sign() > 0 && !hasReserveDebt {
		return LendingPositionResult{}, fmt.Errorf(
			"Aave account reports debt but no reserve reports debt",
		)
	}

	var healthFactorRaw *string
	if totalDebtBase.Sign() > 0 {
		raw := account[5].(*big.Int).String()
		healthFactorRaw = &raw
	}

	return LendingPositionResult{
		Protocol:        "Aave V3",
		PoolAddress:     r.pool.Hex(),
		WalletAddress:   wallet.Hex(),
		Positions:       positions,
		HealthFactorRaw: healthFactorRaw,
		Provenance: Provenance{
			Source:      "Aave V3 via Avalanche RPC",
			SourceType:  "rpc",
			ChainID:     uint64(FujiChainID),
			BlockNumber: &height,
			ObservedAt:  time.Now().UTC(),
			Mode:        "live",
		},
	}, nil
}

func aavePositionAmount(asset Asset, raw string) (TokenAmount, error) {
	formatted, err := FormatTokenAmount(raw, asset.Decimals)
	if err != nil {
		return TokenAmount{}, fmt.Errorf("format Aave position amount: %w", err)
	}
	return TokenAmount{Asset: asset, Raw: raw, Formatted: formatted}, nil
}

var _ LendingPositionReader = (*AavePositionReader)(nil)
