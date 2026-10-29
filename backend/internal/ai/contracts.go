package ai

import (
	"context"
	"encoding/json"
	"time"
)

type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// Message is one normalized message inside the AI domain.
type Message struct {
	Role       Role       `json:"role"`
	Content    string     `json:"content"`
	Name       string     `json:"name,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
}

// ToolDefinition describes a tool that may be shown to an LLM.
type ToolDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

// ToolCall is a structured request from an LLM to execute a tool.
type ToolCall struct {
	ID            string          `json:"id"`
	Name          string          `json:"name"`
	Arguments     json.RawMessage `json:"arguments"`
	ProviderState []byte          `json:"-"`
}

// LLMRequest is independent from OpenAI, Gemini or another provider.
type LLMRequest struct {
	Messages      []Message        `json:"messages"`
	Tools         []ToolDefinition `json:"tools"`
	WalletAddress string           `json:"wallet_address"`
	ChainID       uint64           `json:"chain_id"`
	Locale        string           `json:"locale"`
}

// LLMResponse is the provider-neutral result returned to the agent.
type LLMResponse struct {
	Message   Message    `json:"message"`
	ToolCalls []ToolCall `json:"tool_calls"`
}

// LLMClient is implemented by real providers and test mocks.
type LLMClient interface {
	Complete(
		ctx context.Context,
		request LLMRequest,
	) (LLMResponse, error)
}

type ToolError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ToolResult is the common envelope returned by every AI tool.
type ToolResult struct {
	Data        json.RawMessage `json:"data"`
	Source      string          `json:"source"`
	ChainID     uint64          `json:"chain_id"`
	BlockNumber *uint64         `json:"block_number"`
	ObservedAt  time.Time       `json:"observed_at"`
	Error       *ToolError      `json:"error"`
}

// Tool is one capability available to the AI agent.
type Tool interface {
	Definition() ToolDefinition

	Execute(
		ctx context.Context,
		arguments json.RawMessage,
	) (ToolResult, error)
}

// ToolRegistry provides tool discovery and execution.
type ToolRegistry interface {
	Definitions() []ToolDefinition

	Execute(
		ctx context.Context,
		name string,
		arguments json.RawMessage,
	) (ToolResult, error)
}

// MemoryStore stores normalized conversation messages.
type MemoryStore interface {
	Load(
		ctx context.Context,
		sessionID string,
	) ([]Message, error)

	Append(
		ctx context.Context,
		sessionID string,
		message Message,
	) error
}

type PolicyRequest struct {
	ToolName      string `json:"tool_name"`
	ChainID       uint64 `json:"chain_id"`
	WalletAddress string `json:"wallet_address"`
}

type PolicyDecision struct {
	Allowed              bool   `json:"allowed"`
	RequiresConfirmation bool   `json:"requires_confirmation"`
	Reason               string `json:"reason"`
}

// PolicyEngine evaluates whether a tool call may proceed.
type PolicyEngine interface {
	Evaluate(
		ctx context.Context,
		request PolicyRequest,
	) (PolicyDecision, error)
}
