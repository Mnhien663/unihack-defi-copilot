package avalanche

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

const dataAPIBaseURL = "https://data-api.avax.network"

// DataAPIClient calls Avalanche's indexed Data API.
type DataAPIClient struct {
	httpClient *http.Client
	apiKey     string
}

// DataAPINativeBalance keeps the balance in raw units.
type DataAPINativeBalance struct {
	ChainID  string `json:"chainId"`
	Name     string `json:"name"`
	Symbol   string `json:"symbol"`
	Decimals int    `json:"decimals"`
	Balance  string `json:"balance"`
}

type nativeBalanceResponse struct {
	NativeTokenBalance *DataAPINativeBalance `json:"nativeTokenBalance"`
}

// NewDataAPIClient accepts an optional Data API key.
func NewDataAPIClient(apiKey string) *DataAPIClient {
	return &DataAPIClient{
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		apiKey: strings.TrimSpace(apiKey),
	}
}

// GetNativeBalance reads the native balance on Fuji through the Data API.
func (c *DataAPIClient) GetNativeBalance(
	ctx context.Context,
	walletAddress string,
) (*DataAPINativeBalance, error) {
	if c == nil || c.httpClient == nil {
		return nil, fmt.Errorf("Data API client is not initialized")
	}

	walletAddress = strings.TrimSpace(walletAddress)
	if !common.IsHexAddress(walletAddress) {
		return nil, fmt.Errorf("invalid wallet address")
	}

	address := common.HexToAddress(walletAddress).Hex()

	url := fmt.Sprintf(
		"%s/v1/chains/%d/addresses/%s/balances:getNative",
		dataAPIBaseURL,
		FujiChainID,
		address,
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create Data API request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	if c.apiKey != "" {
		req.Header.Set("x-glacier-api-key", c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request Data API native balance: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf(
			"Data API native balance returned HTTP %d",
			resp.StatusCode,
		)
	}

	const maxResponseBytes = 1 << 20

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read Data API response: %w", err)
	}
	if len(body) > maxResponseBytes {
		return nil, fmt.Errorf("Data API response exceeds size limit")
	}

	var result nativeBalanceResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("decode Data API native balance: %w", err)
	}

	balance := result.NativeTokenBalance
	if balance == nil {
		return nil, fmt.Errorf("Data API response is missing nativeTokenBalance")
	}

	if balance.ChainID != fmt.Sprint(FujiChainID) {
		return nil, fmt.Errorf("Data API returned unexpected chain ID")
	}

	if balance.Symbol != "AVAX" || balance.Decimals != 18 {
		return nil, fmt.Errorf("Data API returned unexpected native token metadata")
	}

	rawBalance, ok := new(big.Int).SetString(balance.Balance, 10)
	if !ok || rawBalance.Sign() < 0 {
		return nil, fmt.Errorf("Data API returned invalid balance")
	}

	return balance, nil
}

type DataAPIAddress struct {
	Address string `json:"address"`
}

type DataAPINativeTransaction struct {
	ChainID        string         `json:"chainId"`
	BlockNumber    string         `json:"blockNumber"`
	BlockTimestamp int64          `json:"blockTimestamp"`
	TxHash         string         `json:"txHash"`
	TxStatus       string         `json:"txStatus"`
	From           DataAPIAddress `json:"from"`
	To             DataAPIAddress `json:"to"`
	Value          string         `json:"value"`
}

type DataAPITransaction struct {
	NativeTransaction DataAPINativeTransaction `json:"nativeTransaction"`
}

type DataAPITransactionPage struct {
	Transactions  []DataAPITransaction `json:"transactions"`
	NextPageToken string               `json:"nextPageToken,omitempty"`
}

func (c *DataAPIClient) ListTransactions(
	ctx context.Context,
	walletAddress string,
	pageSize int,
	pageToken string,
) (*DataAPITransactionPage, error) {
	if c == nil || c.httpClient == nil {
		return nil, fmt.Errorf("Data API client is not initialized")
	}

	walletAddress = strings.TrimSpace(walletAddress)
	if !common.IsHexAddress(walletAddress) {
		return nil, fmt.Errorf("invalid wallet address")
	}

	if pageSize < 1 || pageSize > 100 {
		return nil, fmt.Errorf("page size must be between 1 and 100")
	}

	address := common.HexToAddress(walletAddress).Hex()

	query := url.Values{}
	query.Set("pageSize", fmt.Sprint(pageSize))
	query.Set("sortOrder", "desc")
	if pageToken != "" {
		query.Set("pageToken", pageToken)
	}

	endpoint := fmt.Sprintf(
		"%s/v1/chains/%d/addresses/%s/transactions?%s",
		dataAPIBaseURL,
		FujiChainID,
		address,
		query.Encode(),
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("create Data API transactions request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	if c.apiKey != "" {
		req.Header.Set("x-glacier-api-key", c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request Data API transactions: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf(
			"Data API transactions returned HTTP %d",
			resp.StatusCode,
		)
	}

	const maxResponseBytes = 1 << 20

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read Data API transactions response: %w", err)
	}
	if len(body) > maxResponseBytes {
		return nil, fmt.Errorf("Data API response exceeds size limit")
	}

	// A pointer distinguishes a missing/null field from an empty array.
	var response struct {
		Transactions  *[]DataAPITransaction `json:"transactions"`
		NextPageToken string                `json:"nextPageToken"`
	}

	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("decode Data API transactions: %w", err)
	}

	if response.Transactions == nil {
		return nil, fmt.Errorf("Data API response is missing transactions")
	}

	for _, entry := range *response.Transactions {
		tx := entry.NativeTransaction

		if tx.ChainID != fmt.Sprint(FujiChainID) {
			return nil, fmt.Errorf("Data API transaction has unexpected chain ID")
		}

		if tx.TxHash == "" {
			return nil, fmt.Errorf("Data API transaction is missing transaction hash")
		}

		rawValue, ok := new(big.Int).SetString(tx.Value, 10)
		if !ok || rawValue.Sign() < 0 {
			return nil, fmt.Errorf("Data API transaction has invalid value")
		}
	}

	return &DataAPITransactionPage{
		Transactions:  *response.Transactions,
		NextPageToken: response.NextPageToken,
	}, nil
}
