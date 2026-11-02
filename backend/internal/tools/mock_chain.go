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

	formatted, err := avalanche.FormatTokenAmount(
		mockNativeBalanceRaw,
		18,
	)
	if err != nil {
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
			Raw:       mockNativeBalanceRaw,
			Formatted: formatted,
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

func (m *MockChainClient) ListTokenBalances(
	ctx context.Context,
	walletAddress string,
	pageSize int,
	pageToken string,
) (avalanche.TokenBalancePage, error) {
	if err := ctx.Err(); err != nil {
		return avalanche.TokenBalancePage{}, err
	}

	return avalanche.TokenBalancePage{
		WalletAddress: walletAddress,
		Balances: []avalanche.TokenBalance{
			{
				Name: "USD Coin",
				Amount: avalanche.TokenAmount{
					Asset: avalanche.Asset{
						ChainID: uint64(
							avalanche.FujiChainID,
						),
						Address:  "0xB97EF9Ef8734C71904D8002F8b6Bc66Dd9c48a6E",
						Symbol:   "USDC",
						Decimals: 6,
					},
					Raw:       "1234567",
					Formatted: "1.234567",
				},
				TokenReputation: "Verified",
			},
			{
				Name: "DeFi Copilot Demo Token",
				Amount: avalanche.TokenAmount{
					Asset: avalanche.Asset{
						ChainID: uint64(
							avalanche.FujiChainID,
						),
						Address:  "0xcCCb83Fbe349fAebDE74Ce633D25B282E6FDd21A",
						Symbol:   "DDEMO",
						Decimals: 18,
					},
					Raw:       "2500000000000000000",
					Formatted: "2.5",
				},
				TokenReputation: "Unknown",
			},
		},
		NextPageToken: "",
		Provenance: avalanche.Provenance{
			Source:      "mock_chain",
			ChainID:     uint64(avalanche.FujiChainID),
			BlockNumber: nil,
			ObservedAt:  time.Now().UTC(),
			IsDemo:      true,
		},
	}, nil
}

var _ ChainClient = (*MockChainClient)(nil)
var _ TokenBalanceClient = (*MockChainClient)(nil)
