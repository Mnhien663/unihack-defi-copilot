package ai

import (
	"encoding/json"
	"errors"
	"testing"

	"google.golang.org/genai"
)

func TestToGeminiContentsPreservesToolProtocol(t *testing.T) {
	messages := []Message{
		{
			Role:    RoleSystem,
			Content: "Use tools for current data.",
		},
		{
			Role:    RoleUser,
			Content: "Show my balance.",
		},
		{
			Role: RoleAssistant,
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
			Name:       "get_native_balance",
			ToolCallID: "call-balance-1",
			Content:    `{"data":{"balance":"1"}}`,
		},
	}

	contents, instructions, err := toGeminiContents(messages)
	if err != nil {
		t.Fatalf("map Gemini contents: %v", err)
	}

	if len(instructions) != 1 {
		t.Fatalf(
			"expected one system instruction, got %d",
			len(instructions),
		)
	}

	if instructions[0] != "Use tools for current data." {
		t.Fatalf(
			"unexpected system instruction: %q",
			instructions[0],
		)
	}

	if len(contents) != 3 {
		t.Fatalf(
			"expected three contents, got %d",
			len(contents),
		)
	}

	if contents[0].Role != string(genai.RoleUser) {
		t.Fatalf(
			"expected user role, got %q",
			contents[0].Role,
		)
	}

	if contents[1].Role != string(genai.RoleModel) {
		t.Fatalf(
			"expected model role, got %q",
			contents[1].Role,
		)
	}

	functionCall := contents[1].Parts[0].FunctionCall
	if functionCall == nil {
		t.Fatal("expected function call part")
	}

	if functionCall.Name != "get_native_balance" {
		t.Fatalf(
			"unexpected function name: %q",
			functionCall.Name,
		)
	}

	functionResponse := contents[2].Parts[0].FunctionResponse
	if functionResponse == nil {
		t.Fatal("expected function response part")
	}

	if functionResponse.ID != "call-balance-1" {
		t.Fatalf(
			"unexpected response ID: %q",
			functionResponse.ID,
		)
	}
}

func TestToGeminiContentsRejectsInvalidToolResult(t *testing.T) {
	_, _, err := toGeminiContents([]Message{
		{
			Role:    RoleTool,
			Name:    "get_native_balance",
			Content: `{"data":`,
		},
	})

	if !errors.Is(err, ErrInvalidGeminiMessage) {
		t.Fatalf(
			"expected ErrInvalidGeminiMessage, got %v",
			err,
		)
	}
}

func TestToGeminiToolsPreservesJSONSchema(t *testing.T) {
	tools, err := toGeminiTools([]ToolDefinition{
		{
			Name:        "get_native_balance",
			Description: "Read native AVAX balance.",
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
	})
	if err != nil {
		t.Fatalf("map Gemini tools: %v", err)
	}

	if len(tools) != 1 {
		t.Fatalf("expected one tool, got %d", len(tools))
	}

	declarations := tools[0].FunctionDeclarations
	if len(declarations) != 1 {
		t.Fatalf(
			"expected one declaration, got %d",
			len(declarations),
		)
	}

	declaration := declarations[0]

	if declaration.Name != "get_native_balance" {
		t.Fatalf(
			"unexpected declaration name: %q",
			declaration.Name,
		)
	}

	schema, ok := declaration.ParametersJsonSchema.(map[string]any)
	if !ok {
		t.Fatalf(
			"expected map schema, got %T",
			declaration.ParametersJsonSchema,
		)
	}

	if _, exists := schema["$schema"]; exists {
		t.Fatal("expected schema dialect to be removed")
	}

	if schema["additionalProperties"] != false {
		t.Fatalf(
			"expected additionalProperties=false, got %v",
			schema["additionalProperties"],
		)
	}

	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf(
			"expected properties map, got %T",
			schema["properties"],
		)
	}

	chainSchema, ok := properties["chain_id"].(map[string]any)
	if !ok {
		t.Fatalf(
			"expected chain_id schema map, got %T",
			properties["chain_id"],
		)
	}

	if _, exists := chainSchema["const"]; exists {
		t.Fatal("expected const to be removed")
	}

	enumValues, ok := chainSchema["enum"].([]any)
	if !ok || len(enumValues) != 1 {
		t.Fatalf(
			"expected one enum value, got %#v",
			chainSchema["enum"],
		)
	}

	if enumValues[0] != float64(43113) {
		t.Fatalf(
			"expected enum value 43113, got %#v",
			enumValues[0],
		)
	}
}

func TestToGeminiToolsRejectsInvalidSchema(t *testing.T) {
	_, err := toGeminiTools([]ToolDefinition{
		{
			Name:        "broken_tool",
			InputSchema: json.RawMessage(`{"type":`),
		},
	})

	if !errors.Is(err, ErrInvalidGeminiSchema) {
		t.Fatalf(
			"expected ErrInvalidGeminiSchema, got %v",
			err,
		)
	}
}

func TestFromGeminiFunctionCallsCreatesFallbackID(t *testing.T) {
	result, err := fromGeminiFunctionCalls(
		[]*genai.FunctionCall{
			{
				Name: "get_native_balance",
				Args: map[string]any{
					"wallet_address": "0x0000000000000000000000000000000000000001",
					"chain_id":       float64(43113),
				},
			},
		},
	)
	if err != nil {
		t.Fatalf("map Gemini calls: %v", err)
	}

	if len(result) != 1 {
		t.Fatalf("expected one call, got %d", len(result))
	}

	if result[0].ID != "gemini-call-1" {
		t.Fatalf(
			"expected fallback ID, got %q",
			result[0].ID,
		)
	}

	if result[0].Name != "get_native_balance" {
		t.Fatalf(
			"unexpected function name: %q",
			result[0].Name,
		)
	}

	if !json.Valid(result[0].Arguments) {
		t.Fatalf(
			"invalid arguments: %s",
			result[0].Arguments,
		)
	}
}

func TestFromGeminiFunctionCallsRejectsMissingName(t *testing.T) {
	_, err := fromGeminiFunctionCalls(
		[]*genai.FunctionCall{
			{
				Args: map[string]any{},
			},
		},
	)

	if !errors.Is(err, ErrInvalidGeminiToolCall) {
		t.Fatalf(
			"expected ErrInvalidGeminiToolCall, got %v",
			err,
		)
	}
}

func TestFromGeminiResponsePreservesThoughtSignature(t *testing.T) {
	expectedSignature := []byte("encrypted-thought-signature")

	response := &genai.GenerateContentResponse{
		Candidates: []*genai.Candidate{
			{
				Content: &genai.Content{
					Role: genai.RoleModel,
					Parts: []*genai.Part{
						{
							FunctionCall: &genai.FunctionCall{
								ID:   "call-1",
								Name: "get_native_balance",
								Args: map[string]any{
									"wallet_address": "0x0000000000000000000000000000000000000001",
									"chain_id":       float64(43113),
								},
							},
							ThoughtSignature: expectedSignature,
						},
					},
				},
			},
		},
	}

	toolCalls, err := fromGeminiResponseFunctionCalls(response)
	if err != nil {
		t.Fatalf("fromGeminiResponseFunctionCalls returned error: %v", err)
	}

	if len(toolCalls) != 1 {
		t.Fatalf("got %d tool calls, want 1", len(toolCalls))
	}

	if string(toolCalls[0].ProviderState) != string(expectedSignature) {
		t.Fatalf(
			"ProviderState = %q, want %q",
			toolCalls[0].ProviderState,
			expectedSignature,
		)
	}
}

func TestToGeminiContentsRestoresThoughtSignature(t *testing.T) {
	expectedSignature := []byte("encrypted-thought-signature")

	messages := []Message{
		{
			Role: RoleAssistant,
			ToolCalls: []ToolCall{
				{
					ID:   "call-1",
					Name: "get_native_balance",
					Arguments: json.RawMessage(
						`{"wallet_address":"0x0000000000000000000000000000000000000001","chain_id":43113}`,
					),
					ProviderState: expectedSignature,
				},
			},
		},
	}

	contents, _, err := toGeminiContents(messages)
	if err != nil {
		t.Fatalf("toGeminiContents returned error: %v", err)
	}

	if len(contents) != 1 || len(contents[0].Parts) != 1 {
		t.Fatalf("unexpected Gemini contents structure")
	}

	actualSignature := contents[0].Parts[0].ThoughtSignature

	if string(actualSignature) != string(expectedSignature) {
		t.Fatalf(
			"ThoughtSignature = %q, want %q",
			actualSignature,
			expectedSignature,
		)
	}
}
