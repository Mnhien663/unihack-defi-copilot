package avalanche

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestDataAPIClientUsesInjectedServer(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(
		http.HandlerFunc(
			func(
				writer http.ResponseWriter,
				request *http.Request,
			) {
				if request.URL.Path !=
					"/v1/chains/43113/addresses/"+
						"0x4DFb0d91C07A7C706F65F974953cE8b111D27Ee5"+
						"/balances:getNative" {
					t.Errorf(
						"unexpected path: %s",
						request.URL.Path,
					)
				}

				if request.Header.Get(
					"x-glacier-api-key",
				) != "test-key" {
					t.Error("expected Data API key header")
				}

				writer.Header().Set(
					"Content-Type",
					"application/json",
				)

				_, _ = writer.Write([]byte(`{
					"nativeTokenBalance": {
						"chainId": "43113",
						"name": "Avalanche",
						"symbol": "AVAX",
						"decimals": 18,
						"balance": "2181886544100000121"
					}
				}`))
			},
		),
	)
	defer server.Close()

	client, err := NewDataAPIClientWithConfig(
		DataAPIClientConfig{
			HTTPClient: server.Client(),
			BaseURL:    server.URL,
			APIKey:     "test-key",
			MaxRetries: 0,
			RetryDelay: time.Millisecond,
		},
	)
	if err != nil {
		t.Fatalf("create Data API client: %v", err)
	}

	result, err := client.ReadNativeBalance(
		context.Background(),
		"0x4DFb0d91C07A7C706F65F974953cE8b111D27Ee5",
	)
	if err != nil {
		t.Fatalf("read native balance: %v", err)
	}

	if result.Balance.Raw != "2181886544100000121" {
		t.Fatalf(
			"raw balance = %q",
			result.Balance.Raw,
		)
	}

	if result.Balance.Formatted !=
		"2.181886544100000121" {
		t.Fatalf(
			"formatted balance = %q",
			result.Balance.Formatted,
		)
	}

	if result.Provenance.Source !=
		"Avalanche Data API" {
		t.Fatalf(
			"source = %q",
			result.Provenance.Source,
		)
	}
}

func TestDataAPIClientRejectsInvalidConfig(t *testing.T) {
	t.Parallel()

	_, err := NewDataAPIClientWithConfig(
		DataAPIClientConfig{
			BaseURL: "not-a-url",
		},
	)
	if err == nil {
		t.Fatal("expected invalid base URL error")
	}

	_, err = NewDataAPIClientWithConfig(
		DataAPIClientConfig{
			MaxRetries: 6,
		},
	)
	if err == nil {
		t.Fatal("expected invalid retry count error")
	}
}

func TestDataAPIClientRetriesTemporaryErrors(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32

	server := httptest.NewServer(
		http.HandlerFunc(
			func(
				writer http.ResponseWriter,
				request *http.Request,
			) {
				currentAttempt := attempts.Add(1)

				if currentAttempt <= 2 {
					http.Error(
						writer,
						"temporarily unavailable",
						http.StatusServiceUnavailable,
					)
					return
				}

				writer.Header().Set(
					"Content-Type",
					"application/json",
				)

				_, _ = writer.Write([]byte(`{
					"nativeTokenBalance": {
						"chainId": "43113",
						"name": "Avalanche",
						"symbol": "AVAX",
						"decimals": 18,
						"balance": "1000000000000000000"
					}
				}`))
			},
		),
	)
	defer server.Close()

	client, err := NewDataAPIClientWithConfig(
		DataAPIClientConfig{
			HTTPClient: server.Client(),
			BaseURL:    server.URL,
			MaxRetries: 2,
			RetryDelay: time.Millisecond,
		},
	)
	if err != nil {
		t.Fatalf("create Data API client: %v", err)
	}

	balance, err := client.GetNativeBalance(
		context.Background(),
		"0x4DFb0d91C07A7C706F65F974953cE8b111D27Ee5",
	)
	if err != nil {
		t.Fatalf("get native balance: %v", err)
	}

	if balance.Balance != "1000000000000000000" {
		t.Fatalf(
			"balance = %q",
			balance.Balance,
		)
	}

	if attempts.Load() != 3 {
		t.Fatalf(
			"attempt count = %d, want 3",
			attempts.Load(),
		)
	}
}

func TestDataAPIClientDoesNotRetryPermanentError(
	t *testing.T,
) {
	t.Parallel()

	var attempts atomic.Int32

	server := httptest.NewServer(
		http.HandlerFunc(
			func(
				writer http.ResponseWriter,
				request *http.Request,
			) {
				attempts.Add(1)

				http.Error(
					writer,
					"bad request",
					http.StatusBadRequest,
				)
			},
		),
	)
	defer server.Close()

	client, err := NewDataAPIClientWithConfig(
		DataAPIClientConfig{
			HTTPClient: server.Client(),
			BaseURL:    server.URL,
			MaxRetries: 2,
			RetryDelay: time.Millisecond,
		},
	)
	if err != nil {
		t.Fatalf("create Data API client: %v", err)
	}

	_, err = client.GetNativeBalance(
		context.Background(),
		"0x4DFb0d91C07A7C706F65F974953cE8b111D27Ee5",
	)
	if err == nil {
		t.Fatal("expected HTTP 400 error")
	}

	if attempts.Load() != 1 {
		t.Fatalf(
			"attempt count = %d, want 1",
			attempts.Load(),
		)
	}
}
