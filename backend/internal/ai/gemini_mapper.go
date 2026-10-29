package ai

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"google.golang.org/genai"
)

var (
	ErrInvalidGeminiMessage  = errors.New("invalid Gemini message")
	ErrInvalidGeminiSchema   = errors.New("invalid Gemini tool schema")
	ErrInvalidGeminiToolCall = errors.New("invalid Gemini tool call")
)

func toGeminiContents(
	messages []Message,
) ([]*genai.Content, []string, error) {
	contents := make([]*genai.Content, 0, len(messages))
	systemInstructions := make([]string, 0)

	for _, message := range messages {
		switch message.Role {
		case RoleSystem:
			content := strings.TrimSpace(message.Content)
			if content != "" {
				systemInstructions = append(
					systemInstructions,
					content,
				)
			}

		case RoleUser:
			contents = append(
				contents,
				genai.NewContentFromText(
					message.Content,
					genai.RoleUser,
				),
			)

		case RoleAssistant:
			parts := make(
				[]*genai.Part,
				0,
				len(message.ToolCalls)+1,
			)

			if strings.TrimSpace(message.Content) != "" {
				parts = append(
					parts,
					genai.NewPartFromText(message.Content),
				)
			}

			for index, call := range message.ToolCalls {
				name := strings.TrimSpace(call.Name)
				if name == "" {
					return nil, nil, fmt.Errorf(
						"%w: assistant tool call %d has no name",
						ErrInvalidGeminiMessage,
						index,
					)
				}

				var arguments map[string]any
				if err := json.Unmarshal(
					call.Arguments,
					&arguments,
				); err != nil {
					return nil, nil, fmt.Errorf(
						"%w: decode arguments for %q: %v",
						ErrInvalidGeminiMessage,
						name,
						err,
					)
				}

				parts = append(
					parts,
					&genai.Part{
						FunctionCall: &genai.FunctionCall{
							ID:   call.ID,
							Name: name,
							Args: arguments,
						},
						ThoughtSignature: append(
							[]byte(nil),
							call.ProviderState...,
						),
					},
				)
			}

			if len(parts) == 0 {
				return nil, nil, fmt.Errorf(
					"%w: assistant message is empty",
					ErrInvalidGeminiMessage,
				)
			}

			contents = append(
				contents,
				genai.NewContentFromParts(
					parts,
					genai.RoleModel,
				),
			)

		case RoleTool:
			name := strings.TrimSpace(message.Name)
			if name == "" {
				return nil, nil, fmt.Errorf(
					"%w: tool result has no name",
					ErrInvalidGeminiMessage,
				)
			}

			var response map[string]any
			if err := json.Unmarshal(
				[]byte(message.Content),
				&response,
			); err != nil {
				return nil, nil, fmt.Errorf(
					"%w: decode result for %q: %v",
					ErrInvalidGeminiMessage,
					name,
					err,
				)
			}

			contents = append(
				contents,
				genai.NewContentFromParts(
					[]*genai.Part{
						{
							FunctionResponse: &genai.FunctionResponse{
								ID:       message.ToolCallID,
								Name:     name,
								Response: response,
							},
						},
					},
					genai.RoleUser,
				),
			)

		default:
			return nil, nil, fmt.Errorf(
				"%w: unsupported role %q",
				ErrInvalidGeminiMessage,
				message.Role,
			)
		}
	}

	return contents, systemInstructions, nil
}

func normalizeGeminiSchema(value any) {
	switch typedValue := value.(type) {
	case map[string]any:
		if constantValue, exists := typedValue["const"]; exists {
			typedValue["enum"] = []any{
				constantValue,
			}

			delete(typedValue, "const")
		}

		delete(typedValue, "$schema")

		for _, nestedValue := range typedValue {
			normalizeGeminiSchema(nestedValue)
		}

	case []any:
		for _, nestedValue := range typedValue {
			normalizeGeminiSchema(nestedValue)
		}
	}
}

func toGeminiTools(
	definitions []ToolDefinition,
) ([]*genai.Tool, error) {
	if len(definitions) == 0 {
		return nil, nil
	}

	declarations := make(
		[]*genai.FunctionDeclaration,
		0,
		len(definitions),
	)

	for _, definition := range definitions {
		var schema map[string]any

		if err := json.Unmarshal(
			definition.InputSchema,
			&schema,
		); err != nil {
			return nil, fmt.Errorf(
				"%w for %q: %v",
				ErrInvalidGeminiSchema,
				definition.Name,
				err,
			)
		}

		normalizeGeminiSchema(schema)

		declarations = append(
			declarations,
			&genai.FunctionDeclaration{
				Name:                 definition.Name,
				Description:          definition.Description,
				ParametersJsonSchema: schema,
			},
		)
	}

	return []*genai.Tool{
		{
			FunctionDeclarations: declarations,
		},
	}, nil
}

func fromGeminiFunctionCalls(
	functionCalls []*genai.FunctionCall,
) ([]ToolCall, error) {
	result := make(
		[]ToolCall,
		0,
		len(functionCalls),
	)

	for index, functionCall := range functionCalls {
		if functionCall == nil {
			return nil, fmt.Errorf(
				"%w: call %d is nil",
				ErrInvalidGeminiToolCall,
				index,
			)
		}

		name := strings.TrimSpace(functionCall.Name)
		if name == "" {
			return nil, fmt.Errorf(
				"%w: call %d has no name",
				ErrInvalidGeminiToolCall,
				index,
			)
		}

		arguments, err := json.Marshal(functionCall.Args)
		if err != nil {
			return nil, fmt.Errorf(
				"%w: encode arguments for %q: %v",
				ErrInvalidGeminiToolCall,
				name,
				err,
			)
		}

		callID := strings.TrimSpace(functionCall.ID)
		if callID == "" {
			callID = fmt.Sprintf(
				"gemini-call-%d",
				index+1,
			)
		}

		result = append(
			result,
			ToolCall{
				ID:        callID,
				Name:      name,
				Arguments: arguments,
			},
		)
	}

	return result, nil
}

func fromGeminiResponseFunctionCalls(
	response *genai.GenerateContentResponse,
) ([]ToolCall, error) {
	if response == nil {
		return nil, fmt.Errorf(
			"%w: response is nil",
			ErrInvalidGeminiToolCall,
		)
	}

	functionCalls := make([]*genai.FunctionCall, 0)
	thoughtSignatures := make([][]byte, 0)

	for _, candidate := range response.Candidates {
		if candidate == nil || candidate.Content == nil {
			continue
		}

		for _, part := range candidate.Content.Parts {
			if part == nil || part.FunctionCall == nil {
				continue
			}

			functionCalls = append(
				functionCalls,
				part.FunctionCall,
			)

			thoughtSignatures = append(
				thoughtSignatures,
				append(
					[]byte(nil),
					part.ThoughtSignature...,
				),
			)
		}
	}

	toolCalls, err := fromGeminiFunctionCalls(functionCalls)
	if err != nil {
		return nil, err
	}

	for index := range toolCalls {
		toolCalls[index].ProviderState = thoughtSignatures[index]
	}

	return toolCalls, nil
}
