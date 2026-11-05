package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"unihack-backend/internal/ai"
	"unihack-backend/internal/avalanche"

	"github.com/ethereum/go-ethereum/common"
)

const (
	ListTransactionsToolName = "list_transactions"
	defaultHistoryPageSize   = 10
	maxHistoryPageSize       = 20
	historyReadTimeout       = 5 * time.Second
)

type TransactionClient interface {
	ListTransactions(
		ctx context.Context,
		walletAddress string,
		pageSize int,
		pageToken string,
	) (*avalanche.DataAPITransactionPage, error)
}

type ListTransactionsArguments struct {
	WalletAddress string `json:"wallet_address"`
	ChainID       uint64 `json:"chain_id"`
	PageSize      *int   `json:"page_size,omitempty"`
	PageToken     string `json:"page_token,omitempty"`
}

type ListTransactionsTool struct {
	client TransactionClient
	source string
}

func NewListTransactionsTool(
	client TransactionClient,
	source string,
) (*ListTransactionsTool, error) {
	if client == nil {
		return nil, errors.New("transaction client is nil")
	}

	source = strings.TrimSpace(source)
	if source == "" {
		return nil, errors.New("transaction source is empty")
	}

	return &ListTransactionsTool{
		client: client,
		source: source,
	}, nil
}

func (t *ListTransactionsTool) Definition() ai.ToolDefinition {
	return ai.ToolDefinition{
		Name: ListTransactionsToolName,
		Description: "Read one page of wallet transaction history on " +
			"Avalanche Fuji. Returns at most 20 records. " +
			"Use the returned next_page_token unchanged for the next page. " +
			"An empty page does not prove the wallet has never transacted.",
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
				"page_size": {
					"type": "integer",
					"minimum": 1,
					"maximum": 20,
					"default": 10
				},
				"page_token": {
					"type": "string",
					"maxLength": 2048
				}
			},
			"required": ["wallet_address", "chain_id"],
			"additionalProperties": false
		}`),
	}
}

func (t *ListTransactionsTool) Execute(
	ctx context.Context,
	arguments json.RawMessage,
) (ai.ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return ai.ToolResult{}, err
	}

	var input ListTransactionsArguments
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

	pageSize := defaultHistoryPageSize
	if input.PageSize != nil {
		pageSize = *input.PageSize
	}

	if pageSize < 1 || pageSize > maxHistoryPageSize {
		return newToolValidationError(
			"INVALID_PAGE_SIZE",
			"page_size must be between 1 and 20",
			input.ChainID,
		), nil
	}

	// Cursor is opaque: validate length, but do not trim or decode it.
	if len(input.PageToken) > 2048 {
		return newToolValidationError(
			"INVALID_PAGE_TOKEN",
			"page_token is too long",
			input.ChainID,
		), nil
	}

	wallet := common.HexToAddress(input.WalletAddress).Hex()

	readCtx, cancel := context.WithTimeout(ctx, historyReadTimeout)
	defer cancel()

	page, err := t.client.ListTransactions(
		readCtx,
		wallet,
		pageSize,
		input.PageToken,
	)
	if err != nil {
		// Preserve cancellation of the whole chat request.
		if ctx.Err() != nil {
			return ai.ToolResult{}, ctx.Err()
		}

		var timeoutError net.Error
		if errors.Is(err, context.DeadlineExceeded) ||
			errors.Is(readCtx.Err(), context.DeadlineExceeded) ||
			(errors.As(err, &timeoutError) && timeoutError.Timeout()) {
			return newToolValidationError(
				"UPSTREAM_TIMEOUT",
				"transaction history service timed out",
				input.ChainID,
			), nil
		}

		return newToolValidationError(
			"UPSTREAM_ERROR",
			"transaction history service could not complete the request",
			input.ChainID,
		), nil
	}

	if page == nil {
		return newToolValidationError(
			"UPSTREAM_INVALID_RESPONSE",
			"transaction history service returned no response",
			input.ChainID,
		), nil
	}

	// Reject an oversized response rather than silently dropping records,
	// which could break the provider's pagination cursor.
	if len(page.Transactions) > pageSize {
		return newToolValidationError(
			"UPSTREAM_INVALID_RESPONSE",
			"transaction history exceeded the requested page size",
			input.ChainID,
		), nil
	}

	transactions := page.Transactions
	if transactions == nil {
		transactions = []avalanche.DataAPITransaction{}
	}

	observedAt := time.Now().UTC()

	payload := struct {
		WalletAddress string                         `json:"wallet_address"`
		Transactions  []avalanche.DataAPITransaction `json:"transactions"`
		NextPageToken string                         `json:"next_page_token,omitempty"`
		Source        string                         `json:"source"`
		ChainID       uint64                         `json:"chain_id"`
		ObservedAt    time.Time                      `json:"observed_at"`
	}{
		WalletAddress: wallet,
		Transactions:  transactions,
		NextPageToken: page.NextPageToken,
		Source:        t.source,
		ChainID:       input.ChainID,
		ObservedAt:    observedAt,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return ai.ToolResult{}, fmt.Errorf(
			"encode transaction history: %w",
			err,
		)
	}

	result := ai.ToolResult{
		Data:       data,
		Source:     t.source,
		ChainID:    input.ChainID,
		ObservedAt: observedAt,
	}

	if len(transactions) == 0 {
		empty := newToolValidationError(
			"EMPTY_HISTORY",
			"no transactions were returned for this page",
			input.ChainID,
		)
		result.Error = empty.Error
	}

	return result, nil
}

var _ ai.Tool = (*ListTransactionsTool)(nil)
var _ TransactionClient = (*avalanche.DataAPIClient)(nil)
