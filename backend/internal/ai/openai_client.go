package ai

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

var (
	ErrMissingOpenAIAPIKey  = errors.New("OPENAI_API_KEY is not configured")
	ErrMissingOpenAIModel   = errors.New("OPENAI_MODEL is not configured")
	ErrEmptyOpenAIChoices   = errors.New("OpenAI returned no completion choices")
	ErrOpenAIRequestFailed  = errors.New("OpenAI request failed")
	ErrOpenAIResponseFailed = errors.New("OpenAI response is invalid")
)

type OpenAIConfig struct {
	APIKey string
	Model  string
	Logger *slog.Logger
}

type OpenAIClient struct {
	client openai.Client
	model  string
	logger *slog.Logger
}

func NewOpenAIClient(config OpenAIConfig) (*OpenAIClient, error) {
	apiKey := strings.TrimSpace(config.APIKey)
	if apiKey == "" {
		return nil, ErrMissingOpenAIAPIKey
	}

	model := strings.TrimSpace(config.Model)
	if model == "" {
		return nil, ErrMissingOpenAIModel
	}

	logger := config.Logger
	if logger == nil {
		logger = slog.Default()
	}

	return &OpenAIClient{
		client: openai.NewClient(
			option.WithAPIKey(apiKey),
			option.WithMaxRetries(1),
		),
		model:  model,
		logger: logger,
	}, nil
}

func NewOpenAIClientFromEnv(
	logger *slog.Logger,
) (*OpenAIClient, error) {
	return NewOpenAIClient(OpenAIConfig{
		APIKey: os.Getenv("OPENAI_API_KEY"),
		Model:  os.Getenv("OPENAI_MODEL"),
		Logger: logger,
	})
}

func (c *OpenAIClient) Complete(
	ctx context.Context,
	request LLMRequest,
) (LLMResponse, error) {
	if err := ctx.Err(); err != nil {
		return LLMResponse{}, err
	}

	providerMessages, err := toOpenAIMessages(request.Messages)
	if err != nil {
		return LLMResponse{}, fmt.Errorf(
			"map OpenAI messages: %w",
			err,
		)
	}

	providerTools, err := toOpenAITools(request.Tools)
	if err != nil {
		return LLMResponse{}, fmt.Errorf(
			"map OpenAI tools: %w",
			err,
		)
	}

	contextMessage := fmt.Sprintf(
		"Current request context: chain_id=%d, wallet_address=%s, locale=%s.",
		request.ChainID,
		request.WalletAddress,
		request.Locale,
	)

	messages := make(
		[]openai.ChatCompletionMessageParamUnion,
		0,
		len(providerMessages)+2,
	)

	messages = append(
		messages,
		openai.DeveloperMessage(
			strings.TrimSpace(SystemPrompt),
		),
		openai.DeveloperMessage(contextMessage),
	)

	messages = append(messages, providerMessages...)

	params := openai.ChatCompletionNewParams{
		Model:               openai.ChatModel(c.model),
		Messages:            messages,
		Tools:               providerTools,
		ParallelToolCalls:   openai.Bool(false),
		Store:               openai.Bool(false),
		MaxCompletionTokens: openai.Int(512),
	}

	startedAt := time.Now()

	completion, err := c.client.Chat.Completions.New(
		ctx,
		params,
	)

	latency := time.Since(startedAt)

	if err != nil {
		c.logger.ErrorContext(
			ctx,
			"OpenAI completion failed",
			"provider",
			"openai",
			"model",
			c.model,
			"latency_ms",
			latency.Milliseconds(),
			"error_type",
			fmt.Sprintf("%T", err),
		)

		return LLMResponse{}, fmt.Errorf(
			"%w: %T",
			ErrOpenAIRequestFailed,
			err,
		)
	}

	if len(completion.Choices) == 0 {
		c.logger.ErrorContext(
			ctx,
			"OpenAI completion returned no choices",
			"provider",
			"openai",
			"model",
			c.model,
			"latency_ms",
			latency.Milliseconds(),
		)

		return LLMResponse{}, ErrEmptyOpenAIChoices
	}

	providerMessage := completion.Choices[0].Message

	toolCalls, err := fromOpenAIToolCalls(
		providerMessage.ToolCalls,
	)
	if err != nil {
		c.logger.ErrorContext(
			ctx,
			"OpenAI tool call parsing failed",
			"provider",
			"openai",
			"model",
			c.model,
			"latency_ms",
			latency.Milliseconds(),
			"tool_call_count",
			len(providerMessage.ToolCalls),
		)

		return LLMResponse{}, fmt.Errorf(
			"%w: %v",
			ErrOpenAIResponseFailed,
			err,
		)
	}

	c.logger.InfoContext(
		ctx,
		"OpenAI completion succeeded",
		"provider",
		"openai",
		"model",
		completion.Model,
		"latency_ms",
		latency.Milliseconds(),
		"input_tokens",
		completion.Usage.PromptTokens,
		"output_tokens",
		completion.Usage.CompletionTokens,
		"total_tokens",
		completion.Usage.TotalTokens,
		"tool_call_count",
		len(toolCalls),
	)

	message := Message{
		Role:      RoleAssistant,
		Content:   providerMessage.Content,
		ToolCalls: toolCalls,
	}

	return LLMResponse{
		Message:   message,
		ToolCalls: toolCalls,
	}, nil
}

var _ LLMClient = (*OpenAIClient)(nil)
