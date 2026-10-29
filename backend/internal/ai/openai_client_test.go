package ai

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
)

func testLogger() *slog.Logger {
	return slog.New(
		slog.NewTextHandler(
			io.Discard,
			nil,
		),
	)
}

func TestNewOpenAIClientRejectsMissingAPIKey(t *testing.T) {
	_, err := NewOpenAIClient(OpenAIConfig{
		Model:  "gpt-4.1-mini",
		Logger: testLogger(),
	})

	if !errors.Is(err, ErrMissingOpenAIAPIKey) {
		t.Fatalf(
			"expected ErrMissingOpenAIAPIKey, got %v",
			err,
		)
	}
}

func TestNewOpenAIClientRejectsMissingModel(t *testing.T) {
	_, err := NewOpenAIClient(OpenAIConfig{
		APIKey: "test-api-key",
		Logger: testLogger(),
	})

	if !errors.Is(err, ErrMissingOpenAIModel) {
		t.Fatalf(
			"expected ErrMissingOpenAIModel, got %v",
			err,
		)
	}
}

func TestNewOpenAIClientAcceptsValidConfig(t *testing.T) {
	client, err := NewOpenAIClient(OpenAIConfig{
		APIKey: "test-api-key",
		Model:  "gpt-4.1-mini",
		Logger: testLogger(),
	})
	if err != nil {
		t.Fatalf("create OpenAI client: %v", err)
	}

	if client == nil {
		t.Fatal("expected client, got nil")
	}

	if client.model != "gpt-4.1-mini" {
		t.Fatalf(
			"expected model gpt-4.1-mini, got %q",
			client.model,
		)
	}
}

func TestNewOpenAIClientFromEnv(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-api-key")
	t.Setenv("OPENAI_MODEL", "gpt-4.1-mini")

	client, err := NewOpenAIClientFromEnv(testLogger())
	if err != nil {
		t.Fatalf("create OpenAI client from environment: %v", err)
	}

	if client.model != "gpt-4.1-mini" {
		t.Fatalf(
			"expected model gpt-4.1-mini, got %q",
			client.model,
		)
	}
}

func TestNewOpenAIClientFromEnvRejectsMissingKey(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENAI_MODEL", "gpt-4.1-mini")

	_, err := NewOpenAIClientFromEnv(testLogger())

	if !errors.Is(err, ErrMissingOpenAIAPIKey) {
		t.Fatalf(
			"expected ErrMissingOpenAIAPIKey, got %v",
			err,
		)
	}
}

func TestOpenAIClientHonorsCancelledContext(t *testing.T) {
	client, err := NewOpenAIClient(OpenAIConfig{
		APIKey: "test-api-key",
		Model:  "gpt-4.1-mini",
		Logger: testLogger(),
	})
	if err != nil {
		t.Fatalf("create OpenAI client: %v", err)
	}

	ctx := t.Context()
	cancelledCtx, cancel := context.WithCancel(ctx)
	cancel()

	_, err = client.Complete(
		cancelledCtx,
		LLMRequest{},
	)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf(
			"expected context.Canceled, got %v",
			err,
		)
	}
}
