package ai

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	openai "github.com/openai/openai-go/v3"
)

var (
	ErrUnsupportedMessageRole = errors.New("unsupported message role")
	ErrMissingToolCallID      = errors.New("tool message is missing tool call ID")
	ErrInvalidToolSchema      = errors.New("invalid tool input schema")
	ErrInvalidOpenAIToolCall  = errors.New("invalid OpenAI tool call")
)

func toOpenAIMessages(
	messages []Message,
) ([]openai.ChatCompletionMessageParamUnion, error) {
	result := make(
		[]openai.ChatCompletionMessageParamUnion,
		0,
		len(messages),
	)

	for _, message := range messages {
		switch message.Role {
		case RoleSystem:
			result = append(
				result,
				openai.SystemMessage(message.Content),
			)

		case RoleUser:
			result = append(
				result,
				openai.UserMessage(message.Content),
			)

		case RoleAssistant:
			assistantMessage := openai.AssistantMessage(
				message.Content,
			)

			if len(message.ToolCalls) > 0 {
				toolCalls := make(
					[]openai.ChatCompletionMessageToolCallUnionParam,
					0,
					len(message.ToolCalls),
				)

				for _, call := range message.ToolCalls {
					if strings.TrimSpace(call.ID) == "" ||
						strings.TrimSpace(call.Name) == "" {
						return nil, ErrInvalidOpenAIToolCall
					}

					toolCalls = append(
						toolCalls,
						openai.ChatCompletionMessageToolCallUnionParam{
							OfFunction: &openai.ChatCompletionMessageFunctionToolCallParam{
								ID: call.ID,
								Function: openai.ChatCompletionMessageFunctionToolCallFunctionParam{
									Name:      call.Name,
									Arguments: string(call.Arguments),
								},
							},
						},
					)
				}

				assistantMessage.OfAssistant.ToolCalls = toolCalls
			}

			result = append(result, assistantMessage)

		case RoleTool:
			if strings.TrimSpace(message.ToolCallID) == "" {
				return nil, ErrMissingToolCallID
			}

			result = append(
				result,
				openai.ToolMessage(
					message.Content,
					message.ToolCallID,
				),
			)

		default:
			return nil, fmt.Errorf(
				"%w: %q",
				ErrUnsupportedMessageRole,
				message.Role,
			)
		}
	}

	return result, nil
}

func toOpenAITools(
	definitions []ToolDefinition,
) ([]openai.ChatCompletionToolUnionParam, error) {
	result := make(
		[]openai.ChatCompletionToolUnionParam,
		0,
		len(definitions),
	)

	for _, definition := range definitions {
		var parameters openai.FunctionParameters

		if err := json.Unmarshal(
			definition.InputSchema,
			&parameters,
		); err != nil {
			return nil, fmt.Errorf(
				"%w for %q: %v",
				ErrInvalidToolSchema,
				definition.Name,
				err,
			)
		}

		// OpenAI strict function schemas do not need the JSON Schema
		// dialect declaration.
		delete(parameters, "$schema")

		result = append(
			result,
			openai.ChatCompletionFunctionTool(
				openai.FunctionDefinitionParam{
					Name:        definition.Name,
					Description: openai.String(definition.Description),
					Parameters:  parameters,
					Strict:      openai.Bool(true),
				},
			),
		)
	}

	return result, nil
}

func fromOpenAIToolCalls(
	toolCalls []openai.ChatCompletionMessageToolCallUnion,
) ([]ToolCall, error) {
	result := make([]ToolCall, 0, len(toolCalls))

	for _, toolCall := range toolCalls {
		functionCall := toolCall.AsFunction()

		if strings.TrimSpace(functionCall.ID) == "" ||
			strings.TrimSpace(functionCall.Function.Name) == "" {
			return nil, ErrInvalidOpenAIToolCall
		}

		arguments := json.RawMessage(
			functionCall.Function.Arguments,
		)

		if !json.Valid(arguments) {
			return nil, fmt.Errorf(
				"%w: arguments for %q are not valid JSON",
				ErrInvalidOpenAIToolCall,
				functionCall.Function.Name,
			)
		}

		result = append(
			result,
			ToolCall{
				ID:        functionCall.ID,
				Name:      functionCall.Function.Name,
				Arguments: arguments,
			},
		)
	}

	return result, nil
}
