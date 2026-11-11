package avalanche

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestListTokenBalancesFromServer(t *testing.T) {
	t.Parallel()

	const pageToken = "next/cursor+123="

	server := httptest.NewServer(
		http.HandlerFunc(
			func(
				writer http.ResponseWriter,
				request *http.Request,
			) {
				expectedPath :=
					"/v1/chains/43113/addresses/" +
						"0x4DFb0d91C07A7C706F65F974953cE8b111D27Ee5" +
						"/balances:listErc20"

				if request.URL.Path != expectedPath {
					t.Errorf(
						"path = %q, want %q",
						request.URL.Path,
						expectedPath,
					)
				}

				query := request.URL.Query()

				if query.Get("pageSize") != "2" {
					t.Errorf(
						"pageSize = %q",
						query.Get("pageSize"),
					)
				}

				if query.Get("pageToken") != pageToken {
					t.Errorf(
						"pageToken = %q",
						query.Get("pageToken"),
					)
				}

				if query.Get("filterSpamTokens") != "true" {
					t.Errorf(
						"filterSpamTokens = %q",
						query.Get("filterSpamTokens"),
					)
				}

				writer.Header().Set(
					"Content-Type",
					"application/json",
				)

				_, _ = writer.Write([]byte(`{
					"nextPageToken": "page-2",
					"erc20TokenBalances": [
						{
							"address": "0xB97EF9Ef8734C71904D8002F8b6Bc66Dd9c48a6E",
							"name": "USD Coin",
							"symbol": "USDC",
							"decimals": 6,
							"ercType": "ERC-20",
							"chainId": "43113",
							"balance": "1234567",
							"tokenReputation": "Verified"
						},
						{
							"address": "0x0000000000000000000000000000000000000002",
							"name": "Demo Token",
							"symbol": "DDEMO",
							"decimals": 18,
							"ercType": "ERC-20",
							"chainId": "43113",
							"balance": "2500000000000000000",
							"tokenReputation": "Unknown"
						}
					]
				}`))
			},
		),
	)
	defer server.Close()

	client, err := NewDataAPIClientWithConfig(
		DataAPIClientConfig{
			HTTPClient: server.Client(),
			BaseURL:    server.URL,
			MaxRetries: 0,
			RetryDelay: time.Millisecond,
		},
	)
	if err != nil {
		t.Fatalf("create Data API client: %v", err)
	}

	page, err := client.ListTokenBalances(
		context.Background(),
		"0x4DFb0d91C07A7C706F65F974953cE8b111D27Ee5",
		2,
		pageToken,
	)
	if err != nil {
		t.Fatalf("list token balances: %v", err)
	}

	if len(page.Balances) != 2 {
		t.Fatalf(
			"balance count = %d, want 2",
			len(page.Balances),
		)
	}

	if page.Balances[0].Amount.Formatted != "1.234567" {
		t.Fatalf(
			"USDC formatted = %q",
			page.Balances[0].Amount.Formatted,
		)
	}

	if page.Balances[1].Amount.Formatted != "2.5" {
		t.Fatalf(
			"DDEMO formatted = %q",
			page.Balances[1].Amount.Formatted,
		)
	}

	if page.NextPageToken != "page-2" {
		t.Fatalf(
			"next page token = %q",
			page.NextPageToken,
		)
	}

	if page.Provenance.Source != "Avalanche Data API" {
		t.Fatalf(
			"source = %q",
			page.Provenance.Source,
		)
	}

	if page.Provenance.ObservedAt.IsZero() {
		t.Fatal("observed_at must not be zero")
	}
}

func TestListTokenBalancesRejectsInvalidInput(t *testing.T) {
	t.Parallel()

	client := NewDataAPIClient("")

	_, err := client.ListTokenBalances(
		context.Background(),
		"not-a-wallet",
		10,
		"",
	)
	if err == nil {
		t.Fatal("expected invalid wallet error")
	}

	_, err = client.ListTokenBalances(
		context.Background(),
		"0x0000000000000000000000000000000000000001",
		501,
		"",
	)
	if err == nil {
		t.Fatal("expected invalid page size error")
	}
}

func TestListTokenBalancesResponseBoundaries(t *testing.T) {
	t.Parallel()

	maxUint256 := new(big.Int).Sub(
		new(big.Int).Lsh(big.NewInt(1), 256),
		big.NewInt(1),
	).String()

	overflowUint256 := new(big.Int).Lsh(
		big.NewInt(1),
		256,
	).String()

	tests := []struct {
		name            string
		change          func(map[string]any)
		wantRaw         string
		wantFormatted   string
		wantError       bool
		wantMetadataErr bool
	}{
		{
			name:          "zero balance",
			change:        func(item map[string]any) { item["balance"] = "0" },
			wantRaw:       "0",
			wantFormatted: "0",
		},
		{
			name: "unknown reputation remains a valid result",
			change: func(item map[string]any) {
				item["tokenReputation"] = "Unknown"
			},
			wantRaw:       "1234567",
			wantFormatted: "1.234567",
		},
		{
			name: "missing decimals",
			change: func(item map[string]any) {
				delete(item, "decimals")
			},
			wantError:       true,
			wantMetadataErr: true,
		},
		{
			name: "null decimals",
			change: func(item map[string]any) {
				item["decimals"] = nil
			},
			wantError:       true,
			wantMetadataErr: true,
		},
		{
			name: "zero decimals is valid",
			change: func(item map[string]any) {
				item["decimals"] = 0
			},
			wantRaw:       "1234567",
			wantFormatted: "1234567",
		},
		{
			name: "negative decimals",
			change: func(item map[string]any) {
				item["decimals"] = -1
			},
			wantError:       true,
			wantMetadataErr: true,
		},
		{
			name: "decimals above uint8",
			change: func(item map[string]any) {
				item["decimals"] = 256
			},
			wantError:       true,
			wantMetadataErr: true,
		},
		{
			name: "wrong chain",
			change: func(item map[string]any) {
				item["chainId"] = "43114"
			},
			wantError: true,
		},
		{
			name: "invalid token address",
			change: func(item map[string]any) {
				item["address"] = "not-a-token"
			},
			wantError: true,
		},
		{
			name: "maximum uint256 stays exact",
			change: func(item map[string]any) {
				item["balance"] = maxUint256
				item["decimals"] = 0
			},
			wantRaw:       maxUint256,
			wantFormatted: maxUint256,
		},
		{
			name: "uint256 overflow",
			change: func(item map[string]any) {
				item["balance"] = overflowUint256
			},
			wantError: true,
		},
		{
			name: "negative raw amount",
			change: func(item map[string]any) {
				item["balance"] = "-1"
			},
			wantError: true,
		},
		{
			name: "plus sign in raw amount",
			change: func(item map[string]any) {
				item["balance"] = "+1"
			},
			wantError: true,
		},
		{
			name: "fractional raw amount",
			change: func(item map[string]any) {
				item["balance"] = "1.5"
			},
			wantError: true,
		},
		{
			name: "empty raw amount",
			change: func(item map[string]any) {
				item["balance"] = ""
			},
			wantError: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			item := map[string]any{
				"address":         "0x0000000000000000000000000000000000000002",
				"name":            "Fixture Token",
				"symbol":          "FIX",
				"decimals":        6,
				"ercType":         "ERC-20",
				"chainId":         "43113",
				"balance":         "1234567",
				"tokenReputation": "Verified",
			}

			test.change(item)

			body, err := json.Marshal(map[string]any{
				"erc20TokenBalances": []any{item},
			})
			if err != nil {
				t.Fatal(err)
			}

			server := httptest.NewServer(http.HandlerFunc(
				func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write(body)
				},
			))
			defer server.Close()

			client, err := NewDataAPIClientWithConfig(
				DataAPIClientConfig{
					HTTPClient: server.Client(),
					BaseURL:    server.URL,
					MaxRetries: 0,
				},
			)
			if err != nil {
				t.Fatal(err)
			}

			page, err := client.ListTokenBalances(
				context.Background(),
				"0x0000000000000000000000000000000000000001",
				1,
				"",
			)

			if test.wantError {
				if err == nil {
					t.Fatal("expected an error")
				}

				if test.wantMetadataErr &&
					!errors.Is(err, ErrTokenMetadataUnavailable) {
					t.Fatalf("expected metadata error, got %v", err)
				}

				if len(page.Balances) != 0 {
					t.Fatal("error must not return usable token balances")
				}

				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(page.Balances) != 1 {
				t.Fatalf("got %d balances, want 1", len(page.Balances))
			}

			got := page.Balances[0]

			if got.Amount.Raw != test.wantRaw {
				t.Errorf("raw = %q, want %q", got.Amount.Raw, test.wantRaw)
			}

			if got.Amount.Formatted != test.wantFormatted {
				t.Errorf(
					"formatted = %q, want %q",
					got.Amount.Formatted,
					test.wantFormatted,
				)
			}

			if test.name == "unknown reputation remains a valid result" &&
				got.TokenReputation != "Unknown" {
				t.Errorf("reputation = %q, want Unknown", got.TokenReputation)
			}

			if page.Provenance.SourceType != "data_api" ||
				page.Provenance.Mode != "live" ||
				page.Provenance.IsDemo ||
				page.Provenance.ChainID != uint64(FujiChainID) {
				t.Fatalf("incorrect provenance: %+v", page.Provenance)
			}

			if page.Provenance.BlockNumber != nil {
				t.Fatal("must not invent a block number")
			}

			if page.Provenance.ObservedAt.IsZero() {
				t.Fatal("observation time must be populated")
			}
		})
	}
}

func TestListTokenBalancesRejectsInvalidPages(t *testing.T) {
	t.Parallel()

	const validItem = `{
		"address": "0x0000000000000000000000000000000000000002",
		"name": "Fixture Token",
		"symbol": "FIX",
		"decimals": 6,
		"chainId": "43113",
		"balance": "1234567"
	}`

	tests := []struct {
		name       string
		statusCode int
		body       string
		wantError  string
	}{
		{
			name:       "response exceeds requested page size",
			statusCode: http.StatusOK,
			body: `{"erc20TokenBalances":[` +
				validItem + `,` + validItem + `]}`,
			wantError: "exceeds requested page size",
		},
		{
			name:       "missing balance list",
			statusCode: http.StatusOK,
			body:       `{}`,
			wantError:  "missing erc20TokenBalances",
		},
		{
			name:       "null balance list",
			statusCode: http.StatusOK,
			body:       `{"erc20TokenBalances":null}`,
			wantError:  "missing erc20TokenBalances",
		},
		{
			name:       "malformed JSON",
			statusCode: http.StatusOK,
			body:       `{"erc20TokenBalances":[`,
			wantError:  "decode Data API ERC-20 balances",
		},
		{
			name:       "HTTP 500 is not an empty wallet",
			statusCode: http.StatusInternalServerError,
			body:       `{"statusCode":500,"message":"Internal server error"}`,
			wantError:  "HTTP 500",
		},
		{
			name:       "HTTP 429 is not an empty wallet",
			statusCode: http.StatusTooManyRequests,
			body:       `{"message":"rate limited"}`,
			wantError:  "HTTP 429",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(
				func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(test.statusCode)
					_, _ = w.Write([]byte(test.body))
				},
			))
			defer server.Close()

			client, err := NewDataAPIClientWithConfig(
				DataAPIClientConfig{
					HTTPClient: server.Client(),
					BaseURL:    server.URL,
					MaxRetries: 0,
				},
			)
			if err != nil {
				t.Fatal(err)
			}

			page, err := client.ListTokenBalances(
				context.Background(),
				"0x0000000000000000000000000000000000000001",
				1,
				"",
			)

			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("error = %v, want %q", err, test.wantError)
			}

			if len(page.Balances) != 0 {
				t.Fatal("failed request must not return usable balances")
			}

			attempts, _ := client.ReadUsage()
			if attempts != 1 {
				t.Fatalf("attempts = %d, want 1", attempts)
			}
		})
	}
}

func TestListTokenBalancesAllowsEmptyPage(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"erc20TokenBalances":[]}`))
		},
	))
	defer server.Close()

	client, err := NewDataAPIClientWithConfig(
		DataAPIClientConfig{
			HTTPClient: server.Client(),
			BaseURL:    server.URL,
			MaxRetries: 0,
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	page, err := client.ListTokenBalances(
		context.Background(),
		"0x0000000000000000000000000000000000000001",
		1,
		"",
	)
	if err != nil {
		t.Fatalf("empty page must be valid: %v", err)
	}

	if page.Balances == nil || len(page.Balances) != 0 {
		t.Fatalf("expected non-nil empty balances, got %#v", page.Balances)
	}

	if page.NextPageToken != "" {
		t.Fatalf("unexpected cursor: %q", page.NextPageToken)
	}
}

func TestListTokenBalancesPropagatesTimeout(t *testing.T) {
	t.Parallel()

	started := make(chan struct{})

	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			close(started)
			<-r.Context().Done()
		},
	))
	defer server.Close()

	client, err := NewDataAPIClientWithConfig(
		DataAPIClientConfig{
			HTTPClient: server.Client(),
			BaseURL:    server.URL,
			MaxRetries: 0,
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	// Cancellation happens after the request reaches the local server.
	go func() {
		select {
		case <-started:
			cancel()
		case <-ctx.Done():
		}
	}()

	page, err := client.ListTokenBalances(
		ctx,
		"0x0000000000000000000000000000000000000001",
		1,
		"",
	)

	if !errors.Is(err, context.Canceled) &&
		!errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected cancellation or deadline error, got %v", err)
	}

	if len(page.Balances) != 0 {
		t.Fatal("timed-out request must not return usable balances")
	}
}
