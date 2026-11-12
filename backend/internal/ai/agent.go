package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	maxAgentIterations = 4
	maxAgentToolCalls  = 4
)

var (
	ErrNilLLMClient           = errors.New("LLM client is nil")
	ErrNilToolRegistry        = errors.New("tool registry is nil")
	ErrNilMemoryStore         = errors.New("memory store is nil")
	ErrNilPolicyEngine        = errors.New("policy engine is nil")
	ErrInvalidAgentInput      = errors.New("agent input is invalid")
	ErrEmptyLLMResponse       = errors.New("LLM returned an empty response")
	ErrPolicyDenied           = errors.New("tool call denied by policy")
	ErrMaxAgentIterations     = errors.New("maximum agent iterations reached")
	ErrMaxAgentToolCalls      = errors.New("maximum agent tool calls reached")
	ErrParallelWriteCalls     = errors.New("parallel write tool calls are not allowed")
	ErrAgentMissingToolCallID = errors.New("agent tool call ID is required")
	ErrDuplicateToolCallID    = errors.New("duplicate tool call ID")
	ErrMixedReadWriteCalls    = errors.New(
		"read and write tool calls cannot share one batch",
	)
)

type AgentInput struct {
	RequestID      string
	IdempotencyKey string
	SessionID      string
	WalletAddress  string
	ChainID        uint64
	Message        string
	Locale         string
}

type ToolExecution struct {
	Call     ToolCall       `json:"call"`
	Decision PolicyDecision `json:"decision"`
	Result   ToolResult     `json:"result"`
}

type PendingAction struct {
	PendingActionID      string          `json:"pending_action_id,omitempty"`
	RequestID            string          `json:"request_id"`
	ToolCallID           string          `json:"tool_call_id"`
	ToolName             string          `json:"tool_name"`
	Arguments            json.RawMessage `json:"arguments"`
	ChainID              uint64          `json:"chain_id"`
	WalletAddress        string          `json:"wallet_address"`
	NormalizedIntentHash string          `json:"normalized_intent_hash,omitempty"`
	ExpiresAt            string          `json:"expires_at,omitempty"`
	RequiresConfirmation bool            `json:"requires_confirmation"`
	IdempotencyKey       string          `json:"-"`
	RequestFingerprint   string          `json:"-"`
}

type AgentOutput struct {
	Message              string          `json:"message"`
	ToolExecutions       []ToolExecution `json:"tool_executions"`
	PendingAction        *PendingAction  `json:"pending_action"`
	RequiresConfirmation bool            `json:"requires_confirmation"`
}

const prepareSwapActionName = "prepare_swap"

type SwapPreparation struct {
	Status    string
	Blockers  []string
	Arguments json.RawMessage // Payload chuẩn hóa do backend dựng.
	ExpiresAt time.Time       // Deadline của giao dịch unsigned.
}

type SwapPreparationFunc func(
	ctx context.Context,
	wallet string,
	chainID uint64,
	arguments json.RawMessage,
) (SwapPreparation, error)

type Agent struct {
	llm         LLMClient
	tools       ToolRegistry
	memory      MemoryStore
	policies    PolicyEngine
	prepareSwap SwapPreparationFunc
}

func pendingActionMessage(locale string) string {
	if strings.EqualFold(strings.TrimSpace(locale), "vi") {
		return "Hành động đã được chuẩn bị nhưng chưa được thực thi. " +
			"Vui lòng kiểm tra chi tiết và xác nhận rõ ràng trong ví."
	}

	return "The action has been prepared but not executed. " +
		"Review the details and explicitly confirm it in your wallet."
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

func NewAgentWithSwapPreparation(
	llm LLMClient,
	tools ToolRegistry,
	memory MemoryStore,
	policies PolicyEngine,
	prepare SwapPreparationFunc,
) (*Agent, error) {
	agent, err := NewAgent(llm, tools, memory, policies)
	if err != nil {
		return nil, err
	}
	if prepare == nil {
		return nil, fmt.Errorf("swap preparation callback is required")
	}
	if _, ok := memory.(TurnRecorder); !ok {
		return nil, fmt.Errorf("swap preparation requires a pending-action recorder")
	}

	agent.prepareSwap = prepare
	return agent, nil
}

func (a *Agent) persistTurn(
	ctx context.Context,
	record TurnRecord,
) error {
	if recorder, ok := a.memory.(TurnRecorder); ok {
		return recorder.RecordTurn(ctx, record)
	}

	return a.memory.Append(
		ctx,
		record.SessionID,
		record.Message,
	)
}

func (a *Agent) Run(
	ctx context.Context,
	input AgentInput,
) (AgentOutput, error) {
	input.RequestID = strings.TrimSpace(input.RequestID)
	input.SessionID = strings.TrimSpace(input.SessionID)
	input.WalletAddress = strings.TrimSpace(input.WalletAddress)
	input.Message = strings.TrimSpace(input.Message)
	input.Locale = strings.ToLower(strings.TrimSpace(input.Locale))

	if input.SessionID == "" || input.Message == "" {
		return AgentOutput{}, ErrInvalidAgentInput
	}

	guardrailDecision := EvaluateInputGuardrail(input.Message)

	if !guardrailDecision.Allowed {
		return AgentOutput{
			Message:              promptInjectionRefusal(input.Locale),
			ToolExecutions:       []ToolExecution{},
			RequiresConfirmation: false,
		}, nil
	}

	if sessionInitializer, ok := a.memory.(SessionInitializer); ok {
		err := sessionInitializer.EnsureSession(
			ctx,
			SessionMetadata{
				SessionID:     input.SessionID,
				WalletAddress: input.WalletAddress,
				ChainID:       input.ChainID,
				Locale:        input.Locale,
			},
		)
		if err != nil {
			return AgentOutput{}, fmt.Errorf(
				"ensure conversation session: %w",
				err,
			)
		}
	}

	var swapFingerprint string

	if input.IdempotencyKey != "" && a.prepareSwap != nil {
		var err error
		swapFingerprint, err = swapRequestFingerprint(input)
		if err != nil {
			return AgentOutput{}, err
		}

		lookup, ok := a.memory.(PendingSwapLookup)
		if !ok {
			return AgentOutput{}, fmt.Errorf("swap preparation requires pending-action lookup")
		}

		existing, err := lookup.LookupPendingSwap(
			ctx,
			input.SessionID,
			input.IdempotencyKey,
			swapFingerprint,
		)
		if err != nil {
			return AgentOutput{}, fmt.Errorf("lookup swap retry: %w", err)
		}
		if existing != nil {
			return AgentOutput{
				Message:              pendingActionMessage(input.Locale),
				ToolExecutions:       []ToolExecution{},
				PendingAction:        existing,
				RequiresConfirmation: true,
			}, nil
		}
	}

	history, err := a.memory.Load(ctx, input.SessionID)
	if err != nil {
		return AgentOutput{}, fmt.Errorf(
			"load conversation memory: %w",
			err,
		)
	}

	userMessage := Message{
		RequestID: input.RequestID,
		Role:      RoleUser,
		Content:   input.Message,
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
	toolCallCount := 0
	seenToolCallIDs := make(map[string]struct{})

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
				RequestID: input.RequestID,
				Role:      RoleAssistant,
				Content:   finalContent,
			}

			if err := a.persistTurn(
				ctx,
				TurnRecord{
					SessionID:      input.SessionID,
					Message:        assistantMessage,
					ToolExecutions: executions,
				},
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

		remainingToolCalls := maxAgentToolCalls - toolCallCount

		if len(response.ToolCalls) > remainingToolCalls {
			return AgentOutput{}, fmt.Errorf(
				"%w: got %d new calls with %d remaining",
				ErrMaxAgentToolCalls,
				len(response.ToolCalls),
				remainingToolCalls,
			)
		}

		toolCallCount += len(response.ToolCalls)

		messages = append(
			messages,
			Message{
				Role:      RoleAssistant,
				Content:   response.Message.Content,
				ToolCalls: response.ToolCalls,
			},
		)

		decisions := make(
			[]PolicyDecision,
			len(response.ToolCalls),
		)

		writeCallCount := 0

		// Preflight the complete batch before executing any tool.
		for index, call := range response.ToolCalls {
			callID := strings.TrimSpace(call.ID)

			if callID == "" {
				return AgentOutput{}, fmt.Errorf(
					"%w: tool %q",
					ErrAgentMissingToolCallID,
					call.Name,
				)
			}

			if _, exists := seenToolCallIDs[callID]; exists {
				return AgentOutput{}, fmt.Errorf(
					"%w: %s",
					ErrDuplicateToolCallID,
					callID,
				)
			}

			seenToolCallIDs[callID] = struct{}{}
			response.ToolCalls[index].ID = callID

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
				writeCallCount++
			}

			decisions[index] = decision
		}

		if writeCallCount > 1 {
			return AgentOutput{}, fmt.Errorf(
				"%w: got %d write calls",
				ErrParallelWriteCalls,
				writeCallCount,
			)
		}

		if writeCallCount == 1 && len(response.ToolCalls) != 1 {
			return AgentOutput{}, fmt.Errorf(
				"%w: got %d calls including one write",
				ErrMixedReadWriteCalls,
				len(response.ToolCalls),
			)
		}

		if writeCallCount == 1 {
			call := response.ToolCalls[0]
			pendingMessage := pendingActionMessage(input.Locale)

			pendingArguments := call.Arguments
			pendingExpiry := ""

			if call.Name == prepareSwapActionName {
				if a.prepareSwap == nil {
					return AgentOutput{}, fmt.Errorf("swap preparation is not enabled")
				}

				if swapFingerprint == "" {
					return AgentOutput{}, ErrInvalidIdempotencyKey
				}

				prepared, err := a.prepareSwap(
					ctx, input.WalletAddress, input.ChainID, call.Arguments,
				)
				if err != nil {
					return AgentOutput{}, fmt.Errorf("prepare swap: %w", err)
				}

				switch prepared.Status {
				case "approval_required", "blocked", "reverted":
					switch prepared.Status {
					case "approval_required":
						pendingMessage = "Swap not prepared. A separate ERC-20 approval for the AllowanceManager is required."
					case "blocked":
						pendingMessage = "Swap not prepared. Requirements not met: " +
							strings.Join(prepared.Blockers, ", ") + "."
					case "reverted":
						pendingMessage = "Swap not prepared. The simulation reverted."
					}

					assistantMessage := Message{
						RequestID: input.RequestID,
						Role:      RoleAssistant,
						Content:   pendingMessage,
					}
					if err := a.persistTurn(ctx, TurnRecord{
						SessionID: input.SessionID,
						Message:   assistantMessage,
					}); err != nil {
						return AgentOutput{}, fmt.Errorf("persist blocked swap response: %w", err)
					}
					return AgentOutput{
						Message:        pendingMessage,
						ToolExecutions: []ToolExecution{},
					}, nil

				case "ready_for_review":
					payload := strings.TrimSpace(string(prepared.Arguments))
					if !strings.HasPrefix(payload, "{") ||
						!json.Valid(prepared.Arguments) ||
						!prepared.ExpiresAt.After(time.Now()) {
						return AgentOutput{}, fmt.Errorf("invalid or expired prepared swap")
					}

					pendingArguments = prepared.Arguments
					pendingExpiry = prepared.ExpiresAt.UTC().Format(time.RFC3339Nano)

				default:
					return AgentOutput{}, fmt.Errorf("unexpected swap preparation status")
				}
			}

			pendingAction := &PendingAction{
				RequestID:            input.RequestID,
				ToolCallID:           call.ID,
				ToolName:             call.Name,
				Arguments:            pendingArguments,
				ExpiresAt:            pendingExpiry,
				ChainID:              input.ChainID,
				WalletAddress:        input.WalletAddress,
				RequiresConfirmation: true,
			}

			if call.Name == prepareSwapActionName {
				pendingAction.IdempotencyKey = input.IdempotencyKey
				pendingAction.RequestFingerprint = swapFingerprint
			}

			assistantMessage := Message{
				RequestID: input.RequestID,
				Role:      RoleAssistant,
				Content:   pendingMessage,
			}

			if err := a.persistTurn(
				ctx,
				TurnRecord{
					SessionID:     input.SessionID,
					Message:       assistantMessage,
					PendingAction: pendingAction,
				},
			); err != nil {
				return AgentOutput{}, fmt.Errorf(
					"persist pending action turn: %w",
					err,
				)
			}

			return AgentOutput{
				Message:              pendingMessage,
				ToolExecutions:       []ToolExecution{},
				PendingAction:        pendingAction,
				RequiresConfirmation: true,
			}, nil
		}

		// Read tools are intentionally executed sequentially for easier debugging.
		for index, call := range response.ToolCalls {
			decision := decisions[index]

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
