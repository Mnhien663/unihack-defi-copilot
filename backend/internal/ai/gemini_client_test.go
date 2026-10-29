package ai

import (
	"context"
	"errors"
	"testing"
)

func TestNewGeminiClientRejectsMissingAPIKey(t *testing.T) {
	_, err := NewGeminiClient(
		t.Context(),
		GeminiConfig{
			Model:  "gemini-3-flash-preview",
			Logger: testLogger(),
		},
	)

	if !errors.Is(err, ErrMissingGeminiAPIKey) {
		t.Fatalf(
			"expected ErrMissingGeminiAPIKey, got %v",
			err,
		)
	}
}

func TestNewGeminiClientRejectsMissingModel(t *testing.T) {
	_, err := NewGeminiClient(
		t.Context(),
		GeminiConfig{
			APIKey: "test-api-key",
			Logger: testLogger(),
		},
	)

	if !errors.Is(err, ErrMissingGeminiModel) {
		t.Fatalf(
			"expected ErrMissingGeminiModel, got %v",
			err,
		)
	}
}

func TestNewGeminiClientAcceptsValidConfig(t *testing.T) {
	client, err := NewGeminiClient(
		t.Context(),
		GeminiConfig{
			APIKey: "test-api-key",
			Model:  "gemini-3-flash-preview",
			Logger: testLogger(),
		},
	)
	if err != nil {
		t.Fatalf("create Gemini client: %v", err)
	}

	if client == nil {
		t.Fatal("expected client, got nil")
	}

	if client.model != "gemini-3-flash-preview" {
		t.Fatalf(
			"unexpected model: %q",
			client.model,
		)
	}
}

func TestNewGeminiClientFromEnv(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "test-api-key")
	t.Setenv("GEMINI_MODEL", "gemini-3-flash-preview")

	client, err := NewGeminiClientFromEnv(
		t.Context(),
		testLogger(),
	)
	if err != nil {
		t.Fatalf(
			"create Gemini client from environment: %v",
			err,
		)
	}

	if client.model != "gemini-3-flash-preview" {
		t.Fatalf(
			"unexpected model: %q",
			client.model,
		)
	}
}

func TestGeminiClientHonorsCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := NewGeminiClient(
		ctx,
		GeminiConfig{
			APIKey: "test-api-key",
			Model:  "gemini-3-flash-preview",
			Logger: testLogger(),
		},
	)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf(
			"expected context.Canceled, got %v",
			err,
		)
	}
}
