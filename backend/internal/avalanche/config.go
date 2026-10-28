package avalanche

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// RPCConfig contains the settings required to initialize an RPC client.
type RPCConfig struct {
	URL             string
	ExpectedChainID int64
	Timeout         time.Duration
}

// LoadRPCConfig reads RPC settings from environment variables.
func LoadRPCConfig() (RPCConfig, error) {
	cfg := RPCConfig{
		URL:             strings.TrimSpace(os.Getenv("AVALANCHE_RPC_URL")),
		ExpectedChainID: FujiChainID,
		Timeout:         rpcTimeout,
	}

	if value := strings.TrimSpace(
		os.Getenv("AVALANCHE_CHAIN_ID"),
	); value != "" {
		chainID, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return RPCConfig{}, fmt.Errorf(
				"parse AVALANCHE_CHAIN_ID: %w", err,
			)
		}
		cfg.ExpectedChainID = chainID
	}

	if value := strings.TrimSpace(
		os.Getenv("AVALANCHE_RPC_TIMEOUT"),
	); value != "" {
		timeout, err := time.ParseDuration(value)
		if err != nil {
			return RPCConfig{}, fmt.Errorf(
				"parse AVALANCHE_RPC_TIMEOUT: %w", err,
			)
		}
		cfg.Timeout = timeout
	}

	if err := cfg.Validate(); err != nil {
		return RPCConfig{}, err
	}

	return cfg, nil
}

// Validate rejects missing or invalid RPC settings.
func (cfg RPCConfig) Validate() error {
	if strings.TrimSpace(cfg.URL) == "" {
		return fmt.Errorf("AVALANCHE_RPC_URL is not configured")
	}

	parsedURL, err := url.Parse(cfg.URL)
	if err != nil {
		return fmt.Errorf("invalid AVALANCHE_RPC_URL: %w", err)
	}

	switch parsedURL.Scheme {
	case "http", "https", "ws", "wss":
	default:
		return fmt.Errorf(
			"AVALANCHE_RPC_URL must use http, https, ws or wss",
		)
	}

	if parsedURL.Hostname() == "" {
		return fmt.Errorf("AVALANCHE_RPC_URL must contain a hostname")
	}

	if cfg.ExpectedChainID <= 0 {
		return fmt.Errorf("AVALANCHE_CHAIN_ID must be greater than zero")
	}

	if cfg.Timeout <= 0 {
		return fmt.Errorf("AVALANCHE_RPC_TIMEOUT must be greater than zero")
	}

	return nil
}
