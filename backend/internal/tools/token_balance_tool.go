package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"unihack-backend/internal/ai"
	"unihack-backend/internal/avalanche"

	"github.com/ethereum/go-ethereum/common"
)

const (
	ListTokenBalancesToolName = "list_token_balances"
	defaultTokenPageSize      = 100
)

var ErrNilTokenBalanceClient = errors.New(
	"token balance client is nil",
)

type TokenBalanceClient interface {
	avalanche.TokenBalanceReader
}

type ListTokenBalancesArguments struct {
	WalletAddress string `json:"wallet_address"`
	ChainID       uint64 `json:"chain_id"`
	PageSize      int    `json:"page_size,omitempty"`
	PageToken     string `json:"page_token,omitempty"`
}

type ListTokenBalancesTool struct {
	client TokenBalanceClient
}

func NewListTokenBalancesTool(
	client TokenBalanceClient,
) (*ListTokenBalancesTool, error) {
	if client == nil {
		return nil, ErrNilTokenBalanceClient
	}

	return &ListTokenBalancesTool{
		client: client,
	}, nil
}

func (t *ListTokenBalancesTool) Definition() ai.ToolDefinition {
	return ai.ToolDefinition{
		Name: ListTokenBalancesToolName,
		Description: "List paginated ERC-20 token balances for " +
			"a wallet on Avalanche Fuji C-Chain.",
		InputSchema: json.RawMessage(`{
			"$schema": "https://json-schema.org/draft/2020-12/schema",
			"type": "object",
			"properties": {
				"wallet_address": {
					"type": "string",
					"pattern": "^0x[0-9a-fA-F]{40}$"
				},
				"chain_id": {
					"type": "integer",
					"const": 43113
				},
				"page_size": {
					"type": "integer",
					"minimum": 1,
					"maximum": 500,
					"default": 100
				},
				"page_token": {
					"type": "string",
					"maxLength": 2048
				}
			},
			"required": [
				"wallet_address",
				"chain_id"
			],
			"additionalProperties": false
		}`),
	}
}

func (t *ListTokenBalancesTool) Execute(
	ctx context.Context,
	arguments json.RawMessage,
) (ai.ToolResult, error) {
	var input ListTokenBalancesArguments

	if err := decodeStrictJSON(arguments, &input); err != nil {
		return newToolValidationError(
			"INVALID_TOOL_ARGUMENTS",
			"arguments must match the tool input schema",
			0,
		), nil
	}

	input.WalletAddress = strings.TrimSpace(
		input.WalletAddress,
	)
	input.PageToken = strings.TrimSpace(input.PageToken)

	if !common.IsHexAddress(input.WalletAddress) {
		return newToolValidationError(
			"INVALID_WALLET_ADDRESS",
			"wallet_address is invalid",
			input.ChainID,
		), nil
	}

	if input.ChainID != uint64(avalanche.FujiChainID) {
		return newToolValidationError(
			"UNSUPPORTED_CHAIN_ID",
			"chain_id must be 43113 for Avalanche Fuji",
			input.ChainID,
		), nil
	}

	if input.PageSize == 0 {
		input.PageSize = defaultTokenPageSize
	}

	if input.PageSize < 1 ||
		input.PageSize > maxERC20ToolPageSize {
		return newToolValidationError(
			"INVALID_PAGE_SIZE",
			"page_size must be between 1 and 500",
			input.ChainID,
		), nil
	}

	page, err := t.client.ListTokenBalances(
		ctx,
		input.WalletAddress,
		input.PageSize,
		input.PageToken,
	)
	if err != nil {
		return ai.ToolResult{}, fmt.Errorf(
			"list token balances: %w",
			err,
		)
	}

	data, err := json.Marshal(page)
	if err != nil {
		return ai.ToolResult{}, fmt.Errorf(
			"encode token balance result: %w",
			err,
		)
	}

	return ai.ToolResult{
		Data:        data,
		Source:      page.Provenance.Source,
		ChainID:     page.Provenance.ChainID,
		BlockNumber: page.Provenance.BlockNumber,
		ObservedAt:  page.Provenance.ObservedAt,
		Error:       nil,
	}, nil
}

const maxERC20ToolPageSize = 500

var _ ai.Tool = (*ListTokenBalancesTool)(nil)
