package avalanche

import (
	"context"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
)

// dataAPITestTransport lets tests return responses without calling the network.
type dataAPITestTransport func(*http.Request) (*http.Response, error)

func (f dataAPITestTransport) RoundTrip(
	req *http.Request,
) (*http.Response, error) {
	return f(req)
}

func TestDataAPINativeBalanceFromFixture(t *testing.T) {
	fixture, err := os.ReadFile("testdata/fuji_native_balance.json")
	if err != nil {
		t.Fatal(err)
	}

	client := NewDataAPIClient("")

	client.httpClient.Transport = dataAPITestTransport(
		func(req *http.Request) (*http.Response, error) {
			if req.Method != http.MethodGet {
				t.Errorf("method = %s, want GET", req.Method)
			}

			expectedURL := dataAPIBaseURL +
				"/v1/chains/43113/addresses/" +
				"0x4DFb0d91C07A7C706F65F974953cE8b111D27Ee5" +
				"/balances:getNative"

			if req.URL.String() != expectedURL {
				t.Errorf("unexpected request URL: %s", req.URL)
			}

			if req.Header.Get("x-glacier-api-key") != "" {
				t.Error("empty API key should not be sent")
			}

			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(string(fixture))),
				Header:     make(http.Header),
			}, nil
		},
	)

	balance, err := client.GetNativeBalance(
		context.Background(),
		"0x4DFb0d91C07A7C706F65F974953cE8b111D27Ee5",
	)
	if err != nil {
		t.Fatal(err)
	}

	if balance.ChainID != "43113" {
		t.Errorf("chain ID = %s, want 43113", balance.ChainID)
	}
	if balance.Symbol != "AVAX" || balance.Decimals != 18 {
		t.Errorf("unexpected token metadata: %+v", balance)
	}
	if balance.Balance == "" {
		t.Error("raw balance must not be empty")
	}
}

func TestDataAPINativeBalanceRejectsInvalidAddress(t *testing.T) {
	client := NewDataAPIClient("")
	called := false

	client.httpClient.Transport = dataAPITestTransport(
		func(req *http.Request) (*http.Response, error) {
			called = true
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("{}")),
				Header:     make(http.Header),
			}, nil
		},
	)

	balance, err := client.GetNativeBalance(
		context.Background(),
		"not-a-wallet",
	)

	if err == nil {
		t.Fatal("expected invalid address error")
	}
	if balance != nil {
		t.Error("balance should be nil on error")
	}
	if called {
		t.Error("invalid address must not trigger an HTTP request")
	}
}

func TestDataAPINativeBalanceHandlesRateLimit(t *testing.T) {
	client := NewDataAPIClient("")

	client.httpClient.Transport = dataAPITestTransport(
		func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusTooManyRequests,
				Body:       io.NopCloser(strings.NewReader("{}")),
				Header:     make(http.Header),
			}, nil
		},
	)

	balance, err := client.GetNativeBalance(
		context.Background(),
		"0x4DFb0d91C07A7C706F65F974953cE8b111D27Ee5",
	)

	if err == nil {
		t.Fatal("expected HTTP 429 error")
	}
	if !strings.Contains(err.Error(), "HTTP 429") {
		t.Errorf("unexpected error: %v", err)
	}
	if balance != nil {
		t.Error("balance should be nil on error")
	}
}

func TestDataAPITransactionsFromFixture(t *testing.T) {
	fixture, err := os.ReadFile("testdata/fuji_transactions.json")
	if err != nil {
		t.Fatal(err)
	}

	client := NewDataAPIClient("")
	called := false

	client.httpClient.Transport = dataAPITestTransport(
		func(req *http.Request) (*http.Response, error) {
			called = true

			expectedPath := "/v1/chains/43113/addresses/" +
				"0x4DFb0d91C07A7C706F65F974953cE8b111D27Ee5" +
				"/transactions"

			if req.URL.Path != expectedPath {
				t.Errorf("unexpected path: %s", req.URL.Path)
			}

			query := req.URL.Query()

			if query.Get("pageSize") != "1" {
				t.Error("pageSize should be 1")
			}
			if query.Get("sortOrder") != "desc" {
				t.Error("sortOrder should be desc")
			}

			if _, exists := query["pageToken"]; exists {
				t.Error("first request should not include pageToken")
			}

			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(string(fixture))),
				Header:     make(http.Header),
			}, nil
		},
	)

	page, err := client.ListTransactions(
		context.Background(),
		"0x4DFb0d91C07A7C706F65F974953cE8b111D27Ee5",
		1,
		"",
	)
	if err != nil {
		t.Fatal(err)
	}

	if !called {
		t.Fatal("expected an HTTP request")
	}
	if len(page.Transactions) != 1 {
		t.Fatalf("got %d transactions, want 1", len(page.Transactions))
	}

	tx := page.Transactions[0].NativeTransaction

	expectedHash := "0xc522af1f87362c123dec3df453e154970d429357e008a46a2734e2c26ede81d3"

	if tx.TxHash != expectedHash {
		t.Errorf("transaction hash = %s, want %s", tx.TxHash, expectedHash)
	}
	if tx.BlockNumber != "60107076" {
		t.Errorf("block number = %s, want 60107076", tx.BlockNumber)
	}
	if tx.TxStatus != "1" {
		t.Errorf("transaction status = %s, want 1", tx.TxStatus)
	}
	if page.NextPageToken == "" {
		t.Error("expected a next page token in the saved fixture")
	}
}

func TestDataAPITransactionsPassesPageToken(t *testing.T) {
	client := NewDataAPIClient("")
	pageToken := "cursor+with/special=characters&next"
	called := false

	client.httpClient.Transport = dataAPITestTransport(
		func(req *http.Request) (*http.Response, error) {
			called = true

			if got := req.URL.Query().Get("pageToken"); got != pageToken {
				t.Errorf("page token = %q, want %q", got, pageToken)
			}

			if req.URL.Query().Get("pageSize") != "1" {
				t.Error("pageSize should remain 1")
			}

			return &http.Response{
				StatusCode: http.StatusOK,
				Body: io.NopCloser(strings.NewReader(
					`{"transactions":[]}`,
				)),
				Header: make(http.Header),
			}, nil
		},
	)

	page, err := client.ListTransactions(
		context.Background(),
		"0x4DFb0d91C07A7C706F65F974953cE8b111D27Ee5",
		1,
		pageToken,
	)
	if err != nil {
		t.Fatal(err)
	}

	if !called {
		t.Fatal("expected an HTTP request")
	}
	if len(page.Transactions) != 0 {
		t.Error("expected an empty transaction list")
	}
	if page.NextPageToken != "" {
		t.Error("expected no next page token")
	}
}
