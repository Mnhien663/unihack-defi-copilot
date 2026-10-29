package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

var (
	ErrDemoLLMNoMessages = errors.New("demo LLM received no messages")
	ErrDemoLLMNoTools    = errors.New("demo LLM received no tools")
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
				Name:      request.Tools[0].Name,
				Arguments: arguments,
			},
		},
	}, nil
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

	var balanceData struct {
		Balance struct {
			Raw string `json:"raw"`
		} `json:"balance"`
	}

	if err := json.Unmarshal(
		result.Data,
		&balanceData,
	); err != nil {
		return LLMResponse{}, fmt.Errorf(
			"decode demo balance data: %w",
			err,
		)
	}

	content := fmt.Sprintf(
		"Mock mode: your native balance is %s Wei.",
		balanceData.Balance.Raw,
	)

	if locale == "vi" {
		content = fmt.Sprintf(
			"Chế độ mock: số dư native của ví là %s Wei.",
			balanceData.Balance.Raw,
		)
	}

	return LLMResponse{
		Message: Message{
			Role:    RoleAssistant,
			Content: content,
		},
	}, nil
}

var _ LLMClient = (*DemoLLMClient)(nil)
