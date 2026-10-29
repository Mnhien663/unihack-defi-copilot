package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

const maxAgentIterations = 4

var (
	ErrNilLLMClient       = errors.New("LLM client is nil")
	ErrNilToolRegistry    = errors.New("tool registry is nil")
	ErrNilMemoryStore     = errors.New("memory store is nil")
	ErrNilPolicyEngine    = errors.New("policy engine is nil")
	ErrInvalidAgentInput  = errors.New("agent input is invalid")
	ErrEmptyLLMResponse   = errors.New("LLM returned an empty response")
	ErrPolicyDenied       = errors.New("tool call denied by policy")
	ErrMaxAgentIterations = errors.New("maximum agent iterations reached")
)

type AgentInput struct {
	SessionID     string
	WalletAddress string
	ChainID       uint64
	Message       string
	Locale        string
}

type ToolExecution struct {
	Call     ToolCall       `json:"call"`
	Decision PolicyDecision `json:"decision"`
	Result   ToolResult     `json:"result"`
}

type AgentOutput struct {
	Message              string          `json:"message"`
	ToolExecutions       []ToolExecution `json:"tool_executions"`
	RequiresConfirmation bool            `json:"requires_confirmation"`
}

type Agent struct {
	llm      LLMClient
	tools    ToolRegistry
	memory   MemoryStore
	policies PolicyEngine
}

func NewAgent(
	llm LLMClient,
	tools ToolRegistry,
	memory MemoryStore,
	policies PolicyEngine,
) (*Agent, error) {
	if llm == nil {
		return nil, ErrNilLLMClient
	}

	if tools == nil {
		return nil, ErrNilToolRegistry
	}

	if memory == nil {
		return nil, ErrNilMemoryStore
	}

	if policies == nil {
		return nil, ErrNilPolicyEngine
	}

	return &Agent{
		llm:      llm,
		tools:    tools,
		memory:   memory,
		policies: policies,
	}, nil
}

func (a *Agent) Run(
	ctx context.Context,
	input AgentInput,
) (AgentOutput, error) {
	input.SessionID = strings.TrimSpace(input.SessionID)
	input.WalletAddress = strings.TrimSpace(input.WalletAddress)
	input.Message = strings.TrimSpace(input.Message)
	input.Locale = strings.ToLower(strings.TrimSpace(input.Locale))

	if input.SessionID == "" || input.Message == "" {
		return AgentOutput{}, ErrInvalidAgentInput
	}

	history, err := a.memory.Load(ctx, input.SessionID)
	if err != nil {
		return AgentOutput{}, fmt.Errorf(
			"load conversation memory: %w",
			err,
		)
	}

	userMessage := Message{
		Role:    RoleUser,
		Content: input.Message,
	}

	if err := a.memory.Append(
		ctx,
		input.SessionID,
		userMessage,
	); err != nil {
		return AgentOutput{}, fmt.Errorf(
			"append user message: %w",
			err,
		)
	}

	messages := make([]Message, 0, len(history)+1)
	messages = append(messages, history...)
	messages = append(messages, userMessage)

	executions := make([]ToolExecution, 0)
	requiresConfirmation := false
	definitions := a.tools.Definitions()

	for iteration := 0; iteration < maxAgentIterations; iteration++ {
		requestMessages := make([]Message, len(messages))
		copy(requestMessages, messages)

		response, err := a.llm.Complete(
			ctx,
			LLMRequest{
				Messages:      requestMessages,
				Tools:         definitions,
				WalletAddress: input.WalletAddress,
				ChainID:       input.ChainID,
				Locale:        input.Locale,
			},
		)
		if err != nil {
			return AgentOutput{}, fmt.Errorf(
				"complete LLM request: %w",
				err,
			)
		}

		if len(response.ToolCalls) == 0 {
			finalContent := strings.TrimSpace(
				response.Message.Content,
			)

			if finalContent == "" {
				return AgentOutput{}, ErrEmptyLLMResponse
			}

			assistantMessage := Message{
				Role:    RoleAssistant,
				Content: finalContent,
			}

			if err := a.memory.Append(
				ctx,
				input.SessionID,
				assistantMessage,
			); err != nil {
				return AgentOutput{}, fmt.Errorf(
					"append assistant message: %w",
					err,
				)
			}

			return AgentOutput{
				Message:              finalContent,
				ToolExecutions:       executions,
				RequiresConfirmation: requiresConfirmation,
			}, nil
		}

		messages = append(
			messages,
			Message{
				Role:      RoleAssistant,
				Content:   response.Message.Content,
				ToolCalls: response.ToolCalls,
			},
		)

		for _, call := range response.ToolCalls {
			decision, err := a.policies.Evaluate(
				ctx,
				PolicyRequest{
					ToolName:      call.Name,
					ChainID:       input.ChainID,
					WalletAddress: input.WalletAddress,
				},
			)
			if err != nil {
				return AgentOutput{}, fmt.Errorf(
					"evaluate tool policy: %w",
					err,
				)
			}

			if !decision.Allowed {
				return AgentOutput{}, fmt.Errorf(
					"%w: %s",
					ErrPolicyDenied,
					decision.Reason,
				)
			}

			if decision.RequiresConfirmation {
				requiresConfirmation = true
			}

			result, err := a.tools.Execute(
				ctx,
				call.Name,
				call.Arguments,
			)
			if err != nil {
				return AgentOutput{}, fmt.Errorf(
					"execute tool %q: %w",
					call.Name,
					err,
				)
			}

			executions = append(
				executions,
				ToolExecution{
					Call:     call,
					Decision: decision,
					Result:   result,
				},
			)

			resultJSON, err := json.Marshal(result)
			if err != nil {
				return AgentOutput{}, fmt.Errorf(
					"encode tool result: %w",
					err,
				)
			}

			messages = append(
				messages,
				Message{
					Role:       RoleTool,
					Content:    string(resultJSON),
					Name:       call.Name,
					ToolCallID: call.ID,
				},
			)
		}
	}

	return AgentOutput{}, ErrMaxAgentIterations
}
