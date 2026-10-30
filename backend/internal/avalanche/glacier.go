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
	baseURL    string
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
		apiKey:  strings.TrimSpace(apiKey),
		baseURL: dataAPIBaseURL,
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
		c.baseURL,
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

// DataAPIERC20Balance represents an indexed ERC-20 balance.
// Balance remains a decimal string in the token's smallest units.
type DataAPIERC20Balance struct {
	Address  string `json:"address"`
	ChainID  string `json:"chainId"`
	Name     string `json:"name"`
	Symbol   string `json:"symbol"`
	Decimals int    `json:"decimals"`
	Balance  string `json:"balance"`
}

// DataAPIERC20BalancePage contains one page, not all wallet tokens.
type DataAPIERC20BalancePage struct {
	Balances      []DataAPIERC20Balance `json:"erc20TokenBalances"`
	NextPageToken string                `json:"nextPageToken,omitempty"`
	Provenance    Provenance            `json:"provenance"`
}

func (c *DataAPIClient) ListERC20Balances(
	ctx context.Context,
	walletAddress string,
	pageSize int,
	pageToken string,
) (*DataAPIERC20BalancePage, error) {
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

	query := url.Values{}
	query.Set("pageSize", fmt.Sprint(pageSize))
	if pageToken != "" {
		query.Set("pageToken", pageToken)
	}

	endpoint := fmt.Sprintf(
		"%s/v1/chains/%d/addresses/%s/balances:listErc20?%s",
		c.baseURL,
		FujiChainID,
		common.HexToAddress(walletAddress).Hex(),
		query.Encode(),
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("create Data API ERC-20 request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if c.apiKey != "" {
		req.Header.Set("x-glacier-api-key", c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request Data API ERC-20 balances: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf(
			"Data API ERC-20 balances returned HTTP %d",
			resp.StatusCode,
		)
	}

	const maxResponseBytes = 1 << 20
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read Data API ERC-20 response: %w", err)
	}
	if len(body) > maxResponseBytes {
		return nil, fmt.Errorf("Data API response exceeds size limit")
	}

	var response struct {
		Balances      *[]DataAPIERC20Balance `json:"erc20TokenBalances"`
		NextPageToken string                 `json:"nextPageToken"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("decode Data API ERC-20 balances: %w", err)
	}
	if response.Balances == nil {
		return nil, fmt.Errorf("Data API response is missing erc20TokenBalances")
	}
	if len(*response.Balances) > pageSize {
		return nil, fmt.Errorf("Data API ERC-20 response exceeds requested page size")
	}

	for _, balance := range *response.Balances {
		if balance.ChainID != fmt.Sprint(FujiChainID) {
			return nil, fmt.Errorf("Data API ERC-20 balance has unexpected chain ID")
		}
		if !common.IsHexAddress(balance.Address) {
			return nil, fmt.Errorf("Data API ERC-20 balance has invalid token address")
		}
		if balance.Decimals < 0 || balance.Decimals > 255 {
			return nil, fmt.Errorf("Data API ERC-20 balance has invalid decimals")
		}

		// Accept decimal digits only, within uint256 bounds.
		if balance.Balance == "" ||
			strings.IndexFunc(balance.Balance, func(r rune) bool {
				return r < '0' || r > '9'
			}) >= 0 {
			return nil, fmt.Errorf("Data API ERC-20 balance has invalid raw amount")
		}
		raw, ok := new(big.Int).SetString(balance.Balance, 10)
		if !ok || raw.BitLen() > 256 {
			return nil, fmt.Errorf("Data API ERC-20 balance exceeds uint256")
		}
	}

	return &DataAPIERC20BalancePage{
		Balances:      *response.Balances,
		NextPageToken: response.NextPageToken,
		Provenance: Provenance{
			Source:     "Avalanche Data API",
			SourceType: "data_api",
			ChainID:    uint64(FujiChainID),
			ObservedAt: time.Now().UTC(),
			Mode:       "live",
			IsDemo:     false,
		},
	}, nil
}

// DataAPIHistoryResult may contain only part of the wallet's history.
// Truncated means another page remains available.
type DataAPIHistoryResult struct {
	Transactions  []DataAPITransaction
	NextPageToken string
	Truncated     bool
	Provenance    Provenance `json:"provenance"`
}

func (c *DataAPIClient) ReadTransactionHistory(
	ctx context.Context,
	walletAddress string,
) (*DataAPIHistoryResult, error) {
	const maxPages = 2
	const maxRecords = 100
	const pageSize = 50

	result := &DataAPIHistoryResult{
		Transactions: make([]DataAPITransaction, 0),
		Provenance: Provenance{
			Source:     "Avalanche Data API",
			SourceType: "data_api",
			ChainID:    uint64(FujiChainID),
			Mode:       "live",
			IsDemo:     false,
		},
	}
	cursor := ""
	seen := map[string]bool{"": true}

	for pageNumber := 0; pageNumber < maxPages; pageNumber++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		limit := pageSize
		if remaining := maxRecords - len(result.Transactions); remaining < limit {
			limit = remaining
		}

		page, err := c.ListTransactions(
			ctx, walletAddress, limit, cursor,
		)
		if err != nil {
			return nil, fmt.Errorf("read history page %d: %w", pageNumber+1, err)
		}
		if len(page.Transactions) > limit {
			return nil, fmt.Errorf("history response exceeds requested page size")
		}

		result.Transactions = append(result.Transactions, page.Transactions...)
		result.NextPageToken = page.NextPageToken
		result.Provenance.ObservedAt = time.Now().UTC()

		if page.NextPageToken == "" {
			return result, nil
		}
		if seen[page.NextPageToken] {
			return nil, fmt.Errorf("Data API returned a repeated history cursor")
		}
		seen[page.NextPageToken] = true
		cursor = page.NextPageToken

		if len(result.Transactions) >= maxRecords {
			break
		}
	}

	result.Truncated = true
	return result, nil
}

// ReadNativeBalance adapts the Data API response to the shared result type.
func (c *DataAPIClient) ReadNativeBalance(
	ctx context.Context,
	walletAddress string,
) (NativeBalanceResult, error) {
	balance, err := c.GetNativeBalance(ctx, walletAddress)
	if err != nil {
		return NativeBalanceResult{}, err
	}

	raw, ok := new(big.Int).SetString(balance.Balance, 10)
	if !ok || raw.Sign() < 0 || raw.BitLen() > 256 {
		return NativeBalanceResult{}, fmt.Errorf(
			"Data API native balance is outside uint256 bounds",
		)
	}

	return NativeBalanceResult{
		WalletAddress: common.HexToAddress(
			strings.TrimSpace(walletAddress),
		).Hex(),
		Balance: TokenAmount{
			Asset: Asset{
				ChainID:  uint64(FujiChainID),
				Symbol:   balance.Symbol,
				Decimals: uint8(balance.Decimals),
			},
			Raw: raw.String(),
		},
		Provenance: Provenance{
			Source:     "Avalanche Data API",
			SourceType: "data_api",
			ChainID:    uint64(FujiChainID),
			ObservedAt: time.Now().UTC(),
			Mode:       "live",
			IsDemo:     false,
		},
	}, nil
}
