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

const maxERC20PageSize = 500

type dataAPIERC20Balance struct {
	Address         string `json:"address"`
	Name            string `json:"name"`
	Symbol          string `json:"symbol"`
	Decimals        *int   `json:"decimals"`
	LogoURI         string `json:"logoUri"`
	ERCType         string `json:"ercType"`
	ChainID         string `json:"chainId"`
	Balance         string `json:"balance"`
	TokenReputation string `json:"tokenReputation"`
}

type dataAPIERC20BalanceResponse struct {
	ERC20TokenBalances *[]dataAPIERC20Balance `json:"erc20TokenBalances"`
	NextPageToken      string                 `json:"nextPageToken"`
}

func (c *DataAPIClient) ListTokenBalances(
	ctx context.Context,
	walletAddress string,
	pageSize int,
	pageToken string,
) (TokenBalancePage, error) {
	if c == nil || c.httpClient == nil {
		return TokenBalancePage{}, fmt.Errorf(
			"Data API client is not initialized",
		)
	}

	walletAddress = strings.TrimSpace(walletAddress)
	if !common.IsHexAddress(walletAddress) {
		return TokenBalancePage{}, fmt.Errorf(
			"invalid wallet address",
		)
	}

	if pageSize < 1 || pageSize > maxERC20PageSize {
		return TokenBalancePage{}, fmt.Errorf(
			"page size must be between 1 and %d",
			maxERC20PageSize,
		)
	}

	checksumWallet := common.HexToAddress(
		walletAddress,
	).Hex()

	query := url.Values{}
	query.Set("pageSize", fmt.Sprint(pageSize))
	query.Set("filterSpamTokens", "true")

	pageToken = strings.TrimSpace(pageToken)
	if pageToken != "" {
		query.Set("pageToken", pageToken)
	}

	endpoint := fmt.Sprintf(
		"%s/v1/chains/%d/addresses/%s/balances:listErc20?%s",
		c.baseURL,
		FujiChainID,
		checksumWallet,
		query.Encode(),
	)

	response, err := c.doGET(ctx, endpoint)
	if err != nil {
		return TokenBalancePage{}, fmt.Errorf(
			"request Data API ERC-20 balances: %w",
			err,
		)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return TokenBalancePage{}, fmt.Errorf(
			"Data API ERC-20 balances returned HTTP %d",
			response.StatusCode,
		)
	}

	const maxResponseBytes = 2 << 20

	body, err := io.ReadAll(
		io.LimitReader(
			response.Body,
			maxResponseBytes+1,
		),
	)
	if err != nil {
		return TokenBalancePage{}, fmt.Errorf(
			"read Data API ERC-20 response: %w",
			err,
		)
	}

	if len(body) > maxResponseBytes {
		return TokenBalancePage{}, fmt.Errorf(
			"Data API ERC-20 response exceeds size limit",
		)
	}

	var decoded dataAPIERC20BalanceResponse

	if err := json.Unmarshal(body, &decoded); err != nil {
		return TokenBalancePage{}, fmt.Errorf(
			"decode Data API ERC-20 balances: %w",
			err,
		)
	}

	if decoded.ERC20TokenBalances == nil {
		return TokenBalancePage{}, fmt.Errorf(
			"Data API response is missing erc20TokenBalances",
		)
	}

	if len(*decoded.ERC20TokenBalances) > pageSize {
		return TokenBalancePage{}, fmt.Errorf(
			"Data API ERC-20 response exceeds requested page size",
		)
	}

	balances := make(
		[]TokenBalance,
		0,
		len(*decoded.ERC20TokenBalances),
	)

	for _, item := range *decoded.ERC20TokenBalances {
		if item.ChainID != fmt.Sprint(FujiChainID) {
			return TokenBalancePage{}, fmt.Errorf(
				"ERC-20 balance has unexpected chain ID",
			)
		}

		if !common.IsHexAddress(item.Address) {
			return TokenBalancePage{}, fmt.Errorf(
				"ERC-20 balance has invalid contract address",
			)
		}

		if item.ERCType != "" && item.ERCType != "ERC-20" {
			return TokenBalancePage{}, fmt.Errorf(
				"unexpected token type %q",
				item.ERCType,
			)
		}

		if item.Decimals == nil {
			return TokenBalancePage{}, fmt.Errorf(
				"%w: ERC-20 decimals is missing",
				ErrTokenMetadataUnavailable,
			)
		}

		if *item.Decimals < 0 || *item.Decimals > 255 {
			return TokenBalancePage{}, fmt.Errorf(
				"%w: ERC-20 decimals is outside uint8 bounds",
				ErrTokenMetadataUnavailable,
			)
		}

		decimals := uint8(*item.Decimals)
		if item.Balance == "" ||
			strings.IndexFunc(item.Balance, func(r rune) bool {
				return r < '0' || r > '9'
			}) >= 0 {
			return TokenBalancePage{}, fmt.Errorf(
				"ERC-20 balance has invalid raw amount",
			)
		}
		rawAmount, ok := new(big.Int).SetString(
			item.Balance,
			10,
		)
		if !ok || rawAmount.Sign() < 0 {
			return TokenBalancePage{}, fmt.Errorf(
				"ERC-20 balance has invalid raw amount",
			)
		}

		if rawAmount.BitLen() > 256 {
			return TokenBalancePage{}, fmt.Errorf(
				"ERC-20 balance exceeds uint256",
			)
		}

		formatted, err := FormatTokenAmount(
			item.Balance,
			decimals,
		)
		if err != nil {
			return TokenBalancePage{}, fmt.Errorf(
				"format ERC-20 balance: %w",
				err,
			)
		}

		balances = append(
			balances,
			TokenBalance{
				Name: item.Name,
				Amount: TokenAmount{
					Asset: Asset{
						ChainID: uint64(FujiChainID),
						Address: common.HexToAddress(
							item.Address,
						).Hex(),
						Symbol:   item.Symbol,
						Decimals: decimals,
					},
					Raw:       item.Balance,
					Formatted: formatted,
				},
				LogoURI:         item.LogoURI,
				TokenReputation: item.TokenReputation,
			},
		)
	}

	return TokenBalancePage{
		WalletAddress: checksumWallet,
		Balances:      balances,
		NextPageToken: strings.TrimSpace(
			decoded.NextPageToken,
		),
		Provenance: Provenance{
			Source:      "Avalanche Data API",
			SourceType:  "data_api",
			ChainID:     uint64(FujiChainID),
			BlockNumber: nil,
			ObservedAt:  time.Now().UTC(),
			Mode:        "live",
			IsDemo:      false,
		},
	}, nil
}

var _ TokenBalanceReader = (*DataAPIClient)(nil)
