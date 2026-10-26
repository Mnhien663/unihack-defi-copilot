package avalanche

import (
	"context"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
)

// GetLatestBlockNumber returns the newest block number from Avalanche.
func GetLatestBlockNumber(
	ctx context.Context,
	client *ethclient.Client,
) (uint64, error) {
	if client == nil {
		return 0, fmt.Errorf("Avalanche client is nil")
	}

	blockNumber, err := client.BlockNumber(ctx)
	if err != nil {
		return 0, fmt.Errorf("get latest Avalanche block number: %w", err)
	}

	return blockNumber, nil
}

// GetNativeBalance returns the wallet's native AVAX balance in Wei.
func GetNativeBalance(
	ctx context.Context,
	client *ethclient.Client,
	walletAddress string,
) (*big.Int, error) {
	if client == nil {
		return nil, fmt.Errorf("Avalanche client is nil")
	}

	walletAddress = strings.TrimSpace(walletAddress)
	if !common.IsHexAddress(walletAddress) {
		return nil, fmt.Errorf("invalid wallet address")
	}

	address := common.HexToAddress(walletAddress)

	balanceWei, err := client.BalanceAt(ctx, address, nil)
	if err != nil {
		return nil, fmt.Errorf("get native AVAX balance: %w", err)
	}

	return balanceWei, nil
}

// WeiToAVAX converts Wei to a human-readable AVAX string.
func WeiToAVAX(wei *big.Int) (string, error) {
	if wei == nil {
		return "", fmt.Errorf("Wei amount is nil")
	}

	if wei.Sign() < 0 {
		return "", fmt.Errorf("Wei amount cannot be negative")
	}

	weiPerAVAX := big.NewInt(1_000_000_000_000_000_000)

	avax := new(big.Rat).SetFrac(
		new(big.Int).Set(wei),
		weiPerAVAX,
	)

	result := avax.FloatString(18)
	result = strings.TrimRight(result, "0")
	result = strings.TrimRight(result, ".")

	if result == "" {
		return "0", nil
	}

	return result, nil
}
