package avalanche

import (
	"context"
	"fmt"
	"math/big"
	"os"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/ethclient"
)

const (
	FujiChainID = int64(43113)
	rpcTimeout  = 5 * time.Second
)

// InitClient creates and verifies a connection to Avalanche Fuji C-Chain.
func InitClient() (*ethclient.Client, error) {
	rpcURL := strings.TrimSpace(os.Getenv("AVALANCHE_RPC_URL"))
	if rpcURL == "" {
		return nil, fmt.Errorf("AVALANCHE_RPC_URL is not configured")
	}

	ctx, cancel := context.WithTimeout(context.Background(), rpcTimeout)
	defer cancel()

	client, err := ethclient.DialContext(ctx, rpcURL)
	if err != nil {
		return nil, fmt.Errorf("connect to Avalanche RPC: %w", err)
	}

	chainID, err := client.ChainID(ctx)
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("get Avalanche chain ID: %w", err)
	}

	expectedChainID := big.NewInt(FujiChainID)
	if chainID.Cmp(expectedChainID) != 0 {
		client.Close()

		return nil, fmt.Errorf(
			"unexpected Avalanche chain ID: got %s, want %s",
			chainID.String(),
			expectedChainID.String(),
		)
	}

	fmt.Printf(
		"Connected to Avalanche Fuji. Chain ID: %s\n",
		chainID.String(),
	)

	return client, nil
}
