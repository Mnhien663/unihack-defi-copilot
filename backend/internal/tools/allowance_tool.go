package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"

	"unihack-backend/internal/ai"
	"unihack-backend/internal/avalanche"

	"github.com/ethereum/go-ethereum/common"
)

const GetAllowanceToolName = "get_allowance"

type GetAllowanceArguments struct {
	WalletAddress  string `json:"wallet_address"`
	ChainID        uint64 `json:"chain_id"`
	TokenAddress   string `json:"token_address"`
	SpenderAddress string `json:"spender_address"`
}

type GetAllowanceTool struct {
	reader avalanche.AllowanceReader
}

func NewGetAllowanceTool(
	reader avalanche.AllowanceReader,
) (*GetAllowanceTool, error) {
	if reader == nil {
		return nil, errors.New("allowance reader is nil")
	}

	return &GetAllowanceTool{reader: reader}, nil
}

func (t *GetAllowanceTool) Definition() ai.ToolDefinition {
	return ai.ToolDefinition{
		Name: GetAllowanceToolName,
		Description: "Read the ERC-20 spending allowance granted by " +
			"wallet_address (owner) to spender_address on Avalanche Fuji. " +
			"Allowance is NOT the wallet balance and does NOT guarantee " +
			"a transfer will succeed. Only application-allowlisted tokens " +
			"are supported. This read does not approve or send transactions. " +
			"Ask for missing token or spender; never invent their addresses.",
		InputSchema: json.RawMessage(`{
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
				"token_address": {
					"type": "string",
					"pattern": "^0x[0-9a-fA-F]{40}$"
				},
				"spender_address": {
					"type": "string",
					"pattern": "^0x[0-9a-fA-F]{40}$"
				}
			},
			"required": [
				"wallet_address",
				"chain_id",
				"token_address",
				"spender_address"
			],
			"additionalProperties": false
		}`),
	}
}

func (t *GetAllowanceTool) Execute(
	ctx context.Context,
	arguments json.RawMessage,
) (ai.ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return ai.ToolResult{}, err
	}

	var input GetAllowanceArguments
	if err := decodeStrictJSON(arguments, &input); err != nil {
		return newToolValidationError(
			"INVALID_TOOL_ARGUMENTS",
			"arguments must match the tool input schema",
			0,
		), nil
	}

	input.WalletAddress = strings.TrimSpace(input.WalletAddress)
	input.TokenAddress = strings.TrimSpace(input.TokenAddress)
	input.SpenderAddress = strings.TrimSpace(input.SpenderAddress)

	for _, item := range []struct {
		value   string
		code    string
		message string
	}{
		{
			input.WalletAddress,
			"INVALID_WALLET_ADDRESS",
			"wallet_address is invalid",
		},
		{
			input.TokenAddress,
			"INVALID_TOKEN_ADDRESS",
			"token_address is invalid",
		},
		{
			input.SpenderAddress,
			"INVALID_SPENDER_ADDRESS",
			"spender_address is invalid",
		},
	} {
		if !common.IsHexAddress(item.value) {
			return newToolValidationError(
				item.code,
				item.message,
				input.ChainID,
			), nil
		}
	}

	if input.ChainID != uint64(avalanche.FujiChainID) {
		return newToolValidationError(
			"UNSUPPORTED_CHAIN_ID",
			"chain_id must be 43113 for Avalanche Fuji",
			input.ChainID,
		), nil
	}

	result, err := t.reader.ReadAllowance(
		ctx,
		common.HexToAddress(input.TokenAddress).Hex(),
		common.HexToAddress(input.WalletAddress).Hex(),
		common.HexToAddress(input.SpenderAddress).Hex(),
	)
	if err != nil {
		if ctx.Err() != nil {
			return ai.ToolResult{}, ctx.Err()
		}

		code := "UPSTREAM_ERROR"
		message := "allowance could not be read"

		var timeoutError net.Error

		switch {
		case errors.Is(err, avalanche.ErrAllowanceUnsupportedToken):
			code = "UNSUPPORTED_TOKEN"
			message = "token is not in the application allowlist"

		case errors.Is(err, avalanche.ErrWrongChain):
			code = "UNSUPPORTED_CHAIN_ID"
			message = "RPC endpoint is not Avalanche Fuji"

		case errors.Is(err, context.DeadlineExceeded),
			errors.As(err, &timeoutError) && timeoutError.Timeout():
			code = "UPSTREAM_TIMEOUT"
			message = "allowance RPC request timed out"

		case errors.Is(err, avalanche.ErrTokenMetadataUnavailable):
			code = "TOKEN_METADATA_UNAVAILABLE"
			message = "token decimals could not be read; amount was not guessed"
		}

		return newToolValidationError(
			code,
			message,
			input.ChainID,
		), nil
	}

	data, err := json.Marshal(result)
	if err != nil {
		return ai.ToolResult{}, fmt.Errorf(
			"encode allowance result: %w",
			err,
		)
	}

	return ai.ToolResult{
		Data:        data,
		Source:      result.Provenance.Source,
		ChainID:     result.Provenance.ChainID,
		BlockNumber: result.Provenance.BlockNumber,
		ObservedAt:  result.Provenance.ObservedAt,
	}, nil
}

var _ ai.Tool = (*GetAllowanceTool)(nil)
