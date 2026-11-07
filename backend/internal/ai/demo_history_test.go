package ai

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestDemoLLMSelectsTransactionHistory(t *testing.T) {
	for _, message := range []string{
		"Show my transaction history",
		"Show USDC transaction history",
		"Xem lịch sử giao dịch của tôi",
	} {
		t.Run(message, func(t *testing.T) {
			got := selectDemoTool(message)
			if got != demoTransactionsToolName {
				t.Fatalf("tool = %q; want %q", got, demoTransactionsToolName)
			}
		})
	}
}

func TestDemoLLMHistoryResponseIncludesProvenance(t *testing.T) {
	result := ToolResult{
		Data: json.RawMessage(`{
			"transactions": [{
				"nativeTransaction": {
					"txHash": "0xfixture",
					"txStatus": "1"
				}
			}],
			"next_page_token": "next-cursor"
		}`),
		Source:     "mock_chain",
		ChainID:    43113,
		ObservedAt: time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC),
	}

	for _, locale := range []string{"en", "vi"} {
		t.Run(locale, func(t *testing.T) {
			content, err := demoTransactionResponse(locale, result)
			if err != nil {
				t.Fatal(err)
			}

			for _, expected := range []string{
				"0xfixture",
				"43113",
				"mock_chain",
				"2026-11-05T12:00:00Z",
			} {
				if !strings.Contains(content, expected) {
					t.Fatalf("missing %q in %q", expected, content)
				}
			}

			expected := "Another page is available"
			if locale == "vi" {
				expected = "Có trang tiếp theo"
			}
			if !strings.Contains(content, expected) {
				t.Fatalf("missing pagination notice: %q", content)
			}
		})
	}
}

func TestDemoLLMEmptyHistoryIsNotGenericToolError(t *testing.T) {
	result := ToolResult{
		Data:       json.RawMessage(`{"transactions":[]}`),
		Source:     "mock_chain",
		ChainID:    43113,
		ObservedAt: time.Now().UTC(),
	}

	// Decode through JSON to avoid depending on the ToolError type name.
	if err := json.Unmarshal(
		[]byte(`{
			"error": {
				"code": "EMPTY_HISTORY",
				"message": "no transactions returned"
			}
		}`),
		&result,
	); err != nil {
		t.Fatal(err)
	}

	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}

	client := NewDemoLLMClient()
	response, err := client.finalResponse("en", Message{
		Role:    RoleTool,
		Name:    demoTransactionsToolName,
		Content: string(encoded),
	})
	if err != nil {
		t.Fatal(err)
	}

	content := response.Message.Content
	if strings.Contains(content, "Mock tool error") ||
		!strings.Contains(content, "No transactions were returned for this page") {
		t.Fatalf("unexpected empty history response: %q", content)
	}
}
