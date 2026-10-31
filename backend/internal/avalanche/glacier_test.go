package avalanche

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
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

func TestDataAPIAuthenticationHeader(t *testing.T) {
	const wallet = "0x4DFb0d91C07A7C706F65F974953cE8b111D27Ee5"
	const fakeKey = "test-key-not-a-real-secret"

	fixture, err := os.ReadFile("testdata/fuji_native_balance.json")
	if err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if got := r.Header.Get("x-glacier-api-key"); got != fakeKey {
				t.Error("API key header is missing or incorrect")
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}

			if r.Header.Get("Accept") != "application/json" {
				t.Error("Accept header should be application/json")
			}

			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(fixture)
		},
	))
	defer server.Close()

	// The constructor should trim surrounding whitespace.
	client := NewDataAPIClient("  " + fakeKey + "  ")
	client.baseURL = server.URL

	balance, err := client.GetNativeBalance(
		context.Background(),
		wallet,
	)
	if err != nil {
		t.Fatal(err)
	}
	if balance == nil {
		t.Fatal("expected a native balance")
	}
}

func TestDataAPIERC20RejectsInvalidInput(t *testing.T) {
	const wallet = "0x4DFb0d91C07A7C706F65F974953cE8b111D27Ee5"

	tests := []struct {
		name     string
		address  string
		pageSize int
	}{
		{"invalid address", "not-a-wallet", 1},
		{"zero page size", wallet, 0},
		{"negative page size", wallet, -1},
		{"page size exceeds limit", wallet, 101},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			called := false
			client := NewDataAPIClient("")
			client.httpClient.Transport = dataAPITestTransport(
				func(req *http.Request) (*http.Response, error) {
					called = true
					return nil, context.Canceled
				},
			)

			page, err := client.ListERC20Balances(
				context.Background(), tt.address, tt.pageSize, "",
			)
			if err == nil {
				t.Fatal("expected invalid input error")
			}
			if page != nil {
				t.Error("expected nil page")
			}
			if called {
				t.Error("invalid input must not trigger an HTTP request")
			}
		})
	}
}

func TestDataAPIHistoryLimits(t *testing.T) {
	const wallet = "0x4DFb0d91C07A7C706F65F974953cE8b111D27Ee5"

	tests := []struct {
		name           string
		cursors        []string
		wantCalls      int
		wantRecords    int
		wantTruncated  bool
		wantErr        bool
		recordsPerPage int
	}{
		{
			name:           "stop when history ends",
			cursors:        []string{""},
			wantCalls:      1,
			wantRecords:    50,
			recordsPerPage: 50,
		},
		{
			name:           "stop at two pages and 100 records",
			cursors:        []string{"page-2", "page-3"},
			wantCalls:      2,
			wantRecords:    100,
			wantTruncated:  true,
			recordsPerPage: 50,
		},
		{
			name:           "reject repeated cursor",
			cursors:        []string{"page-2", "page-2"},
			wantCalls:      2,
			wantErr:        true,
			recordsPerPage: 50,
		},
		{
			name:           "stop at two pages below record limit",
			cursors:        []string{"page-2", "page-3"},
			recordsPerPage: 1,
			wantCalls:      2,
			wantRecords:    2,
			wantTruncated:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			client := NewDataAPIClient("")
			client.httpClient.Transport = dataAPITestTransport(
				func(req *http.Request) (*http.Response, error) {
					calls++
					if calls > len(tt.cursors) {
						t.Error("unexpected extra request")
						return nil, context.Canceled
					}
					if req.URL.Query().Get("pageSize") != "50" {
						t.Error("expected pageSize=50")
					}

					expectedCursor := ""
					if calls > 1 {
						expectedCursor = tt.cursors[calls-2]
					}
					if req.URL.Query().Get("pageToken") != expectedCursor {
						t.Error("unexpected request cursor")
					}

					transactions := make([]DataAPITransaction, tt.recordsPerPage)
					for i := range transactions {
						transactions[i].NativeTransaction =
							DataAPINativeTransaction{
								ChainID: "43113",
								TxHash:  "test-hash",
								Value:   "0",
							}
					}

					body, err := json.Marshal(DataAPITransactionPage{
						Transactions:  transactions,
						NextPageToken: tt.cursors[calls-1],
					})
					if err != nil {
						return nil, err
					}
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       io.NopCloser(strings.NewReader(string(body))),
						Header:     make(http.Header),
					}, nil
				},
			)

			result, err := client.ReadTransactionHistory(
				context.Background(), wallet,
			)
			if calls != tt.wantCalls {
				t.Errorf("requests = %d, want %d", calls, tt.wantCalls)
			}
			if tt.wantErr {
				if err == nil || !strings.Contains(err.Error(), "repeated") {
					t.Fatalf("expected repeated cursor error, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if result == nil {
				t.Fatal("expected a non-nil history result")
			}

			p := result.Provenance
			if p.Source != "Avalanche Data API" ||
				p.SourceType != "data_api" ||
				p.ChainID != uint64(FujiChainID) ||
				p.Mode != "live" ||
				p.IsDemo ||
				p.ObservedAt.IsZero() ||
				p.BlockNumber != nil {
				t.Fatalf("unexpected history provenance: %+v", p)
			}

			if len(result.Transactions) != tt.wantRecords {
				t.Errorf("records = %d, want %d",
					len(result.Transactions), tt.wantRecords)
			}
			if result.Truncated != tt.wantTruncated {
				t.Errorf("truncated = %v, want %v",
					result.Truncated, tt.wantTruncated)
			}
			if result.NextPageToken != tt.cursors[len(tt.cursors)-1] {
				t.Error("unexpected continuation cursor")
			}
		})
	}
}

func TestDataAPINativeBalanceProvenance(t *testing.T) {
	const wallet = "0x4DFb0d91C07A7C706F65F974953cE8b111D27Ee5"

	fixture, err := os.ReadFile("testdata/fuji_native_balance.json")
	if err != nil {
		t.Fatal(err)
	}

	calls := 0
	client := NewDataAPIClient("")
	client.httpClient.Transport = dataAPITestTransport(
		func(req *http.Request) (*http.Response, error) {
			calls++
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(string(fixture))),
				Header:     make(http.Header),
			}, nil
		},
	)

	before := time.Now().UTC()
	result, err := client.ReadNativeBalance(context.Background(), wallet)
	after := time.Now().UTC()
	if err != nil {
		t.Fatal(err)
	}

	if calls != 1 {
		t.Errorf("requests = %d, want 1", calls)
	}
	if result.WalletAddress != wallet {
		t.Error("unexpected wallet address")
	}
	if result.Balance.Asset.ChainID != uint64(FujiChainID) {
		t.Error("unexpected asset chain ID")
	}

	p := result.Provenance
	if p.Source != "Avalanche Data API" ||
		p.SourceType != "data_api" ||
		p.ChainID != uint64(FujiChainID) ||
		p.Mode != "live" ||
		p.IsDemo {
		t.Fatalf("unexpected provenance: %+v", p)
	}
	if p.BlockNumber != nil {
		t.Error("must not invent a balance block number")
	}
	if p.ObservedAt.Before(before) || p.ObservedAt.After(after) {
		t.Error("observation time is outside the request interval")
	}

	encoded, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatal(err)
	}
	if _, exists := fields["block_number"]; exists {
		t.Error("nil block number must be omitted from JSON")
	}
}

func TestDataAPIERC20Provenance(t *testing.T) {
	const wallet = "0x4DFb0d91C07A7C706F65F974953cE8b111D27Ee5"

	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"erc20TokenBalances":[]}`))
		},
	))
	defer server.Close()

	client := NewDataAPIClient("")
	client.baseURL = server.URL

	page, err := client.ListERC20Balances(
		context.Background(), wallet, 1, "",
	)
	if err != nil {
		t.Fatal(err)
	}
	if page == nil {
		t.Fatal("expected a non-nil page")
	}

	p := page.Provenance
	if p.Source != "Avalanche Data API" ||
		p.SourceType != "data_api" ||
		p.Mode != "live" ||
		p.ChainID != uint64(FujiChainID) ||
		p.ObservedAt.IsZero() ||
		p.BlockNumber != nil ||
		p.IsDemo {
		t.Fatalf("unexpected ERC-20 provenance: %+v", p)
	}
}

func TestDataAPIERC20Balances(t *testing.T) {
	const wallet = "0x4DFb0d91C07A7C706F65F974953cE8b111D27Ee5"
	const token = "0xcCCb83Fbe349fAebDE74Ce633D25B282E6FDd21A"
	const cursor = "cursor+with/special=characters&next"

	tests := []struct {
		name    string
		body    string
		wantErr string
		wantLen int
	}{
		{
			name: "balance and next cursor",
			body: `{"erc20TokenBalances":[{
				"address":"` + token + `",
				"chainId":"43113",
				"decimals":18,
				"balance":"123456789012345678901234567890"
			}],"nextPageToken":"next-page"}`,
			wantLen: 1,
		},
		{
			name: "empty page",
			body: `{"erc20TokenBalances":[]}`,
		},
		{
			name:    "missing balances",
			body:    `{}`,
			wantErr: "missing erc20TokenBalances",
		},
		{
			name: "wrong chain",
			body: `{"erc20TokenBalances":[{
				"address":"` + token + `",
				"chainId":"43114","decimals":18,"balance":"1"
			}]}`,
			wantErr: "unexpected chain ID",
		},
		{
			name: "invalid token address",
			body: `{"erc20TokenBalances":[{
				"address":"invalid",
				"chainId":"43113","decimals":18,"balance":"1"
			}]}`,
			wantErr: "invalid token address",
		},
		{
			name: "invalid decimals",
			body: `{"erc20TokenBalances":[{
				"address":"` + token + `",
				"chainId":"43113","decimals":256,"balance":"1"
			}]}`,
			wantErr: "invalid decimals",
		},
		{
			name: "fractional amount",
			body: `{"erc20TokenBalances":[{
				"address":"` + token + `",
				"chainId":"43113","decimals":18,"balance":"1.5"
			}]}`,
			wantErr: "invalid raw amount",
		},
		{
			name: "uint256 overflow",
			body: `{"erc20TokenBalances":[{
				"address":"` + token + `",
				"chainId":"43113","decimals":18,
				"balance":"115792089237316195423570985008687907853269984665640564039457584007913129639936"
			}]}`,
			wantErr: "exceeds uint256",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(
				func(w http.ResponseWriter, r *http.Request) {
					expectedPath := "/v1/chains/43113/addresses/" +
						wallet + "/balances:listErc20"

					if r.Method != http.MethodGet ||
						r.URL.Path != expectedPath {
						t.Error("unexpected request method or path")
					}
					if r.URL.Query().Get("pageSize") != "1" {
						t.Error("expected pageSize=1")
					}
					if r.URL.Query().Get("pageToken") != cursor {
						t.Error("page cursor was not preserved")
					}

					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(tt.body))
				},
			))
			defer server.Close()

			client := NewDataAPIClient("")
			client.baseURL = server.URL

			page, err := client.ListERC20Balances(
				context.Background(), wallet, 1, cursor,
			)
			if tt.wantErr != "" {
				if err == nil ||
					!strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got %v",
						tt.wantErr, err)
				}
				if page != nil {
					t.Error("expected nil page on error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if page == nil {
				t.Fatal("expected a non-nil page")
			}
			if len(page.Balances) != tt.wantLen {
				t.Fatalf("got %d balances, want %d",
					len(page.Balances), tt.wantLen)
			}
			if tt.wantLen == 1 {
				if page.Balances[0].Balance !=
					"123456789012345678901234567890" {
					t.Error("raw amount lost precision")
				}
				if page.NextPageToken != "next-page" {
					t.Error("next page cursor was not preserved")
				}
			}
		})
	}
}

func TestDataAPINativeBalanceRejectsHTTPAndJSONErrors(t *testing.T) {
	const wallet = "0x4DFb0d91C07A7C706F65F974953cE8b111D27Ee5"

	tests := []struct {
		name    string
		status  int
		body    string
		wantErr string
	}{
		{
			name:    "unauthorized",
			status:  http.StatusUnauthorized,
			body:    `{"message":"unauthorized"}`,
			wantErr: "HTTP 401",
		},
		{
			name:    "malformed JSON",
			status:  http.StatusOK,
			body:    `{"nativeTokenBalance":`,
			wantErr: "decode Data API native balance",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(
				func(w http.ResponseWriter, r *http.Request) {
					calls++
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(tt.status)
					_, _ = w.Write([]byte(tt.body))
				},
			))
			defer server.Close()

			client := NewDataAPIClient("")
			client.baseURL = server.URL

			balance, err := client.GetNativeBalance(
				context.Background(), wallet,
			)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error containing %q, got %v",
					tt.wantErr, err)
			}
			if balance != nil {
				t.Error("expected nil balance on error")
			}
			if calls != 1 {
				t.Errorf("requests = %d, want 1; must not retry", calls)
			}
		})
	}
}

func TestDataAPIReadRetry(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		statuses   []int
		retryAfter string
		wantCalls  int
		wantStatus int
		wantErr    bool
	}{
		{
			name:       "retry 429 then succeed",
			method:     http.MethodGet,
			statuses:   []int{429, 200},
			retryAfter: "0",
			wantCalls:  2,
			wantStatus: 200,
		},
		{
			name:       "stop after second 429",
			method:     http.MethodGet,
			statuses:   []int{429, 429},
			retryAfter: "0",
			wantCalls:  2,
			wantStatus: 429,
		},
		{
			name:       "do not retry 401",
			method:     http.MethodGet,
			statuses:   []int{401},
			wantCalls:  1,
			wantStatus: 401,
		},
		{
			name:       "do not retry long Retry-After",
			method:     http.MethodGet,
			statuses:   []int{429},
			retryAfter: "60",
			wantCalls:  1,
			wantStatus: 429,
		},
		{
			name:    "reject POST without sending",
			method:  http.MethodPost,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(
				func(w http.ResponseWriter, r *http.Request) {
					calls++
					if calls > len(tt.statuses) {
						t.Error("unexpected extra request")
						w.WriteHeader(http.StatusInternalServerError)
						return
					}
					if tt.retryAfter != "" {
						w.Header().Set("Retry-After", tt.retryAfter)
					}
					w.WriteHeader(tt.statuses[calls-1])
				},
			))
			defer server.Close()

			client := NewDataAPIClient("")
			req, err := http.NewRequestWithContext(
				context.Background(), tt.method, server.URL, nil,
			)
			if err != nil {
				t.Fatal(err)
			}

			resp, err := client.doRead(req)
			if resp != nil {
				defer resp.Body.Close()
			}

			if calls != tt.wantCalls {
				t.Errorf("requests = %d, want %d", calls, tt.wantCalls)
			}
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if resp == nil {
				t.Fatal("expected HTTP response")
			}
			if resp.StatusCode != tt.wantStatus {
				t.Errorf("HTTP status = %d, want %d",
					resp.StatusCode, tt.wantStatus)
			}
		})
	}
}

func TestDataAPIReadRetryStopsOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	calls := 0
	client := NewDataAPIClient("")
	client.httpClient.Transport = dataAPITestTransport(
		func(req *http.Request) (*http.Response, error) {
			calls++
			cancel()

			return &http.Response{
				StatusCode: http.StatusTooManyRequests,
				Body:       io.NopCloser(strings.NewReader("{}")),
				Header:     make(http.Header),
			}, nil
		},
	)

	req, err := http.NewRequestWithContext(
		ctx, http.MethodGet, "https://example.invalid", nil,
	)
	if err != nil {
		t.Fatal(err)
	}

	resp, err := client.doRead(req)
	if resp != nil {
		resp.Body.Close()
		t.Error("expected nil response on cancellation")
	}
	if err != context.Canceled {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if calls != 1 {
		t.Errorf("requests = %d, want 1", calls)
	}
}

func TestDataAPIReadBudget(t *testing.T) {
	tests := []struct {
		name      string
		limit     uint64
		status    int
		wantCalls int
		wantErr   bool
	}{
		{
			name:      "one successful request",
			limit:     1,
			status:    http.StatusOK,
			wantCalls: 1,
		},
		{
			name:    "zero budget blocks request",
			limit:   0,
			status:  http.StatusOK,
			wantErr: true,
		},
		{
			name:      "retry blocked when budget exhausted",
			limit:     1,
			status:    http.StatusTooManyRequests,
			wantCalls: 1,
			wantErr:   true,
		},
		{
			name:      "retry consumes second attempt",
			limit:     2,
			status:    http.StatusTooManyRequests,
			wantCalls: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			client := NewDataAPIClient("")
			client.readLimit = tt.limit
			client.httpClient.Transport = dataAPITestTransport(
				func(req *http.Request) (*http.Response, error) {
					calls++
					header := make(http.Header)
					header.Set("Retry-After", "0")
					return &http.Response{
						StatusCode: tt.status,
						Body:       io.NopCloser(strings.NewReader("{}")),
						Header:     header,
					}, nil
				},
			)

			req, err := http.NewRequestWithContext(
				context.Background(),
				http.MethodGet,
				"https://example.invalid",
				nil,
			)
			if err != nil {
				t.Fatal(err)
			}

			resp, err := client.doRead(req)
			if resp != nil {
				resp.Body.Close()
			}

			if tt.wantErr {
				if err == nil ||
					!strings.Contains(err.Error(), "budget exhausted") {
					t.Fatalf("expected budget error, got %v", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}

			if calls != tt.wantCalls {
				t.Errorf("requests = %d, want %d", calls, tt.wantCalls)
			}

			attempts, limit := client.ReadUsage()
			if attempts != uint64(tt.wantCalls) || limit != tt.limit {
				t.Errorf("usage = %d/%d, want %d/%d",
					attempts, limit, tt.wantCalls, tt.limit)
			}

			// All cases have now consumed their available budget.
			resp, err = client.doRead(req)
			if resp != nil {
				resp.Body.Close()
			}
			if err == nil ||
				!strings.Contains(err.Error(), "budget exhausted") {
				t.Fatalf("expected budget error on next call, got %v", err)
			}
			if calls != tt.wantCalls {
				t.Error("exhausted budget must not send another request")
			}
		})
	}
}
