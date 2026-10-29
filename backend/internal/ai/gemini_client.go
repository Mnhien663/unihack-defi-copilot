package ai

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"google.golang.org/genai"
)

var (
	ErrMissingGeminiAPIKey = errors.New("GEMINI_API_KEY is not configured")
	ErrMissingGeminiModel  = errors.New("GEMINI_MODEL is not configured")
	ErrGeminiRequestFailed = errors.New("Gemini request failed")
	ErrEmptyGeminiResponse = errors.New("Gemini returned an empty response")
)

type GeminiConfig struct {
	APIKey string
	Model  string
	Logger *slog.Logger
}

type GeminiClient struct {
	client *genai.Client
	model  string
	logger *slog.Logger
}

func NewGeminiClient(
	ctx context.Context,
	config GeminiConfig,
) (*GeminiClient, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	apiKey := strings.TrimSpace(config.APIKey)
	if apiKey == "" {
		return nil, ErrMissingGeminiAPIKey
	}

	model := strings.TrimSpace(config.Model)
	if model == "" {
		return nil, ErrMissingGeminiModel
	}

	logger := config.Logger
	if logger == nil {
		logger = slog.Default()
	}

	client, err := genai.NewClient(
		ctx,
		&genai.ClientConfig{
			APIKey:  apiKey,
			Backend: genai.BackendGeminiAPI,
		},
	)
	if err != nil {
		return nil, fmt.Errorf(
			"create Gemini SDK client: %w",
			err,
		)
	}

	return &GeminiClient{
		client: client,
		model:  model,
		logger: logger,
	}, nil
}

func NewGeminiClientFromEnv(
	ctx context.Context,
	logger *slog.Logger,
) (*GeminiClient, error) {
	return NewGeminiClient(
		ctx,
		GeminiConfig{
			APIKey: os.Getenv("GEMINI_API_KEY"),
			Model:  os.Getenv("GEMINI_MODEL"),
			Logger: logger,
		},
	)
}

func (c *GeminiClient) Complete(
	ctx context.Context,
	request LLMRequest,
) (LLMResponse, error) {
	if err := ctx.Err(); err != nil {
		return LLMResponse{}, err
	}

	contents, requestInstructions, err := toGeminiContents(
		request.Messages,
	)
	if err != nil {
		return LLMResponse{}, fmt.Errorf(
			"map Gemini contents: %w",
			err,
		)
	}

	tools, err := toGeminiTools(request.Tools)
	if err != nil {
		return LLMResponse{}, fmt.Errorf(
			"map Gemini tools: %w",
			err,
		)
	}

	instructions := make(
		[]string,
		0,
		len(requestInstructions)+2,
	)

	instructions = append(
		instructions,
		strings.TrimSpace(SystemPrompt),
	)

	instructions = append(
		instructions,
		requestInstructions...,
	)

	instructions = append(
		instructions,
		fmt.Sprintf(
			"Current request context: chain_id=%d, wallet_address=%s, locale=%s.",
			request.ChainID,
			request.WalletAddress,
			request.Locale,
		),
	)

	config := &genai.GenerateContentConfig{
		SystemInstruction: genai.NewContentFromText(
			strings.Join(instructions, "\n\n"),
			genai.RoleUser,
		),
		Tools:           tools,
		MaxOutputTokens: 512,
	}

	startedAt := time.Now()

	response, err := c.client.Models.GenerateContent(
		ctx,
		c.model,
		contents,
		config,
	)

	latency := time.Since(startedAt)

	if err != nil {
		safeError := err.Error()

		if apiKey := strings.TrimSpace(
			os.Getenv("GEMINI_API_KEY"),
		); apiKey != "" {
			safeError = strings.ReplaceAll(
				safeError,
				apiKey,
				"[REDACTED_API_KEY]",
			)
		}

		if request.WalletAddress != "" {
			safeError = strings.ReplaceAll(
				safeError,
				request.WalletAddress,
				"[REDACTED_WALLET]",
			)
		}

		for _, message := range request.Messages {
			if message.Content == "" {
				continue
			}

			safeError = strings.ReplaceAll(
				safeError,
				message.Content,
				"[REDACTED_CONTENT]",
			)
		}

		c.logger.ErrorContext(
			ctx,
			"Gemini completion failed",
			"provider",
			"gemini",
			"model",
			c.model,
			"latency_ms",
			latency.Milliseconds(),
			"error_type",
			fmt.Sprintf("%T", err),
			"error",
			safeError,
		)

		return LLMResponse{}, fmt.Errorf(
			"%w: %T",
			ErrGeminiRequestFailed,
			err,
		)
	}

	functionCalls := response.FunctionCalls()

	toolCalls, err := fromGeminiResponseFunctionCalls(
		response,
	)

	if err != nil {
		safeError := err.Error()

		if apiKey := strings.TrimSpace(
			os.Getenv("GEMINI_API_KEY"),
		); apiKey != "" {
			safeError = strings.ReplaceAll(
				safeError,
				apiKey,
				"[REDACTED_API_KEY]",
			)
		}

		if request.WalletAddress != "" {
			safeError = strings.ReplaceAll(
				safeError,
				request.WalletAddress,
				"[REDACTED_WALLET]",
			)
		}

		for _, message := range request.Messages {
			if message.Content == "" {
				continue
			}

			safeError = strings.ReplaceAll(
				safeError,
				message.Content,
				"[REDACTED_CONTENT]",
			)
		}

		c.logger.ErrorContext(
			ctx,
			"Gemini function call parsing failed",
			"provider",
			"gemini",
			"model",
			c.model,
			"latency_ms",
			latency.Milliseconds(),
			"tool_call_count",
			len(functionCalls),
			"error",
			safeError,
		)

		return LLMResponse{}, fmt.Errorf(
			"parse Gemini function calls: %w",
			err,
		)
	}

	content := strings.TrimSpace(response.Text())

	if len(toolCalls) == 0 && content == "" {
		return LLMResponse{}, ErrEmptyGeminiResponse
	}

	inputTokens := int32(0)
	outputTokens := int32(0)
	totalTokens := int32(0)
	thinkingTokens := int32(0)

	if response.UsageMetadata != nil {
		inputTokens = response.UsageMetadata.PromptTokenCount
		outputTokens = response.UsageMetadata.CandidatesTokenCount
		totalTokens = response.UsageMetadata.TotalTokenCount
		thinkingTokens = response.UsageMetadata.ThoughtsTokenCount
	}

	c.logger.InfoContext(
		ctx,
		"Gemini completion succeeded",
		"provider",
		"gemini",
		"model",
		response.ModelVersion,
		"latency_ms",
		latency.Milliseconds(),
		"input_tokens",
		inputTokens,
		"output_tokens",
		outputTokens,
		"thinking_tokens",
		thinkingTokens,
		"total_tokens",
		totalTokens,
		"tool_call_count",
		len(toolCalls),
	)

	message := Message{
		Role:      RoleAssistant,
		Content:   content,
		ToolCalls: toolCalls,
	}

	return LLMResponse{
		Message:   message,
		ToolCalls: toolCalls,
	}, nil
}

var _ LLMClient = (*GeminiClient)(nil)
