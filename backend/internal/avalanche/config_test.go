package avalanche

import (
	"testing"
	"time"
)

func TestLoadRPCConfig(t *testing.T) {
	tests := []struct {
		name        string
		url         string
		chainID     string
		timeout     string
		wantChainID int64
		wantTimeout time.Duration
		wantErr     bool
	}{
		{
			name:        "default values",
			url:         "https://example.com",
			wantChainID: FujiChainID,
			wantTimeout: 5 * time.Second,
		},
		{
			name:        "custom values",
			url:         "http://localhost:9650/ext/bc/C/rpc",
			chainID:     "43114",
			timeout:     "10s",
			wantChainID: 43114,
			wantTimeout: 10 * time.Second,
		},
		{
			name:    "missing URL",
			wantErr: true,
		},
		{
			name:    "invalid URL scheme",
			url:     "ftp://example.com",
			wantErr: true,
		},
		{
			name:    "invalid chain ID",
			url:     "https://example.com",
			chainID: "abc",
			wantErr: true,
		},
		{
			name:    "zero chain ID",
			url:     "https://example.com",
			chainID: "0",
			wantErr: true,
		},
		{
			name:    "invalid timeout",
			url:     "https://example.com",
			timeout: "abc",
			wantErr: true,
		},
		{
			name:    "zero timeout",
			url:     "https://example.com",
			timeout: "0s",
			wantErr: true,
		},
		{
			name:    "negative timeout",
			url:     "https://example.com",
			timeout: "-1s",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("AVALANCHE_RPC_URL", tt.url)
			t.Setenv("AVALANCHE_CHAIN_ID", tt.chainID)
			t.Setenv("AVALANCHE_RPC_TIMEOUT", tt.timeout)

			cfg, err := LoadRPCConfig()

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected an error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if cfg.URL != tt.url {
				t.Errorf("URL = %q, want %q", cfg.URL, tt.url)
			}

			if cfg.ExpectedChainID != tt.wantChainID {
				t.Errorf(
					"chain ID = %d, want %d",
					cfg.ExpectedChainID,
					tt.wantChainID,
				)
			}

			if cfg.Timeout != tt.wantTimeout {
				t.Errorf(
					"timeout = %s, want %s",
					cfg.Timeout,
					tt.wantTimeout,
				)
			}
		})
	}
}
