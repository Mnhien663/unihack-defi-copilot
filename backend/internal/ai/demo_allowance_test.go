package ai

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestDemoAllowanceAsksForMissingSpender(t *testing.T) {
	client := NewDemoLLMClient()

	response, err := client.Complete(context.Background(), LLMRequest{
		WalletAddress: "0x0000000000000000000000000000000000000001",
		ChainID:       43113,
		Locale:        "en",
		Messages: []Message{
			{
				Role: RoleUser,
				Content: "Show allowance " +
					"token_address=0xcCCb83Fbe349fAebDE74Ce633D25B282E6FDd21A",
			},
		},
		Tools: []ToolDefinition{
			{Name: demoAllowanceToolName},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(response.ToolCalls) != 0 {
		t.Fatal("missing spender must not produce a tool call")
	}
	if !strings.Contains(response.Message.Content, "spender_address") {
		t.Fatalf("expected clarification, got %q", response.Message.Content)
	}
}

func TestDemoAllowanceBuildsExplicitArguments(t *testing.T) {
	const (
		wallet  = "0x0000000000000000000000000000000000000001"
		token   = "0xcCCb83Fbe349fAebDE74Ce633D25B282E6FDd21A"
		spender = "0x0000000000000000000000000000000000000002"
	)

	client := NewDemoLLMClient()
	response, err := client.Complete(context.Background(), LLMRequest{
		WalletAddress: wallet,
		ChainID:       43113,
		Locale:        "en",
		Messages: []Message{
			{
				Role: RoleUser,
				Content: "Show DDEMO allowance token_address=" + token +
					" spender_address=" + spender,
			},
		},
		Tools: []ToolDefinition{
			{Name: demoAllowanceToolName},
			{Name: demoTokenBalancesToolName},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(response.ToolCalls) != 1 {
		t.Fatalf("tool calls = %d; want 1", len(response.ToolCalls))
	}

	call := response.ToolCalls[0]
	if call.Name != demoAllowanceToolName {
		t.Fatalf("wrong tool: %s", call.Name)
	}

	var arguments struct {
		WalletAddress  string `json:"wallet_address"`
		ChainID        uint64 `json:"chain_id"`
		TokenAddress   string `json:"token_address"`
		SpenderAddress string `json:"spender_address"`
	}
	if err := json.Unmarshal(call.Arguments, &arguments); err != nil {
		t.Fatal(err)
	}

	if arguments.WalletAddress != wallet ||
		arguments.ChainID != 43113 ||
		arguments.TokenAddress != token ||
		arguments.SpenderAddress != spender {
		t.Fatalf("unexpected arguments: %+v", arguments)
	}
}

func TestDemoAllowanceResponseDistinguishesWalletBalance(t *testing.T) {
	result := ToolResult{
		Data: json.RawMessage(`{
			"owner_address":"0x0000000000000000000000000000000000000001",
			"spender_address":"0x0000000000000000000000000000000000000002",
			"allowance":{
				"asset":{
					"address":"0xcCCb83Fbe349fAebDE74Ce633D25B282E6FDd21A"
				},
				"raw":"1500000000000000000",
				"formatted":"1.5"
			}
		}`),
		Source:     "mock_chain",
		ChainID:    43113,
		ObservedAt: time.Date(2026, 11, 5, 13, 0, 0, 0, time.UTC),
	}

	for _, locale := range []string{"en", "vi"} {
		t.Run(locale, func(t *testing.T) {
			content, err := demoAllowanceResponse(locale, result)
			if err != nil {
				t.Fatal(err)
			}

			for _, expected := range []string{
				"1.5",
				"43113",
				"mock_chain",
				"2026-11-05T13:00:00Z",
			} {
				if !strings.Contains(content, expected) {
					t.Fatalf("missing %q in %q", expected, content)
				}
			}

			disclaimer := "not the wallet balance"
			if locale == "vi" {
				disclaimer = "không phải số dư ví"
			}
			if !strings.Contains(content, disclaimer) {
				t.Fatalf("missing balance disclaimer: %q", content)
			}
		})
	}
}
