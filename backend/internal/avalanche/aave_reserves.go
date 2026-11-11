package avalanche

import (
	"context"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

type aaveUserReserve struct {
	Asset             common.Address
	SuppliedRaw       string
	StableDebtRaw     string
	VariableDebtRaw   string
	CollateralEnabled bool
}

func readAaveUserReserves(
	ctx context.Context,
	client RPCReader,
	dataProvider common.Address,
	wallet common.Address,
	block *big.Int,
	reserves []common.Address,
) ([]aaveUserReserve, error) {
	// Safety cap, not truncation: fail if the configured budget is exceeded.
	const maxReserves = 32

	if client == nil || block == nil || block.Sign() < 0 ||
		dataProvider == (common.Address{}) || wallet == (common.Address{}) {
		return nil, fmt.Errorf("invalid Aave reserve read configuration")
	}
	if len(reserves) == 0 || len(reserves) > maxReserves {
		return nil, fmt.Errorf(
			"Aave reserve count %d outside supported range 1..%d",
			len(reserves), maxReserves,
		)
	}

	seen := make(map[common.Address]bool, len(reserves))
	for _, reserve := range reserves {
		if reserve == (common.Address{}) || seen[reserve] {
			return nil, fmt.Errorf("invalid or duplicate Aave reserve: %s", reserve.Hex())
		}
		seen[reserve] = true
	}

	selector := crypto.Keccak256([]byte("getUserReserveData(address,address)"))[:4]
	walletWord := common.LeftPadBytes(wallet.Bytes(), 32)
	result := make([]aaveUserReserve, 0, len(reserves))

	for _, reserve := range reserves {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		data := make([]byte, 0, 68)
		data = append(data, selector...)
		data = append(data, common.LeftPadBytes(reserve.Bytes(), 32)...)
		data = append(data, walletWord...)

		raw, err := client.CallContract(ctx, ethereum.CallMsg{
			To:   &dataProvider,
			Data: data,
		}, block)
		if err != nil {
			return nil, fmt.Errorf("read Aave reserve %s: %w", reserve.Hex(), err)
		}
		if len(raw) != 9*32 {
			return nil, fmt.Errorf(
				"Aave reserve %s returned %d bytes; want %d",
				reserve.Hex(), len(raw), 9*32,
			)
		}

		collateral := new(big.Int).SetBytes(raw[8*32 : 9*32])
		if collateral.Cmp(big.NewInt(1)) > 0 {
			return nil, fmt.Errorf("invalid collateral flag for Aave reserve %s", reserve.Hex())
		}

		result = append(result, aaveUserReserve{
			Asset:             reserve,
			SuppliedRaw:       new(big.Int).SetBytes(raw[0:32]).String(),
			StableDebtRaw:     new(big.Int).SetBytes(raw[32:64]).String(),
			VariableDebtRaw:   new(big.Int).SetBytes(raw[64:96]).String(),
			CollateralEnabled: collateral.Sign() != 0,
		})
	}

	return result, nil
}
