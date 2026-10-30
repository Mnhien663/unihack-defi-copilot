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

type blockingLLMClient struct{}

func (blockingLLMClient) Complete(
	ctx context.Context,
	request LLMRequest,
) (LLMResponse, error) {
	<-ctx.Done()

	return LLMResponse{}, ctx.Err()
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

func TestAgentStopsAfterMaximumIterations(t *testing.T) {
	steps := make(
		[]MockLLMStep,
		maxAgentIterations,
	)

	callIDs := []string{
		"call-001",
		"call-002",
		"call-003",
		"call-004",
	}

	for index := range steps {
		steps[index] = MockLLMStep{
			Response: LLMResponse{
				ToolCalls: []ToolCall{
					{
						ID:   callIDs[index],
						Name: "get_native_balance",
						Arguments: json.RawMessage(`{
							"wallet_address":
							"0x0000000000000000000000000000000000000001",
							"chain_id": 43113
						}`),
					},
				},
			},
		}
	}

	llm := NewMockLLMClient(steps...)

	registry := &mockToolRegistry{
		definitions: []ToolDefinition{
			{
				Name:        "get_native_balance",
				Description: "Read native AVAX balance",
				InputSchema: json.RawMessage(`{"type":"object"}`),
			},
		},
		result: ToolResult{
			Data:       json.RawMessage(`{"balance":"1"}`),
			Source:     "mock_chain",
			ChainID:    FujiChainID,
			ObservedAt: time.Now().UTC(),
		},
	}

	agent, err := NewAgent(
		llm,
		registry,
		NewInMemoryStore(),
		NewStaticPolicyEngine(
			[]string{"get_native_balance"},
			nil,
		),
	)
	if err != nil {
		t.Fatalf("NewAgent() returned error: %v", err)
	}

	_, err = agent.Run(
		context.Background(),
		AgentInput{
			SessionID:     "infinite-loop-session",
			WalletAddress: testWalletAddress,
			ChainID:       FujiChainID,
			Message:       "Keep checking forever",
			Locale:        "en",
		},
	)

	if !errors.Is(err, ErrMaxAgentIterations) {
		t.Fatalf(
			"Run() error = %v, want ErrMaxAgentIterations",
			err,
		)
	}

	if registry.callCount != maxAgentIterations {
		t.Fatalf(
			"tool execution count = %d, want %d",
			registry.callCount,
			maxAgentIterations,
		)
	}

	if len(llm.RecordedRequests()) != maxAgentIterations {
		t.Fatalf(
			"LLM request count = %d, want %d",
			len(llm.RecordedRequests()),
			maxAgentIterations,
		)
	}
}

func TestAgentRejectsToolCallsAboveMaximum(t *testing.T) {
	toolCalls := []ToolCall{
		{
			ID:        "call-001",
			Name:      "get_native_balance",
			Arguments: json.RawMessage(`{}`),
		},
		{
			ID:        "call-002",
			Name:      "get_native_balance",
			Arguments: json.RawMessage(`{}`),
		},
		{
			ID:        "call-003",
			Name:      "get_native_balance",
			Arguments: json.RawMessage(`{}`),
		},
		{
			ID:        "call-004",
			Name:      "get_native_balance",
			Arguments: json.RawMessage(`{}`),
		},
		{
			ID:        "call-005",
			Name:      "get_native_balance",
			Arguments: json.RawMessage(`{}`),
		},
	}

	registry := &mockToolRegistry{}

	agent, err := NewAgent(
		NewMockLLMClient(
			MockLLMStep{
				Response: LLMResponse{
					ToolCalls: toolCalls,
				},
			},
		),
		registry,
		NewInMemoryStore(),
		NewStaticPolicyEngine(
			[]string{"get_native_balance"},
			nil,
		),
	)
	if err != nil {
		t.Fatalf("NewAgent() returned error: %v", err)
	}

	_, err = agent.Run(
		context.Background(),
		AgentInput{
			SessionID:     "too-many-tools-session",
			WalletAddress: testWalletAddress,
			ChainID:       FujiChainID,
			Message:       "Run too many tools",
			Locale:        "en",
		},
	)

	if !errors.Is(err, ErrMaxAgentToolCalls) {
		t.Fatalf(
			"Run() error = %v, want ErrMaxAgentToolCalls",
			err,
		)
	}

	if registry.callCount != 0 {
		t.Fatalf(
			"tool execution count = %d, want 0",
			registry.callCount,
		)
	}
}

func TestAgentRejectsParallelWriteCallsBeforeExecution(t *testing.T) {
	registry := &mockToolRegistry{}

	llm := NewMockLLMClient(
		MockLLMStep{
			Response: LLMResponse{
				ToolCalls: []ToolCall{
					{
						ID:        "write-call-001",
						Name:      "mock_transfer",
						Arguments: json.RawMessage(`{"amount":"1"}`),
					},
					{
						ID:        "write-call-002",
						Name:      "mock_transfer",
						Arguments: json.RawMessage(`{"amount":"2"}`),
					},
				},
			},
		},
	)

	agent, err := NewAgent(
		llm,
		registry,
		NewInMemoryStore(),
		NewStaticPolicyEngine(
			nil,
			[]string{"mock_transfer"},
		),
	)
	if err != nil {
		t.Fatalf("NewAgent() returned error: %v", err)
	}

	_, err = agent.Run(
		context.Background(),
		AgentInput{
			SessionID:     "parallel-write-session",
			WalletAddress: testWalletAddress,
			ChainID:       FujiChainID,
			Message:       "Make two transfers",
			Locale:        "en",
		},
	)

	if !errors.Is(err, ErrParallelWriteCalls) {
		t.Fatalf(
			"Run() error = %v, want ErrParallelWriteCalls",
			err,
		)
	}

	if registry.callCount != 0 {
		t.Fatalf(
			"tool execution count = %d, want 0",
			registry.callCount,
		)
	}
}

func TestAgentRejectsMissingToolCallIDBeforeExecution(t *testing.T) {
	registry := &mockToolRegistry{}

	agent, err := NewAgent(
		NewMockLLMClient(
			MockLLMStep{
				Response: LLMResponse{
					ToolCalls: []ToolCall{
						{
							ID:        "   ",
							Name:      "get_native_balance",
							Arguments: json.RawMessage(`{}`),
						},
					},
				},
			},
		),
		registry,
		NewInMemoryStore(),
		NewStaticPolicyEngine(
			[]string{"get_native_balance"},
			nil,
		),
	)
	if err != nil {
		t.Fatalf("NewAgent() returned error: %v", err)
	}

	_, err = agent.Run(
		context.Background(),
		AgentInput{
			RequestID:     "request-missing-call-id",
			SessionID:     "missing-call-id-session",
			WalletAddress: testWalletAddress,
			ChainID:       FujiChainID,
			Message:       "Check balance",
			Locale:        "en",
		},
	)

	if !errors.Is(err, ErrAgentMissingToolCallID) {
		t.Fatalf(
			"Run() error = %v, want ErrAgentMissingToolCallID",
			err,
		)
	}

	if registry.callCount != 0 {
		t.Fatalf(
			"tool execution count = %d, want 0",
			registry.callCount,
		)
	}
}

func TestAgentRejectsDuplicateToolCallIDBeforeExecution(t *testing.T) {
	registry := &mockToolRegistry{}

	agent, err := NewAgent(
		NewMockLLMClient(
			MockLLMStep{
				Response: LLMResponse{
					ToolCalls: []ToolCall{
						{
							ID:        "duplicate-call",
							Name:      "get_native_balance",
							Arguments: json.RawMessage(`{}`),
						},
						{
							ID:        "duplicate-call",
							Name:      "get_native_balance",
							Arguments: json.RawMessage(`{}`),
						},
					},
				},
			},
		),
		registry,
		NewInMemoryStore(),
		NewStaticPolicyEngine(
			[]string{"get_native_balance"},
			nil,
		),
	)
	if err != nil {
		t.Fatalf("NewAgent() returned error: %v", err)
	}

	_, err = agent.Run(
		context.Background(),
		AgentInput{
			RequestID:     "request-duplicate-call-id",
			SessionID:     "duplicate-call-id-session",
			WalletAddress: testWalletAddress,
			ChainID:       FujiChainID,
			Message:       "Check balance twice",
			Locale:        "en",
		},
	)

	if !errors.Is(err, ErrDuplicateToolCallID) {
		t.Fatalf(
			"Run() error = %v, want ErrDuplicateToolCallID",
			err,
		)
	}

	if registry.callCount != 0 {
		t.Fatalf(
			"tool execution count = %d, want 0",
			registry.callCount,
		)
	}
}

func TestAgentHonorsRequestTimeout(t *testing.T) {
	agent, err := NewAgent(
		blockingLLMClient{},
		&mockToolRegistry{},
		NewInMemoryStore(),
		NewStaticPolicyEngine(nil, nil),
	)
	if err != nil {
		t.Fatalf("NewAgent() returned error: %v", err)
	}

	ctx, cancel := context.WithTimeout(
		context.Background(),
		20*time.Millisecond,
	)
	defer cancel()

	startedAt := time.Now()

	_, err = agent.Run(
		ctx,
		AgentInput{
			RequestID:     "request-timeout-001",
			SessionID:     "timeout-session",
			WalletAddress: testWalletAddress,
			ChainID:       FujiChainID,
			Message:       "This request should time out",
			Locale:        "en",
		},
	)

	elapsed := time.Since(startedAt)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf(
			"Run() error = %v, want context.DeadlineExceeded",
			err,
		)
	}

	if elapsed > time.Second {
		t.Fatalf(
			"agent took %v to honor timeout, want under 1 second",
			elapsed,
		)
	}
}

func TestAgentRunsThreeMockQuestionsThroughToolLoop(t *testing.T) {
	testCases := []struct {
		name          string
		sessionID     string
		question      string
		toolName      string
		toolCallID    string
		arguments     json.RawMessage
		toolData      json.RawMessage
		finalResponse string
	}{
		{
			name:       "native balance",
			sessionID:  "mock-balance-session",
			question:   "What is my AVAX balance?",
			toolName:   "get_native_balance",
			toolCallID: "balance-call-001",
			arguments: json.RawMessage(`{
				"wallet_address":
				"0x0000000000000000000000000000000000000001",
				"chain_id": 43113
			}`),
			toolData: json.RawMessage(`{
				"balance_avax": "2.1818865441"
			}`),
			finalResponse: "Your balance is 2.1818865441 AVAX.",
		},
		{
			name:       "latest block",
			sessionID:  "mock-block-session",
			question:   "What is the latest Fuji block?",
			toolName:   "get_latest_block",
			toolCallID: "block-call-001",
			arguments: json.RawMessage(`{
				"chain_id": 43113
			}`),
			toolData: json.RawMessage(`{
				"block_number": 60034644
			}`),
			finalResponse: "The latest Fuji block is 60034644.",
		},
		{
			name:       "transaction history",
			sessionID:  "mock-history-session",
			question:   "Show my recent transactions.",
			toolName:   "list_transactions",
			toolCallID: "history-call-001",
			arguments: json.RawMessage(`{
				"wallet_address":
				"0x0000000000000000000000000000000000000001",
				"chain_id": 43113
			}`),
			toolData: json.RawMessage(`{
				"transactions": [
					{"hash": "0xmock", "status": "success"}
				]
			}`),
			finalResponse: "You have one recent successful transaction.",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			blockNumber := uint64(60034644)

			registry := &mockToolRegistry{
				definitions: []ToolDefinition{
					{
						Name:        testCase.toolName,
						Description: "Mock read tool",
						InputSchema: json.RawMessage(
							`{"type":"object"}`,
						),
					},
				},
				result: ToolResult{
					Data:        testCase.toolData,
					Source:      "mock_chain",
					ChainID:     FujiChainID,
					BlockNumber: &blockNumber,
					ObservedAt: time.Date(
						2026,
						time.October,
						30,
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
						ToolCalls: []ToolCall{
							{
								ID:        testCase.toolCallID,
								Name:      testCase.toolName,
								Arguments: testCase.arguments,
							},
						},
					},
				},
				MockLLMStep{
					Response: LLMResponse{
						Message: Message{
							Role:    RoleAssistant,
							Content: testCase.finalResponse,
						},
					},
				},
			)

			agent, err := NewAgent(
				llm,
				registry,
				NewInMemoryStore(),
				NewStaticPolicyEngine(
					[]string{testCase.toolName},
					nil,
				),
			)
			if err != nil {
				t.Fatalf(
					"NewAgent() returned error: %v",
					err,
				)
			}

			output, err := agent.Run(
				context.Background(),
				AgentInput{
					RequestID:     "request-" + testCase.sessionID,
					SessionID:     testCase.sessionID,
					WalletAddress: testWalletAddress,
					ChainID:       FujiChainID,
					Message:       testCase.question,
					Locale:        "en",
				},
			)
			if err != nil {
				t.Fatalf("Run() returned error: %v", err)
			}

			if output.Message != testCase.finalResponse {
				t.Fatalf(
					"message = %q, want %q",
					output.Message,
					testCase.finalResponse,
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

			secondRequestMessages := requests[1].Messages
			lastMessage := secondRequestMessages[len(secondRequestMessages)-1]

			if lastMessage.Role != RoleTool {
				t.Fatalf(
					"last message role = %q, want tool",
					lastMessage.Role,
				)
			}

			if lastMessage.ToolCallID != testCase.toolCallID {
				t.Fatalf(
					"tool call ID = %q, want %q",
					lastMessage.ToolCallID,
					testCase.toolCallID,
				)
			}
		})
	}
}
