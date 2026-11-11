package tools

import (
	"context"
	"fmt"
	"time"
	"unihack-backend/internal/avalanche"

	"github.com/ethereum/go-ethereum/common"
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

func (m *MockChainClient) ListTransactions(
	ctx context.Context,
	walletAddress string,
	pageSize int,
	pageToken string,
) (*avalanche.DataAPITransactionPage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if pageSize < 1 || pageSize > 20 {
		return nil, fmt.Errorf("page size must be between 1 and 20")
	}

	// Two deterministic demo records, with real pagination behavior.
	transactions := []avalanche.DataAPITransaction{
		{
			NativeTransaction: avalanche.DataAPINativeTransaction{
				ChainID:        "43113",
				BlockNumber:    "60034644",
				BlockTimestamp: 1793628000,
				TxHash: fmt.Sprintf(
					"0x%064x",
					1,
				),
				TxStatus: "1",
				From: avalanche.DataAPIAddress{
					Address: walletAddress,
				},
				To: avalanche.DataAPIAddress{
					Address: "0x0000000000000000000000000000000000000002",
				},
				Value: "1000000000000000",
			},
		},
		{
			NativeTransaction: avalanche.DataAPINativeTransaction{
				ChainID:        "43113",
				BlockNumber:    "60034643",
				BlockTimestamp: 1793627900,
				TxHash: fmt.Sprintf(
					"0x%064x",
					2,
				),
				TxStatus: "1",
				From: avalanche.DataAPIAddress{
					Address: "0x0000000000000000000000000000000000000002",
				},
				To: avalanche.DataAPIAddress{
					Address: walletAddress,
				},
				Value: "2000000000000000",
			},
		},
	}

	start := 0
	switch pageToken {
	case "":
	case "mock-history-page-2":
		start = 1
	default:
		return nil, fmt.Errorf("invalid mock history cursor")
	}

	end := start + pageSize
	if end > len(transactions) {
		end = len(transactions)
	}

	nextPageToken := ""
	if end < len(transactions) {
		nextPageToken = "mock-history-page-2"
	}

	return &avalanche.DataAPITransactionPage{
		Transactions:  transactions[start:end],
		NextPageToken: nextPageToken,
	}, nil
}

func (m *MockChainClient) ReadAllowance(
	ctx context.Context,
	tokenAddress string,
	ownerAddress string,
	spenderAddress string,
) (avalanche.AllowanceResult, error) {
	if err := ctx.Err(); err != nil {
		return avalanche.AllowanceResult{}, err
	}

	for _, address := range []string{
		tokenAddress,
		ownerAddress,
		spenderAddress,
	} {
		if !common.IsHexAddress(address) {
			return avalanche.AllowanceResult{}, avalanche.ErrInvalidAddress
		}
	}

	token := common.HexToAddress(tokenAddress)

	// Demo allowlist: DDEMO only.
	if token != common.HexToAddress(
		"0xcCCb83Fbe349fAebDE74Ce633D25B282E6FDd21A",
	) {
		return avalanche.AllowanceResult{},
			avalanche.ErrAllowanceUnsupportedToken
	}

	const raw = "1500000000000000000"

	formatted, err := avalanche.FormatTokenAmount(raw, 18)
	if err != nil {
		return avalanche.AllowanceResult{}, err
	}

	block := uint64(60034644)

	return avalanche.AllowanceResult{
		OwnerAddress:   common.HexToAddress(ownerAddress).Hex(),
		SpenderAddress: common.HexToAddress(spenderAddress).Hex(),
		Allowance: avalanche.TokenAmount{
			Asset: avalanche.Asset{
				ChainID:  uint64(avalanche.FujiChainID),
				Address:  token.Hex(),
				Symbol:   "DDEMO",
				Decimals: 18,
			},
			Raw:       raw,
			Formatted: formatted,
		},
		Provenance: avalanche.Provenance{
			Source:      "mock_chain",
			SourceType:  "mock",
			ChainID:     uint64(avalanche.FujiChainID),
			BlockNumber: &block,
			ObservedAt:  time.Now().UTC(),
			Mode:        "mock",
			IsDemo:      true,
		},
	}, nil
}

var _ avalanche.AllowanceReader = (*MockChainClient)(nil)
var _ TransactionClient = (*MockChainClient)(nil)
var _ ChainClient = (*MockChainClient)(nil)
var _ TokenBalanceClient = (*MockChainClient)(nil)
