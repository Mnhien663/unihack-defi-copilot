package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"unihack-backend/internal/avalanche"
)

type transactionClientStub struct {
	calls     int
	wallet    string
	pageSize  int
	pageToken string
	page      *avalanche.DataAPITransactionPage
	err       error
}

func (s *transactionClientStub) ListTransactions(
	ctx context.Context,
	wallet string,
	pageSize int,
	pageToken string,
) (*avalanche.DataAPITransactionPage, error) {
	s.calls++
	s.wallet = wallet
	s.pageSize = pageSize
	s.pageToken = pageToken

	return s.page, s.err
}

const historyTestWallet = "0x0000000000000000000000000000000000000001"

func TestListTransactionsToolPreservesPagination(t *testing.T) {
	client := &transactionClientStub{
		page: &avalanche.DataAPITransactionPage{
			Transactions: []avalanche.DataAPITransaction{
				{
					NativeTransaction: avalanche.DataAPINativeTransaction{
						ChainID: "43113",
						TxHash:  "0xexample",
					},
				},
			},
			NextPageToken: "next-cursor",
		},
	}

	tool, err := NewListTransactionsTool(client, "mock_chain")
	if err != nil {
		t.Fatal(err)
	}

	arguments, err := json.Marshal(map[string]any{
		"wallet_address": historyTestWallet,
		"chain_id":       43113,
		"page_size":      5,
		"page_token":     " opaque+/= ",
	})
	if err != nil {
		t.Fatal(err)
	}

	result, err := tool.Execute(context.Background(), arguments)
	if err != nil {
		t.Fatal(err)
	}
	if result.Error != nil {
		t.Fatalf("unexpected tool error: %+v", result.Error)
	}

	if client.calls != 1 ||
		client.wallet != historyTestWallet ||
		client.pageSize != 5 ||
		client.pageToken != " opaque+/= " {
		t.Fatalf("unexpected client arguments: %+v", client)
	}

	var payload struct {
		Transactions  []avalanche.DataAPITransaction `json:"transactions"`
		NextPageToken string                         `json:"next_page_token"`
	}
	if err := json.Unmarshal(result.Data, &payload); err != nil {
		t.Fatal(err)
	}

	if len(payload.Transactions) != 1 ||
		payload.NextPageToken != "next-cursor" {
		t.Fatalf("unexpected payload: %s", result.Data)
	}

	if result.Source != "mock_chain" ||
		result.ChainID != 43113 ||
		result.ObservedAt.IsZero() {
		t.Fatalf("missing provenance: %+v", result)
	}
}

func TestListTransactionsToolRejectsInvalidArguments(t *testing.T) {
	tests := []struct {
		name string
		args map[string]any
		code string
	}{
		{
			name: "invalid wallet",
			args: map[string]any{"wallet_address": "bad", "chain_id": 43113},
			code: "INVALID_WALLET_ADDRESS",
		},
		{
			name: "wrong chain",
			args: map[string]any{"wallet_address": historyTestWallet, "chain_id": 1},
			code: "UNSUPPORTED_CHAIN_ID",
		},
		{
			name: "explicit zero page size",
			args: map[string]any{
				"wallet_address": historyTestWallet,
				"chain_id":       43113, "page_size": 0,
			},
			code: "INVALID_PAGE_SIZE",
		},
		{
			name: "oversized page",
			args: map[string]any{
				"wallet_address": historyTestWallet,
				"chain_id":       43113, "page_size": 21,
			},
			code: "INVALID_PAGE_SIZE",
		},
		{
			name: "long cursor",
			args: map[string]any{
				"wallet_address": historyTestWallet,
				"chain_id":       43113,
				"page_token":     strings.Repeat("x", 2049),
			},
			code: "INVALID_PAGE_TOKEN",
		},
		{
			name: "unknown field",
			args: map[string]any{
				"wallet_address": historyTestWallet,
				"chain_id":       43113, "unexpected": true,
			},
			code: "INVALID_TOOL_ARGUMENTS",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &transactionClientStub{}
			tool, err := NewListTransactionsTool(client, "mock_chain")
			if err != nil {
				t.Fatal(err)
			}

			args, err := json.Marshal(tt.args)
			if err != nil {
				t.Fatal(err)
			}

			result, err := tool.Execute(context.Background(), args)
			if err != nil {
				t.Fatal(err)
			}
			if result.Error == nil || result.Error.Code != tt.code {
				t.Fatalf("want %s, got %+v", tt.code, result.Error)
			}
			if client.calls != 0 {
				t.Fatalf("client called %d times; want 0", client.calls)
			}
		})
	}
}

func TestListTransactionsToolNormalizesResults(t *testing.T) {
	tests := []struct {
		name string
		page *avalanche.DataAPITransactionPage
		err  error
		code string
	}{
		{
			name: "empty history",
			page: &avalanche.DataAPITransactionPage{},
			code: "EMPTY_HISTORY",
		},
		{
			name: "upstream timeout",
			err:  context.DeadlineExceeded,
			code: "UPSTREAM_TIMEOUT",
		},
		{
			name: "upstream error",
			err:  errors.New("connection failed"),
			code: "UPSTREAM_ERROR",
		},
		{
			name: "nil response",
			code: "UPSTREAM_INVALID_RESPONSE",
		},
		{
			name: "too many records",
			page: &avalanche.DataAPITransactionPage{
				Transactions: make([]avalanche.DataAPITransaction, 11),
			},
			code: "UPSTREAM_INVALID_RESPONSE",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &transactionClientStub{
				page: tt.page,
				err:  tt.err,
			}
			tool, err := NewListTransactionsTool(client, "mock_chain")
			if err != nil {
				t.Fatal(err)
			}

			result, err := tool.Execute(
				context.Background(),
				json.RawMessage(`{
					"wallet_address":"`+historyTestWallet+`",
					"chain_id":43113
				}`),
			)
			if err != nil {
				t.Fatal(err)
			}

			if result.Error == nil || result.Error.Code != tt.code {
				t.Fatalf("want %s, got %+v", tt.code, result.Error)
			}
			if client.pageSize != 10 {
				t.Fatalf("default page size = %d; want 10", client.pageSize)
			}

			if tt.code == "EMPTY_HISTORY" &&
				!strings.Contains(string(result.Data), `"transactions":[]`) {
				t.Fatalf("expected empty JSON array: %s", result.Data)
			}
		})
	}
}

func TestListTransactionsToolHonorsCancellation(t *testing.T) {
	client := &transactionClientStub{}
	tool, err := NewListTransactionsTool(client, "mock_chain")
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = tool.Execute(ctx, json.RawMessage(`{}`))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
	if client.calls != 0 {
		t.Fatalf("client called %d times; want 0", client.calls)
	}
}
