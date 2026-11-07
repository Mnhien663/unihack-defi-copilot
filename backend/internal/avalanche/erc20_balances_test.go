package avalanche

import (
	"context"
	"net/http"
	"net/http/httptest"
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
