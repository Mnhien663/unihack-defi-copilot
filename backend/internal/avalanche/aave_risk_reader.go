package avalanche

import (
	"context"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

type AaveRiskSnapshot struct {
	Position LendingPositionResult
	Assets   []LendingRiskAsset
	Oracle   string
	BaseUnit string
}

func (r *AaveLendingReader) ReadLendingRisk(
	ctx context.Context,
	walletAddress string,
) (AaveRiskSnapshot, error) {
	readCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	position, err := r.ReadLendingPosition(readCtx, walletAddress)
	if err != nil {
		return AaveRiskSnapshot{}, err
	}
	if position.Provenance.BlockNumber == nil {
		return AaveRiskSnapshot{}, fmt.Errorf("Aave position has no source block")
	}

	result := AaveRiskSnapshot{
		Position: position,
		Assets:   make([]LendingRiskAsset, 0, len(position.Positions)),
	}

	var hasDebt bool
	for _, p := range position.Positions {
		if p.StableDebt.Raw != "0" || p.VariableDebt.Raw != "0" {
			hasDebt = true
			break
		}
	}
	if !hasDebt {
		return result, nil
	}

	block := new(big.Int).SetUint64(*position.Provenance.BlockNumber)
	call := func(to common.Address, signature string, words int, args ...byte) ([]byte, error) {
		data := append(crypto.Keccak256([]byte(signature))[:4], args...)
		raw, err := r.client.CallContract(readCtx, ethereum.CallMsg{
			To:   &to,
			Data: data,
		}, block)
		if err != nil {
			return nil, fmt.Errorf("Aave %s at block %s: %w", signature, block, err)
		}
		if len(raw) != words*32 {
			return nil, fmt.Errorf("Aave %s returned %d bytes; want %d", signature, len(raw), words*32)
		}
		return raw, nil
	}

	wallet := common.HexToAddress(position.WalletAddress)
	mode, err := call(r.pool, "getUserEMode(address)", 1, common.LeftPadBytes(wallet.Bytes(), 32)...)
	if err != nil {
		return AaveRiskSnapshot{}, err
	}
	if new(big.Int).SetBytes(mode).Sign() != 0 {
		return AaveRiskSnapshot{}, fmt.Errorf("%w: eMode is not supported in v1", ErrLendingScenario)
	}

	providerWord, err := call(r.pool, "ADDRESSES_PROVIDER()", 1)
	if err != nil {
		return AaveRiskSnapshot{}, err
	}
	provider := common.BytesToAddress(providerWord)
	if provider == (common.Address{}) {
		return AaveRiskSnapshot{}, fmt.Errorf("Aave Addresses Provider is zero")
	}

	oracleWord, err := call(provider, "getPriceOracle()", 1)
	if err != nil {
		return AaveRiskSnapshot{}, err
	}
	oracle := common.BytesToAddress(oracleWord)
	if oracle == (common.Address{}) {
		return AaveRiskSnapshot{}, fmt.Errorf("Aave price oracle is zero")
	}

	unitWord, err := call(oracle, "BASE_CURRENCY_UNIT()", 1)
	if err != nil {
		return AaveRiskSnapshot{}, err
	}
	unit := new(big.Int).SetBytes(unitWord)
	if unit.Sign() <= 0 {
		return AaveRiskSnapshot{}, fmt.Errorf("invalid Aave oracle base unit")
	}
	result.Oracle = oracle.Hex()
	result.BaseUnit = unit.String()

	for _, p := range position.Positions {
		token := common.HexToAddress(p.Supplied.Asset.Address)
		tokenWord := common.LeftPadBytes(token.Bytes(), 32)

		supplied, err := aaveRiskQuantity(p.Supplied)
		if err != nil {
			return AaveRiskSnapshot{}, err
		}
		stable, err := aaveRiskQuantity(p.StableDebt)
		if err != nil {
			return AaveRiskSnapshot{}, err
		}
		variable, err := aaveRiskQuantity(p.VariableDebt)
		if err != nil {
			return AaveRiskSnapshot{}, err
		}
		debt := new(big.Rat).Add(stable, variable)

		item := LendingRiskAsset{
			AssetID:           token.Hex(),
			Supplied:          supplied,
			Debt:              debt,
			CollateralEnabled: p.CollateralEnabled,
		}

		if p.CollateralEnabled && supplied.Sign() > 0 {
			config, err := call(r.dataProvider, "getReserveConfigurationData(address)", 10, tokenWord...)
			if err != nil {
				return AaveRiskSnapshot{}, err
			}

			decimals := new(big.Int).SetBytes(config[:32])
			threshold := new(big.Int).SetBytes(config[64:96])
			if decimals.Cmp(big.NewInt(int64(p.Supplied.Asset.Decimals))) != 0 ||
				threshold.Cmp(big.NewInt(10_000)) > 0 {
				return AaveRiskSnapshot{}, fmt.Errorf("invalid Aave reserve configuration for %s", token.Hex())
			}
			item.LiquidationThresholdBPS = uint16(threshold.Uint64())
		}

		if debt.Sign() > 0 || item.LiquidationThresholdBPS > 0 {
			priceWord, err := call(oracle, "getAssetPrice(address)", 1, tokenWord...)
			if err != nil {
				return AaveRiskSnapshot{}, err
			}
			price := new(big.Int).SetBytes(priceWord)
			if price.Sign() <= 0 {
				return AaveRiskSnapshot{}, fmt.Errorf("%w: %s", ErrLendingPriceMissing, token.Hex())
			}
			item.OraclePrice = new(big.Rat).SetFrac(price, unit)
		}

		result.Assets = append(result.Assets, item)
	}

	return result, nil
}

func aaveRiskQuantity(amount TokenAmount) (*big.Rat, error) {
	raw, ok := new(big.Int).SetString(amount.Raw, 10)
	if !ok || raw.Sign() < 0 {
		return nil, fmt.Errorf("invalid Aave raw amount for %s", amount.Asset.Address)
	}

	scale := new(big.Int).Exp(
		big.NewInt(10),
		big.NewInt(int64(amount.Asset.Decimals)),
		nil,
	)
	return new(big.Rat).SetFrac(raw, scale), nil
}
