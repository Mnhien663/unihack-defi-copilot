package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

const (
	demoNativeBalanceToolName = "get_native_balance"
	demoTokenBalancesToolName = "list_token_balances"
	demoTransactionsToolName  = "list_transactions"
	demoAllowanceToolName     = "get_allowance"
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

	toolArguments := map[string]any{
		"wallet_address": request.WalletAddress,
		"chain_id":       request.ChainID,
	}

	if toolName == demoAllowanceToolName {
		token := demoNamedAddress(lastMessage.Content, "token_address")
		spender := demoNamedAddress(lastMessage.Content, "spender_address")

		if token == "" || spender == "" {
			content := "Please provide token_address and spender_address " +
				"as complete 0x addresses. Allowance is spending permission, " +
				"not your wallet balance."

			if request.Locale == "vi" {
				content = "Vui lòng cung cấp token_address và spender_address " +
					"dạng địa chỉ 0x đầy đủ. Allowance là quyền chi tiêu, " +
					"không phải số dư ví."
			}

			return LLMResponse{
				Message: Message{
					Role:    RoleAssistant,
					Content: content,
				},
			}, nil
		}

		toolArguments["token_address"] = token
		toolArguments["spender_address"] = spender
	}

	arguments, err := json.Marshal(toolArguments)
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

	if strings.Contains(normalized, "allowance") ||
		strings.Contains(normalized, "quyền chi tiêu") ||
		strings.Contains(normalized, "hạn mức chi tiêu") {
		return demoAllowanceToolName
	}

	historyKeywords := []string{
		"transaction",
		"history",
		"lịch sử",
		"giao dịch",
	}

	for _, keyword := range historyKeywords {
		if strings.Contains(normalized, keyword) {
			return demoTransactionsToolName
		}
	}

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

	if result.Error != nil &&
		!(toolMessage.Name == demoTransactionsToolName &&
			result.Error.Code == "EMPTY_HISTORY") {
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
	case demoTransactionsToolName:
		content, err = demoTransactionResponse(locale, result)

	case demoTokenBalancesToolName:
		content, err = demoTokenBalanceResponse(locale, result)

	case demoAllowanceToolName:
		content, err = demoAllowanceResponse(locale, result)

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

func demoTransactionResponse(
	locale string,
	result ToolResult,
) (string, error) {
	var data struct {
		Transactions []struct {
			NativeTransaction struct {
				TxHash   string `json:"txHash"`
				TxStatus string `json:"txStatus"`
			} `json:"nativeTransaction"`
		} `json:"transactions"`
		NextPageToken string `json:"next_page_token"`
	}

	if err := json.Unmarshal(result.Data, &data); err != nil {
		return "", fmt.Errorf(
			"decode demo transaction history: %w",
			err,
		)
	}

	observedAt := formatObservedAt(result.ObservedAt)

	if len(data.Transactions) == 0 {
		if locale == "vi" {
			return fmt.Sprintf(
				"Trang này không có giao dịch trên Avalanche Fuji "+
					"(chain ID %d). Nguồn: %s, quan sát lúc %s.",
				result.ChainID,
				result.Source,
				observedAt,
			), nil
		}

		return fmt.Sprintf(
			"No transactions were returned for this page on Avalanche Fuji "+
				"(chain ID %d). Source: %s, observed at %s.",
			result.ChainID,
			result.Source,
			observedAt,
		), nil
	}

	items := make([]string, 0, len(data.Transactions))
	for _, item := range data.Transactions {
		tx := item.NativeTransaction
		status := tx.TxStatus
		if status == "" {
			status = "unknown"
		}

		// Preserve the upstream status rather than guessing its meaning.
		items = append(items, fmt.Sprintf(
			"%s (status: %s)",
			tx.TxHash,
			status,
		))
	}

	if locale == "vi" {
		more := ""
		if data.NextPageToken != "" {
			more = " Có trang tiếp theo."
		}

		return fmt.Sprintf(
			"Trang lịch sử có %d giao dịch trên Avalanche Fuji "+
				"(chain ID %d): %s.%s Nguồn: %s, quan sát lúc %s.",
			len(items),
			result.ChainID,
			strings.Join(items, "; "),
			more,
			result.Source,
			observedAt,
		), nil
	}

	more := ""
	if data.NextPageToken != "" {
		more = " Another page is available."
	}

	return fmt.Sprintf(
		"This history page contains %d transactions on Avalanche Fuji "+
			"(chain ID %d): %s.%s Source: %s, observed at %s.",
		len(items),
		result.ChainID,
		strings.Join(items, "; "),
		more,
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

func demoNamedAddress(message, field string) string {
	pattern := `(?i)\b` + regexp.QuoteMeta(field) +
		`\s*[:=]\s*(0x[0-9a-f]{40})\b`

	match := regexp.MustCompile(pattern).FindStringSubmatch(message)
	if len(match) != 2 {
		return ""
	}

	return match[1]
}

func demoAllowanceResponse(
	locale string,
	result ToolResult,
) (string, error) {
	var data struct {
		OwnerAddress   string `json:"owner_address"`
		SpenderAddress string `json:"spender_address"`
		Allowance      struct {
			Asset struct {
				Address string `json:"address"`
			} `json:"asset"`
			Formatted string `json:"formatted"`
		} `json:"allowance"`
	}

	if err := json.Unmarshal(result.Data, &data); err != nil {
		return "", fmt.Errorf("decode demo allowance: %w", err)
	}

	if data.Allowance.Formatted == "" {
		return "", errors.New("demo allowance formatted amount is missing")
	}

	if locale == "vi" {
		return fmt.Sprintf(
			"Ví %s đã cấp quyền chi tiêu %s token của hợp đồng %s "+
				"cho spender %s trên Avalanche Fuji (chain ID %d). "+
				"Đây không phải số dư ví và không đảm bảo chuyển token thành công. "+
				"Nguồn: %s, quan sát lúc %s.",
			data.OwnerAddress,
			data.Allowance.Formatted,
			data.Allowance.Asset.Address,
			data.SpenderAddress,
			result.ChainID,
			result.Source,
			formatObservedAt(result.ObservedAt),
		), nil
	}

	return fmt.Sprintf(
		"Owner %s has granted spender %s an allowance of %s tokens "+
			"for contract %s on Avalanche Fuji (chain ID %d). "+
			"This is not the wallet balance and does not guarantee a transfer will succeed. "+
			"Source: %s, observed at %s.",
		data.OwnerAddress,
		data.SpenderAddress,
		data.Allowance.Formatted,
		data.Allowance.Asset.Address,
		result.ChainID,
		result.Source,
		formatObservedAt(result.ObservedAt),
	), nil
}

var _ LLMClient = (*DemoLLMClient)(nil)
