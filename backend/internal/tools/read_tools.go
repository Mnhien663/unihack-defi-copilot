package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"unihack-backend/internal/ai"
	"unihack-backend/internal/avalanche"

	"github.com/ethereum/go-ethereum/common"
)

const NativeBalanceToolName = "get_native_balance"

var ErrNilChainClient = errors.New("chain client is nil")

// ChainClient is the AI-facing boundary for chain reads.
type ChainClient interface {
	avalanche.ChainReader
}

type NativeBalanceArguments struct {
	WalletAddress string `json:"wallet_address"`
	ChainID       uint64 `json:"chain_id"`
}

type NativeBalanceTool struct {
	chainClient ChainClient
}

func NewNativeBalanceTool(
	chainClient ChainClient,
) (*NativeBalanceTool, error) {
	if chainClient == nil {
		return nil, ErrNilChainClient
	}

	return &NativeBalanceTool{
		chainClient: chainClient,
	}, nil
}

func (t *NativeBalanceTool) Definition() ai.ToolDefinition {
	return ai.ToolDefinition{
		Name: NativeBalanceToolName,
		Description: "Read the native AVAX balance of a wallet " +
			"on Avalanche Fuji C-Chain.",
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

func (t *NativeBalanceTool) Execute(
	ctx context.Context,
	arguments json.RawMessage,
) (ai.ToolResult, error) {
	var input NativeBalanceArguments

	if err := decodeStrictJSON(arguments, &input); err != nil {
		return newToolValidationError(
			"INVALID_TOOL_ARGUMENTS",
			"arguments must match the tool input schema",
			0,
		), nil
	}

	input.WalletAddress = strings.TrimSpace(input.WalletAddress)

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

	balance, err := t.chainClient.ReadNativeBalance(
		ctx,
		input.WalletAddress,
	)
	if err != nil {
		return ai.ToolResult{}, fmt.Errorf(
			"read native balance: %w",
			err,
		)
	}

	data, err := json.Marshal(balance)
	if err != nil {
		return ai.ToolResult{}, fmt.Errorf(
			"encode native balance result: %w",
			err,
		)
	}

	return ai.ToolResult{
		Data:        data,
		Source:      balance.Provenance.Source,
		ChainID:     balance.Provenance.ChainID,
		BlockNumber: balance.Provenance.BlockNumber,
		ObservedAt:  balance.Provenance.ObservedAt,
		Error:       nil,
	}, nil
}

func decodeStrictJSON(
	data json.RawMessage,
	target any,
) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(target); err != nil {
		return err
	}

	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return errors.New("multiple JSON values are not allowed")
		}

		return err
	}

	return nil
}

func newToolValidationError(
	code string,
	message string,
	chainID uint64,
) ai.ToolResult {
	return ai.ToolResult{
		Data:        json.RawMessage(`null`),
		Source:      "validation",
		ChainID:     chainID,
		BlockNumber: nil,
		ObservedAt:  time.Now().UTC(),
		Error: &ai.ToolError{
			Code:    code,
			Message: message,
		},
	}
}

var _ ai.Tool = (*NativeBalanceTool)(nil)
