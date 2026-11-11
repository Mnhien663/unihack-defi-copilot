package tools

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"unihack-backend/internal/ai"
)

type mockTool struct {
	definition ai.ToolDefinition
	result     ai.ToolResult
	err        error
}

func (m *mockTool) Definition() ai.ToolDefinition {
	return m.definition
}

func (m *mockTool) Execute(
	ctx context.Context,
	arguments json.RawMessage,
) (ai.ToolResult, error) {
	return m.result, m.err
}

func newMockTool(name string) *mockTool {
	return &mockTool{
		definition: ai.ToolDefinition{
			Name:        name,
			Description: "Mock tool for registry tests",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"additionalProperties": false
			}`),
		},
		result: ai.ToolResult{
			Data: json.RawMessage(`{
				"status": "ok"
			}`),
			Source:  "mock",
			ChainID: 43113,
			ObservedAt: time.Date(
				2026,
				time.October,
				28,
				0,
				0,
				0,
				0,
				time.UTC,
			),
			Error: nil,
		},
	}
}

func TestRegistryRegisterAndListDefinitions(t *testing.T) {
	registry := NewRegistry()

	toolB := newMockTool("tool_b")
	toolA := newMockTool("tool_a")

	if err := registry.Register(toolB); err != nil {
		t.Fatalf("Register(toolB) returned error: %v", err)
	}

	if err := registry.Register(toolA); err != nil {
		t.Fatalf("Register(toolA) returned error: %v", err)
	}

	definitions := registry.Definitions()

	if len(definitions) != 2 {
		t.Fatalf(
			"Definitions() returned %d definitions, want 2",
			len(definitions),
		)
	}

	if definitions[0].Name != "tool_a" {
		t.Fatalf(
			"first tool = %q, want tool_a",
			definitions[0].Name,
		)
	}

	if definitions[1].Name != "tool_b" {
		t.Fatalf(
			"second tool = %q, want tool_b",
			definitions[1].Name,
		)
	}
}

func TestRegistryRejectsDuplicateTool(t *testing.T) {
	registry := NewRegistry()
	tool := newMockTool("get_native_balance")

	if err := registry.Register(tool); err != nil {
		t.Fatalf("first Register() returned error: %v", err)
	}

	err := registry.Register(tool)

	if !errors.Is(err, ErrToolAlreadyRegistered) {
		t.Fatalf(
			"second Register() error = %v, want ErrToolAlreadyRegistered",
			err,
		)
	}
}

func TestRegistryRejectsInvalidSchema(t *testing.T) {
	registry := NewRegistry()

	tool := newMockTool("invalid_schema")
	tool.definition.InputSchema = json.RawMessage(`{"type":`)

	err := registry.Register(tool)

	if !errors.Is(err, ErrInvalidToolSchema) {
		t.Fatalf(
			"Register() error = %v, want ErrInvalidToolSchema",
			err,
		)
	}
}

func TestRegistryReturnsUnknownToolError(t *testing.T) {
	registry := NewRegistry()

	_, err := registry.Execute(
		context.Background(),
		"unknown_tool",
		json.RawMessage(`{}`),
	)

	if !errors.Is(err, ErrToolNotFound) {
		t.Fatalf(
			"Execute() error = %v, want ErrToolNotFound",
			err,
		)
	}
}

func TestRegistryExecutesRegisteredTool(t *testing.T) {
	registry := NewRegistry()
	tool := newMockTool("get_native_balance")

	if err := registry.Register(tool); err != nil {
		t.Fatalf("Register() returned error: %v", err)
	}

	result, err := registry.Execute(
		context.Background(),
		"get_native_balance",
		json.RawMessage(`{
			"wallet_address":
			"0x0000000000000000000000000000000000000001"
		}`),
	)
	if err != nil {
		t.Fatalf("Execute() returned error: %v", err)
	}

	if result.Source != "mock" {
		t.Fatalf(
			"result source = %q, want mock",
			result.Source,
		)
	}

	if result.ChainID != 43113 {
		t.Fatalf(
			"result chain ID = %d, want 43113",
			result.ChainID,
		)
	}
}
