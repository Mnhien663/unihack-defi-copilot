package ai

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestDemoLLMSelectsToolFromUserMessage(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		message      string
		expectedTool string
	}{
		{
			name:         "native balance",
			message:      "Show my AVAX balance",
			expectedTool: demoNativeBalanceToolName,
		},
		{
			name:         "ERC20 balances",
			message:      "List my ERC-20 token balances",
			expectedTool: demoTokenBalancesToolName,
		},
		{
			name:         "Vietnamese token balances",
			message:      "Hãy liệt kê các token trong ví của tôi",
			expectedTool: demoTokenBalancesToolName,
		},
	}

	for _, testCase := range testCases {
		testCase := testCase

		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			client := NewDemoLLMClient()

			response, err := client.Complete(
				context.Background(),
				LLMRequest{
					Messages: []Message{
						{
							Role:    RoleUser,
							Content: testCase.message,
						},
					},
					Tools: []ToolDefinition{
						{
							Name: demoNativeBalanceToolName,
						},
						{
							Name: demoTokenBalancesToolName,
						},
					},
					WalletAddress: "0x0000000000000000000000000000000000000001",
					ChainID:       FujiChainID,
					Locale:        "en",
				},
			)
			if err != nil {
				t.Fatalf("Complete() error = %v", err)
			}

			if len(response.ToolCalls) != 1 {
				t.Fatalf(
					"tool call count = %d, want 1",
					len(response.ToolCalls),
				)
			}

			if response.ToolCalls[0].Name != testCase.expectedTool {
				t.Fatalf(
					"tool name = %q, want %q",
					response.ToolCalls[0].Name,
					testCase.expectedTool,
				)
			}
		})
	}
}

func TestDemoLLMNativeFinalResponseIncludesProvenance(
	t *testing.T,
) {
	t.Parallel()

	observedAt := time.Date(
		2026,
		time.November,
		2,
		14,
		30,
		0,
		0,
		time.UTC,
	)

	data := json.RawMessage(`{
		"balance": {
			"asset": {
				"symbol": "AVAX"
			},
			"raw": "2181886544100000121",
			"formatted": "2.181886544100000121"
		}
	}`)

	result := ToolResult{
		Data:       data,
		Source:     "mock_chain",
		ChainID:    FujiChainID,
		ObservedAt: observedAt,
	}

	content := encodeDemoToolResult(t, result)

	response, err := NewDemoLLMClient().finalResponse(
		"en",
		Message{
			Role:       RoleTool,
			Name:       demoNativeBalanceToolName,
			ToolCallID: "demo-call-001",
			Content:    content,
		},
	)
	if err != nil {
		t.Fatalf("finalResponse() error = %v", err)
	}

	expectedParts := []string{
		"2.181886544100000121 AVAX",
		"Avalanche Fuji",
		"43113",
		"mock_chain",
		"2026-11-02T14:30:00Z",
	}

	for _, expectedPart := range expectedParts {
		if !strings.Contains(
			response.Message.Content,
			expectedPart,
		) {
			t.Errorf(
				"response %q does not contain %q",
				response.Message.Content,
				expectedPart,
			)
		}
	}
}

func TestDemoLLMTokenFinalResponseUsesValidVietnamese(
	t *testing.T,
) {
	t.Parallel()

	result := ToolResult{
		Data: json.RawMessage(`{
			"balances": [
				{
					"name": "USD Coin",
					"amount": {
						"asset": {
							"symbol": "USDC"
						},
						"raw": "12340000",
						"formatted": "12.34"
					}
				}
			]
		}`),
		Source:  "mock_chain",
		ChainID: FujiChainID,
		ObservedAt: time.Date(
			2026,
			time.November,
			2,
			15,
			0,
			0,
			0,
			time.UTC,
		),
	}

	response, err := NewDemoLLMClient().finalResponse(
		"vi",
		Message{
			Role:       RoleTool,
			Name:       demoTokenBalancesToolName,
			ToolCallID: "demo-call-001",
			Content:    encodeDemoToolResult(t, result),
		},
	)
	if err != nil {
		t.Fatalf("finalResponse() error = %v", err)
	}

	expectedParts := []string{
		"Các số dư ERC-20",
		"12.34 USDC",
		"Avalanche Fuji",
		"mock_chain",
	}

	for _, expectedPart := range expectedParts {
		if !strings.Contains(
			response.Message.Content,
			expectedPart,
		) {
			t.Errorf(
				"response %q does not contain %q",
				response.Message.Content,
				expectedPart,
			)
		}
	}

	invalidEncodingMarkers := []string{
		"Ã",
		"Ä",
		"á»",
	}

	for _, marker := range invalidEncodingMarkers {
		if strings.Contains(response.Message.Content, marker) {
			t.Errorf(
				"response contains invalid encoding marker %q: %q",
				marker,
				response.Message.Content,
			)
		}
	}
}

func encodeDemoToolResult(
	t *testing.T,
	result ToolResult,
) string {
	t.Helper()

	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	return string(encoded)
}
