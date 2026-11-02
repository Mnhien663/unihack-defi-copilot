package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	demoNativeBalanceToolName = "get_native_balance"
	demoTokenBalancesToolName = "list_token_balances"
)

var (
	ErrDemoLLMNoMessages = errors.New("demo LLM received no messages")
	ErrDemoLLMNoTools    = errors.New("demo LLM received no tools")
	ErrDemoLLMNoTool     = errors.New("requested demo tool is unavailable")
)

type DemoLLMClient struct{}

func NewDemoLLMClient() *DemoLLMClient {
	return &DemoLLMClient{}
}

func (d *DemoLLMClient) Complete(
	ctx context.Context,
	request LLMRequest,
) (LLMResponse, error) {
	if err := ctx.Err(); err != nil {
		return LLMResponse{}, err
	}

	if len(request.Messages) == 0 {
		return LLMResponse{}, ErrDemoLLMNoMessages
	}

	lastMessage := request.Messages[len(request.Messages)-1]

	if lastMessage.Role == RoleTool {
		return d.finalResponse(request.Locale, lastMessage)
	}

	if len(request.Tools) == 0 {
		return LLMResponse{}, ErrDemoLLMNoTools
	}

	toolName := selectDemoTool(lastMessage.Content)

	if !hasDemoTool(request.Tools, toolName) {
		return LLMResponse{}, fmt.Errorf(
			"%w: %s",
			ErrDemoLLMNoTool,
			toolName,
		)
	}

	arguments, err := json.Marshal(
		struct {
			WalletAddress string `json:"wallet_address"`
			ChainID       uint64 `json:"chain_id"`
		}{
			WalletAddress: request.WalletAddress,
			ChainID:       request.ChainID,
		},
	)
	if err != nil {
		return LLMResponse{}, fmt.Errorf(
			"encode demo tool arguments: %w",
			err,
		)
	}

	return LLMResponse{
		Message: Message{
			Role: RoleAssistant,
		},
		ToolCalls: []ToolCall{
			{
				ID:        "demo-call-001",
				Name:      toolName,
				Arguments: arguments,
			},
		},
	}, nil
}

func selectDemoTool(message string) string {
	normalized := strings.ToLower(strings.TrimSpace(message))

	tokenKeywords := []string{
		"erc-20",
		"erc20",
		"token balance",
		"token balances",
		"list token",
		"usdc",
		"ddemo",
		"danh sách token",
		"các token",
		"số dư token",
	}

	for _, keyword := range tokenKeywords {
		if strings.Contains(normalized, keyword) {
			return demoTokenBalancesToolName
		}
	}

	return demoNativeBalanceToolName
}

func hasDemoTool(
	definitions []ToolDefinition,
	name string,
) bool {
	for _, definition := range definitions {
		if definition.Name == name {
			return true
		}
	}

	return false
}

func (d *DemoLLMClient) finalResponse(
	locale string,
	toolMessage Message,
) (LLMResponse, error) {
	var result ToolResult

	if err := json.Unmarshal(
		[]byte(toolMessage.Content),
		&result,
	); err != nil {
		return LLMResponse{}, fmt.Errorf(
			"decode demo tool result: %w",
			err,
		)
	}

	if result.Error != nil {
		return LLMResponse{
			Message: Message{
				Role: RoleAssistant,
				Content: fmt.Sprintf(
					"Mock tool error: %s",
					result.Error.Message,
				),
			},
		}, nil
	}

	var content string
	var err error

	switch toolMessage.Name {
	case demoTokenBalancesToolName:
		content, err = demoTokenBalanceResponse(locale, result)

	default:
		content, err = demoNativeBalanceResponse(locale, result)
	}

	if err != nil {
		return LLMResponse{}, err
	}

	return LLMResponse{
		Message: Message{
			Role:    RoleAssistant,
			Content: content,
		},
	}, nil
}

func demoNativeBalanceResponse(
	locale string,
	result ToolResult,
) (string, error) {
	var data struct {
		Balance struct {
			Asset struct {
				Symbol string `json:"symbol"`
			} `json:"asset"`
			Raw       string `json:"raw"`
			Formatted string `json:"formatted"`
		} `json:"balance"`
	}

	if err := json.Unmarshal(result.Data, &data); err != nil {
		return "", fmt.Errorf(
			"decode demo native balance data: %w",
			err,
		)
	}

	amount := data.Balance.Formatted
	if amount == "" {
		amount = data.Balance.Raw
	}

	symbol := data.Balance.Asset.Symbol
	if symbol == "" {
		symbol = "AVAX"
	}

	observedAt := formatObservedAt(result.ObservedAt)

	if locale == "vi" {
		return fmt.Sprintf(
			"Số dư native là %s %s trên Avalanche Fuji "+
				"(chain ID %d). Nguồn: %s, quan sát lúc %s.",
			amount,
			symbol,
			result.ChainID,
			result.Source,
			observedAt,
		), nil
	}

	return fmt.Sprintf(
		"Your native balance is %s %s on Avalanche Fuji "+
			"(chain ID %d). Source: %s, observed at %s.",
		amount,
		symbol,
		result.ChainID,
		result.Source,
		observedAt,
	), nil
}

func demoTokenBalanceResponse(
	locale string,
	result ToolResult,
) (string, error) {
	var data struct {
		Balances []struct {
			Name   string `json:"name"`
			Amount struct {
				Asset struct {
					Symbol string `json:"symbol"`
				} `json:"asset"`
				Raw       string `json:"raw"`
				Formatted string `json:"formatted"`
			} `json:"amount"`
		} `json:"balances"`
	}

	if err := json.Unmarshal(result.Data, &data); err != nil {
		return "", fmt.Errorf(
			"decode demo token balances data: %w",
			err,
		)
	}

	tokenDescriptions := make(
		[]string,
		0,
		len(data.Balances),
	)

	for _, balance := range data.Balances {
		amount := balance.Amount.Formatted
		if amount == "" {
			amount = balance.Amount.Raw
		}

		symbol := balance.Amount.Asset.Symbol
		if symbol == "" {
			symbol = balance.Name
		}

		tokenDescriptions = append(
			tokenDescriptions,
			fmt.Sprintf("%s %s", amount, symbol),
		)
	}

	if len(tokenDescriptions) == 0 {
		tokenDescriptions = append(tokenDescriptions, "none")
	}

	observedAt := formatObservedAt(result.ObservedAt)

	if locale == "vi" {
		return fmt.Sprintf(
			"Các số dư ERC-20 trên Avalanche Fuji "+
				"(chain ID %d): %s. Nguồn: %s, quan sát lúc %s.",
			result.ChainID,
			strings.Join(tokenDescriptions, ", "),
			result.Source,
			observedAt,
		), nil
	}

	return fmt.Sprintf(
		"ERC-20 balances on Avalanche Fuji "+
			"(chain ID %d): %s. Source: %s, observed at %s.",
		result.ChainID,
		strings.Join(tokenDescriptions, ", "),
		result.Source,
		observedAt,
	), nil
}

func formatObservedAt(observedAt time.Time) string {
	if observedAt.IsZero() {
		return "unknown"
	}

	return observedAt.UTC().Format(time.RFC3339)
}

var _ LLMClient = (*DemoLLMClient)(nil)
