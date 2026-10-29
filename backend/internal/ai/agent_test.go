package ai

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

type mockToolRegistry struct {
	definitions []ToolDefinition
	result      ToolResult
	err         error
	callCount   int
}

func (m *mockToolRegistry) Definitions() []ToolDefinition {
	return m.definitions
}

func (m *mockToolRegistry) Execute(
	ctx context.Context,
	name string,
	arguments json.RawMessage,
) (ToolResult, error) {
	m.callCount++

	return m.result, m.err
}

func TestAgentRunsToolLoop(t *testing.T) {
	blockNumber := uint64(60034644)

	registry := &mockToolRegistry{
		definitions: []ToolDefinition{
			{
				Name:        "get_native_balance",
				Description: "Read native AVAX balance",
				InputSchema: json.RawMessage(`{"type":"object"}`),
			},
		},
		result: ToolResult{
			Data: json.RawMessage(`{
				"balance_avax": "2.181886544100000121"
			}`),
			Source:      "mock_chain",
			ChainID:     FujiChainID,
			BlockNumber: &blockNumber,
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
		},
	}

	llm := NewMockLLMClient(
		MockLLMStep{
			Response: LLMResponse{
				Message: Message{
					Role: RoleAssistant,
				},
				ToolCalls: []ToolCall{
					{
						ID:   "call-001",
						Name: "get_native_balance",
						Arguments: json.RawMessage(`{
							"wallet_address":
							"0x0000000000000000000000000000000000000001",
							"chain_id": 43113
						}`),
					},
				},
			},
		},
		MockLLMStep{
			Response: LLMResponse{
				Message: Message{
					Role:    RoleAssistant,
					Content: "Your balance is 2.181886544100000121 AVAX.",
				},
			},
		},
	)

	memory := NewInMemoryStore()

	policies := NewStaticPolicyEngine(
		[]string{
			"get_native_balance",
		},
		nil,
	)

	agent, err := NewAgent(
		llm,
		registry,
		memory,
		policies,
	)
	if err != nil {
		t.Fatalf("NewAgent() returned error: %v", err)
	}

	output, err := agent.Run(
		context.Background(),
		AgentInput{
			SessionID:     "session-001",
			WalletAddress: testWalletAddress,
			ChainID:       FujiChainID,
			Message:       "Show my AVAX balance",
			Locale:        "en",
		},
	)
	if err != nil {
		t.Fatalf("Run() returned error: %v", err)
	}

	if output.Message != "Your balance is 2.181886544100000121 AVAX." {
		t.Fatalf(
			"output message = %q, want final mock response",
			output.Message,
		)
	}

	if len(output.ToolExecutions) != 1 {
		t.Fatalf(
			"tool executions = %d, want 1",
			len(output.ToolExecutions),
		)
	}

	if registry.callCount != 1 {
		t.Fatalf(
			"registry call count = %d, want 1",
			registry.callCount,
		)
	}

	requests := llm.RecordedRequests()

	if len(requests) != 2 {
		t.Fatalf(
			"LLM request count = %d, want 2",
			len(requests),
		)
	}

	secondMessages := requests[1].Messages
	lastMessage := secondMessages[len(secondMessages)-1]

	if lastMessage.Role != RoleTool {
		t.Fatalf(
			"last second-request role = %q, want tool",
			lastMessage.Role,
		)
	}

	stored, err := memory.Load(
		context.Background(),
		"session-001",
	)
	if err != nil {
		t.Fatalf("memory Load() returned error: %v", err)
	}

	if len(stored) != 2 {
		t.Fatalf(
			"stored messages = %d, want user and assistant",
			len(stored),
		)
	}
}

func TestAgentRejectsToolDeniedByPolicy(t *testing.T) {
	registry := &mockToolRegistry{}

	llm := NewMockLLMClient(
		MockLLMStep{
			Response: LLMResponse{
				ToolCalls: []ToolCall{
					{
						ID:        "call-001",
						Name:      "delete_everything",
						Arguments: json.RawMessage(`{}`),
					},
				},
			},
		},
	)

	agent, err := NewAgent(
		llm,
		registry,
		NewInMemoryStore(),
		NewStaticPolicyEngine(nil, nil),
	)
	if err != nil {
		t.Fatalf("NewAgent() returned error: %v", err)
	}

	_, err = agent.Run(
		context.Background(),
		AgentInput{
			SessionID:     "session-001",
			WalletAddress: testWalletAddress,
			ChainID:       FujiChainID,
			Message:       "Run an unsafe tool",
			Locale:        "en",
		},
	)

	if !errors.Is(err, ErrPolicyDenied) {
		t.Fatalf(
			"Run() error = %v, want ErrPolicyDenied",
			err,
		)
	}

	if registry.callCount != 0 {
		t.Fatalf(
			"registry was called %d time(s), want 0",
			registry.callCount,
		)
	}
}

func TestAgentRejectsEmptyFinalResponse(t *testing.T) {
	llm := NewMockLLMClient(
		MockLLMStep{
			Response: LLMResponse{},
		},
	)

	agent, err := NewAgent(
		llm,
		&mockToolRegistry{},
		NewInMemoryStore(),
		NewStaticPolicyEngine(nil, nil),
	)
	if err != nil {
		t.Fatalf("NewAgent() returned error: %v", err)
	}

	_, err = agent.Run(
		context.Background(),
		AgentInput{
			SessionID:     "session-001",
			WalletAddress: testWalletAddress,
			ChainID:       FujiChainID,
			Message:       "Hello",
			Locale:        "en",
		},
	)

	if !errors.Is(err, ErrEmptyLLMResponse) {
		t.Fatalf(
			"Run() error = %v, want ErrEmptyLLMResponse",
			err,
		)
	}
}
