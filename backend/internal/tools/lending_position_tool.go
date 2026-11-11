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

const GetLendingPositionToolName = "get_lending_position"

type GetLendingPositionTool struct {
	reader avalanche.LendingPositionReader
}

func NewGetLendingPositionTool(
	reader avalanche.LendingPositionReader,
) (*GetLendingPositionTool, error) {
	if reader == nil {
		return nil, errors.New("lending position reader is nil")
	}
	return &GetLendingPositionTool{reader: reader}, nil
}

func (t *GetLendingPositionTool) Definition() ai.ToolDefinition {
	return ai.ToolDefinition{
		Name: GetLendingPositionToolName,
		Description: "Read a wallet's Aave V3 position on Avalanche Fuji: " +
			"supplied assets, collateral flags, stable and variable debt, " +
			"and protocol health factor when available. " +
			"health_factor_raw uses 18 decimals and may be absent. " +
			"Do not treat a missing health factor as zero or infer no debt " +
			"from it alone. This tool does not compute a liquidation price " +
			"or submit transactions.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"wallet_address": {
					"type": "string",
					"pattern": "^0x[0-9a-fA-F]{40}$"
				},
				"chain_id": {"type": "integer", "const": 43113}
			},
			"required": ["wallet_address", "chain_id"],
			"additionalProperties": false
		}`),
	}
}

func (t *GetLendingPositionTool) Execute(
	ctx context.Context,
	arguments json.RawMessage,
) (ai.ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return ai.ToolResult{}, err
	}

	var input struct {
		WalletAddress string `json:"wallet_address"`
		ChainID       uint64 `json:"chain_id"`
	}
	if err := decodeStrictJSON(arguments, &input); err != nil {
		return newToolValidationError(
			"INVALID_TOOL_ARGUMENTS",
			"arguments must match the lending position input schema",
			0,
		), nil
	}

	fail := func(code, message string) (ai.ToolResult, error) {
		return newToolValidationError(code, message, input.ChainID), nil
	}

	address := strings.TrimSpace(input.WalletAddress)
	if !common.IsHexAddress(address) {
		return fail("INVALID_WALLET_ADDRESS", "wallet_address is invalid")
	}
	wallet := common.HexToAddress(address)
	if wallet == (common.Address{}) {
		return fail("INVALID_WALLET_ADDRESS", "wallet_address cannot be the zero address")
	}
	if input.ChainID != uint64(avalanche.FujiChainID) {
		return fail("UNSUPPORTED_CHAIN_ID", "chain_id must be 43113")
	}

	position, err := t.reader.ReadLendingPosition(ctx, wallet.Hex())
	if ctx.Err() != nil {
		return ai.ToolResult{}, ctx.Err()
	}
	if err != nil {
		var timeoutErr net.Error
		switch {
		case errors.Is(err, avalanche.ErrWrongChain):
			return fail("UNSUPPORTED_CHAIN_ID", "lending RPC endpoint is not Avalanche Fuji")
		case errors.Is(err, context.DeadlineExceeded),
			errors.As(err, &timeoutErr) && timeoutErr.Timeout():
			return fail("UPSTREAM_TIMEOUT", "lending RPC request timed out")
		default:
			return fail("UPSTREAM_LENDING_ERROR", "could not read a complete lending position")
		}
	}

	source := position.Provenance
	if !common.IsHexAddress(position.WalletAddress) ||
		common.HexToAddress(position.WalletAddress) != wallet ||
		source.ChainID != input.ChainID ||
		source.BlockNumber == nil ||
		source.ObservedAt.IsZero() ||
		strings.TrimSpace(source.Source) == "" {
		return fail("INVALID_LENDING_RESPONSE", "lending snapshot has invalid wallet or provenance")
	}

	if position.Positions == nil {
		position.Positions = []avalanche.LendingAssetPosition{}
	}

	data, err := json.Marshal(position)
	if err != nil {
		return ai.ToolResult{}, fmt.Errorf("encode lending position: %w", err)
	}

	return ai.ToolResult{
		Data:        data,
		Source:      source.Source,
		ChainID:     source.ChainID,
		BlockNumber: source.BlockNumber,
		ObservedAt:  source.ObservedAt,
	}, nil
}

var _ ai.Tool = (*GetLendingPositionTool)(nil)
