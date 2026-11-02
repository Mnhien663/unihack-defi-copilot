package tools

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"unihack-backend/internal/avalanche"
)

type mockTokenBalanceClient struct {
	result            avalanche.TokenBalancePage
	err               error
	callCount         int
	receivedWallet    string
	receivedPageSize  int
	receivedPageToken string
}

func (m *mockTokenBalanceClient) ListTokenBalances(
	ctx context.Context,
	walletAddress string,
	pageSize int,
	pageToken string,
) (avalanche.TokenBalancePage, error) {
	m.callCount++
	m.receivedWallet = walletAddress
	m.receivedPageSize = pageSize
	m.receivedPageToken = pageToken

	return m.result, m.err
}

func TestListTokenBalancesToolExecutes(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(
		2026,
		time.November,
		2,
		0,
		0,
		0,
		0,
		time.UTC,
	)

	client := &mockTokenBalanceClient{
		result: avalanche.TokenBalancePage{
			WalletAddress: "0x0000000000000000000000000000000000000001",
			Balances: []avalanche.TokenBalance{
				{
					Name: "USD Coin",
					Amount: avalanche.TokenAmount{
						Asset: avalanche.Asset{
							ChainID:  43113,
							Symbol:   "USDC",
							Decimals: 6,
						},
						Raw:       "1234567",
						Formatted: "1.234567",
					},
				},
			},
			NextPageToken: "next-page",
			Provenance: avalanche.Provenance{
				Source:     "test_data_api",
				ChainID:    43113,
				ObservedAt: observedAt,
			},
		},
	}

	tool, err := NewListTokenBalancesTool(client)
	if err != nil {
		t.Fatalf("create token balance tool: %v", err)
	}

	result, err := tool.Execute(
		context.Background(),
		json.RawMessage(`{
			"wallet_address":
				"0x0000000000000000000000000000000000000001",
			"chain_id": 43113,
			"page_token": "cursor-1"
		}`),
	)
	if err != nil {
		t.Fatalf("execute token balance tool: %v", err)
	}

	if result.Error != nil {
		t.Fatalf("unexpected tool error: %+v", result.Error)
	}

	if client.callCount != 1 {
		t.Fatalf(
			"call count = %d, want 1",
			client.callCount,
		)
	}

	if client.receivedPageSize != defaultTokenPageSize {
		t.Fatalf(
			"page size = %d, want %d",
			client.receivedPageSize,
			defaultTokenPageSize,
		)
	}

	if client.receivedPageToken != "cursor-1" {
		t.Fatalf(
			"page token = %q",
			client.receivedPageToken,
		)
	}

	if result.Source != "test_data_api" {
		t.Fatalf("source = %q", result.Source)
	}

	var page avalanche.TokenBalancePage

	if err := json.Unmarshal(result.Data, &page); err != nil {
		t.Fatalf("decode tool data: %v", err)
	}

	if page.Balances[0].Amount.Formatted != "1.234567" {
		t.Fatalf(
			"formatted amount = %q",
			page.Balances[0].Amount.Formatted,
		)
	}
}

func TestListTokenBalancesToolRejectsUnknownField(
	t *testing.T,
) {
	t.Parallel()

	client := &mockTokenBalanceClient{}

	tool, err := NewListTokenBalancesTool(client)
	if err != nil {
		t.Fatalf("create token balance tool: %v", err)
	}

	result, err := tool.Execute(
		context.Background(),
		json.RawMessage(`{
			"wallet_address":
				"0x0000000000000000000000000000000000000001",
			"chain_id": 43113,
			"unexpected": true
		}`),
	)
	if err != nil {
		t.Fatalf("execute token balance tool: %v", err)
	}

	if result.Error == nil ||
		result.Error.Code != "INVALID_TOOL_ARGUMENTS" {
		t.Fatalf(
			"unexpected validation result: %+v",
			result.Error,
		)
	}

	if client.callCount != 0 {
		t.Fatal("invalid arguments must not call client")
	}
}
