package ai

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	openai "github.com/openai/openai-go/v3"
)

func TestToOpenAIMessagesPreservesToolProtocol(t *testing.T) {
	messages := []Message{
		{
			Role:    RoleSystem,
			Content: "Use tools for current blockchain data.",
		},
		{
			Role:    RoleUser,
			Content: "Show my balance.",
		},
		{
			Role:    RoleAssistant,
			Content: "",
			ToolCalls: []ToolCall{
				{
					ID:   "call-balance-1",
					Name: "get_native_balance",
					Arguments: json.RawMessage(
						`{"wallet_address":"0x0000000000000000000000000000000000000001","chain_id":43113}`,
					),
				},
			},
		},
		{
			Role:       RoleTool,
			Content:    `{"data":{"balance":"1"}}`,
			Name:       "get_native_balance",
			ToolCallID: "call-balance-1",
		},
	}

	result, err := toOpenAIMessages(messages)
	if err != nil {
		t.Fatalf("map OpenAI messages: %v", err)
	}

	if len(result) != len(messages) {
		t.Fatalf(
			"expected %d messages, got %d",
			len(messages),
			len(result),
		)
	}

	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("encode OpenAI messages: %v", err)
	}

	output := string(encoded)

	expectedFragments := []string{
		`"role":"system"`,
		`"role":"user"`,
		`"role":"assistant"`,
		`"tool_calls"`,
		`"id":"call-balance-1"`,
		`"name":"get_native_balance"`,
		`"role":"tool"`,
		`"tool_call_id":"call-balance-1"`,
	}

	for _, fragment := range expectedFragments {
		if !strings.Contains(output, fragment) {
			t.Fatalf(
				"expected encoded messages to contain %q; got %s",
				fragment,
				output,
			)
		}
	}
}

func TestToOpenAIMessagesRejectsToolWithoutCallID(t *testing.T) {
	_, err := toOpenAIMessages([]Message{
		{
			Role:    RoleTool,
			Content: `{}`,
		},
	})

	if !errors.Is(err, ErrMissingToolCallID) {
		t.Fatalf(
			"expected ErrMissingToolCallID, got %v",
			err,
		)
	}
}

func TestToOpenAIToolsUsesStrictSchema(t *testing.T) {
	definitions := []ToolDefinition{
		{
			Name:        "get_native_balance",
			Description: "Read the native AVAX balance.",
			InputSchema: json.RawMessage(`{
				"$schema": "https://json-schema.org/draft/2020-12/schema",
				"type": "object",
				"properties": {
					"wallet_address": {
						"type": "string"
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
		},
	}

	result, err := toOpenAITools(definitions)
	if err != nil {
		t.Fatalf("map OpenAI tools: %v", err)
	}

	if len(result) != 1 {
		t.Fatalf("expected one tool, got %d", len(result))
	}

	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("encode OpenAI tools: %v", err)
	}

	output := string(encoded)

	expectedFragments := []string{
		`"type":"function"`,
		`"name":"get_native_balance"`,
		`"strict":true`,
		`"additionalProperties":false`,
	}

	for _, fragment := range expectedFragments {
		if !strings.Contains(output, fragment) {
			t.Fatalf(
				"expected encoded tool to contain %q; got %s",
				fragment,
				output,
			)
		}
	}

	if strings.Contains(output, `"$schema"`) {
		t.Fatalf(
			"expected schema dialect to be removed; got %s",
			output,
		)
	}
}

func TestToOpenAIToolsRejectsInvalidSchema(t *testing.T) {
	_, err := toOpenAITools([]ToolDefinition{
		{
			Name:        "broken_tool",
			Description: "Invalid test tool.",
			InputSchema: json.RawMessage(`{"type":`),
		},
	})

	if !errors.Is(err, ErrInvalidToolSchema) {
		t.Fatalf(
			"expected ErrInvalidToolSchema, got %v",
			err,
		)
	}
}

func TestFromOpenAIToolCalls(t *testing.T) {
	var providerCalls []openai.ChatCompletionMessageToolCallUnion

	err := json.Unmarshal(
		[]byte(`[{
			"id": "call-balance-1",
			"type": "function",
			"function": {
				"name": "get_native_balance",
				"arguments": "{\"wallet_address\":\"0x0000000000000000000000000000000000000001\",\"chain_id\":43113}"
			}
		}]`),
		&providerCalls,
	)
	if err != nil {
		t.Fatalf("decode provider tool calls: %v", err)
	}

	result, err := fromOpenAIToolCalls(providerCalls)
	if err != nil {
		t.Fatalf("map provider tool calls: %v", err)
	}

	if len(result) != 1 {
		t.Fatalf("expected one tool call, got %d", len(result))
	}

	call := result[0]

	if call.ID != "call-balance-1" {
		t.Fatalf("unexpected call ID: %q", call.ID)
	}

	if call.Name != "get_native_balance" {
		t.Fatalf("unexpected tool name: %q", call.Name)
	}

	if !json.Valid(call.Arguments) {
		t.Fatalf(
			"expected valid arguments, got %s",
			call.Arguments,
		)
	}
}

func TestFromOpenAIToolCallsRejectsInvalidArguments(t *testing.T) {
	var providerCalls []openai.ChatCompletionMessageToolCallUnion

	err := json.Unmarshal(
		[]byte(`[{
			"id": "call-balance-1",
			"type": "function",
			"function": {
				"name": "get_native_balance",
				"arguments": "{\"wallet_address\":"
			}
		}]`),
		&providerCalls,
	)
	if err != nil {
		t.Fatalf("decode provider tool calls: %v", err)
	}

	_, err = fromOpenAIToolCalls(providerCalls)

	if !errors.Is(err, ErrInvalidOpenAIToolCall) {
		t.Fatalf(
			"expected ErrInvalidOpenAIToolCall, got %v",
			err,
		)
	}
}
