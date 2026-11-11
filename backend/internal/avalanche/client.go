package avalanche

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/ethclient"
)

const (
	FujiChainID = int64(43113)
	rpcTimeout  = 5 * time.Second
)

// ErrWrongChain indicates that the RPC endpoint uses an unexpected chain.
var ErrWrongChain = errors.New("unexpected Avalanche chain ID")

// Verify at compile time that ethclient.Client implements RPCReader.
var _ RPCReader = (*ethclient.Client)(nil)

// InitClient loads environment settings and initializes an RPC client.
// The caller must close the returned client.
func InitClient() (*ethclient.Client, error) {
	cfg, err := LoadRPCConfig()
	if err != nil {
		return nil, fmt.Errorf("load Avalanche RPC config: %w", err)
	}

	return NewRPCClient(context.Background(), cfg)
}

// NewRPCClient creates an RPC client and verifies its chain ID.
// The caller must close the returned client.
func NewRPCClient(
	ctx context.Context,
	cfg RPCConfig,
) (*ethclient.Client, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("validate Avalanche RPC config: %w", err)
	}

	startupCtx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()

	client, err := ethclient.DialContext(startupCtx, cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("connect to Avalanche RPC: %w", err)
	}

	chainID, err := client.ChainID(startupCtx)
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("get Avalanche chain ID: %w", err)
	}

	expectedChainID := big.NewInt(cfg.ExpectedChainID)
	if chainID.Cmp(expectedChainID) != 0 {
		client.Close()
		return nil, fmt.Errorf(
			"%w: got %s, want %s",
			ErrWrongChain,
			chainID.String(),
			expectedChainID.String(),
		)
	}

	return client, nil
}
