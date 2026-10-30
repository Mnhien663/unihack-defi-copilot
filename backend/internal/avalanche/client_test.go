package avalanche

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestInitClient(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			var request struct {
				ID     json.RawMessage `json:"id"`
				Method string          `json:"method"`
			}

			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Error("could not decode RPC request")
				http.Error(w, "invalid request", http.StatusBadRequest)
				return
			}

			if request.Method != "eth_chainId" {
				t.Errorf("unexpected RPC method: %s", request.Method)
				http.Error(w, "unexpected method", http.StatusBadRequest)
				return
			}

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      request.ID,
				"result":  "0xa869",
			})
		},
	))
	defer server.Close()

	t.Setenv("AVALANCHE_RPC_URL", server.URL)

	client, err := InitClient()
	if err != nil {
		t.Fatalf("InitClient() returned error: %v", err)
	}
	if client == nil {
		t.Fatal("InitClient() returned a nil client")
	}
	defer client.Close()
}

func TestNewRPCClientChainID(t *testing.T) {
	tests := []struct {
		name      string
		rpcChain  string
		wantWrong bool
	}{
		{
			name:     "correct Fuji chain",
			rpcChain: "0xa869",
		},
		{
			name:      "wrong chain",
			rpcChain:  "0xa86a",
			wantWrong: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(
				func(w http.ResponseWriter, r *http.Request) {
					var request struct {
						ID     json.RawMessage `json:"id"`
						Method string          `json:"method"`
					}

					if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
						http.Error(w, "invalid request", http.StatusBadRequest)
						return
					}

					if request.Method != "eth_chainId" {
						http.Error(w, "unexpected method", http.StatusBadRequest)
						return
					}

					w.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(w).Encode(map[string]any{
						"jsonrpc": "2.0",
						"id":      request.ID,
						"result":  tt.rpcChain,
					})
				},
			))
			defer server.Close()

			client, err := NewRPCClient(
				context.Background(),
				RPCConfig{
					URL:             server.URL,
					ExpectedChainID: FujiChainID,
					Timeout:         time.Second,
				},
			)

			if client != nil {
				defer client.Close()
			}

			if tt.wantWrong {
				if !errors.Is(err, ErrWrongChain) {
					t.Fatalf("expected ErrWrongChain, got %v", err)
				}

				if client != nil {
					t.Fatal("expected nil client for wrong chain")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if client == nil {
				t.Fatal("expected a non-nil client")
			}
		})
	}
}

func TestNewRPCClientTimeout(t *testing.T) {
	release := make(chan struct{})

	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			// Keep the RPC request waiting until canceled or released.
			select {
			case <-r.Context().Done():
			case <-release:
			}
		},
	))
	defer server.Close()
	defer close(release)

	client, err := NewRPCClient(
		context.Background(),
		RPCConfig{
			URL:             server.URL,
			ExpectedChainID: FujiChainID,
			Timeout:         100 * time.Millisecond,
		},
	)

	if client != nil {
		defer client.Close()
		t.Fatal("expected nil client after timeout")
	}

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded, got %v", err)
	}
}
