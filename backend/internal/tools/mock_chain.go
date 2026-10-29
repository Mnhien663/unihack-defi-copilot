package tools

import (
	"context"
	"time"

	"unihack-backend/internal/avalanche"
)

const mockNativeBalanceRaw = "2181886544100000121"

type MockChainClient struct{}

func NewMockChainClient() *MockChainClient {
	return &MockChainClient{}
}

func (m *MockChainClient) ReadNativeBalance(
	ctx context.Context,
	walletAddress string,
) (avalanche.NativeBalanceResult, error) {
	if err := ctx.Err(); err != nil {
		return avalanche.NativeBalanceResult{}, err
	}

	blockNumber := uint64(60034644)

	return avalanche.NativeBalanceResult{
		WalletAddress: walletAddress,
		Balance: avalanche.TokenAmount{
			Asset: avalanche.Asset{
				ChainID:  uint64(avalanche.FujiChainID),
				Symbol:   "AVAX",
				Decimals: 18,
			},
			Raw: mockNativeBalanceRaw,
		},
		Provenance: avalanche.Provenance{
			Source:      "mock_chain",
			ChainID:     uint64(avalanche.FujiChainID),
			BlockNumber: &blockNumber,
			ObservedAt:  time.Now().UTC(),
			IsDemo:      true,
		},
	}, nil
}

var _ ChainClient = (*MockChainClient)(nil)
