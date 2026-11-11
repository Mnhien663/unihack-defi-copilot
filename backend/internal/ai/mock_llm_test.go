package ai

import (
	"context"
	"errors"
	"testing"
)

func TestMockLLMReturnsScriptedResponses(t *testing.T) {
	mock := NewMockLLMClient(
		MockLLMStep{
			Response: LLMResponse{
				ToolCalls: []ToolCall{
					{
						ID:   "call-001",
						Name: "get_native_balance",
					},
				},
			},
		},
		MockLLMStep{
			Response: LLMResponse{
				Message: Message{
					Role:    RoleAssistant,
					Content: "Your balance is 2.18 AVAX.",
				},
			},
		},
	)

	firstResponse, err := mock.Complete(
		context.Background(),
		LLMRequest{},
	)
	if err != nil {
		t.Fatalf("first Complete() returned error: %v", err)
	}

	if len(firstResponse.ToolCalls) != 1 {
		t.Fatalf(
			"first response has %d tool calls, want 1",
			len(firstResponse.ToolCalls),
		)
	}

	if firstResponse.ToolCalls[0].Name != "get_native_balance" {
		t.Fatalf(
			"tool name = %q, want get_native_balance",
			firstResponse.ToolCalls[0].Name,
		)
	}

	secondResponse, err := mock.Complete(
		context.Background(),
		LLMRequest{},
	)
	if err != nil {
		t.Fatalf("second Complete() returned error: %v", err)
	}

	if secondResponse.Message.Content != "Your balance is 2.18 AVAX." {
		t.Fatalf(
			"message = %q, want final mock answer",
			secondResponse.Message.Content,
		)
	}
}

func TestMockLLMRecordsRequests(t *testing.T) {
	mock := NewMockLLMClient(
		MockLLMStep{
			Response: LLMResponse{},
		},
	)

	request := LLMRequest{
		Messages: []Message{
			{
				Role:    RoleUser,
				Content: "Show my AVAX balance",
			},
		},
	}

	_, err := mock.Complete(
		context.Background(),
		request,
	)
	if err != nil {
		t.Fatalf("Complete() returned error: %v", err)
	}

	recorded := mock.RecordedRequests()

	if len(recorded) != 1 {
		t.Fatalf(
			"RecordedRequests() returned %d requests, want 1",
			len(recorded),
		)
	}

	if recorded[0].Messages[0].Content != "Show my AVAX balance" {
		t.Fatalf(
			"recorded content = %q, want original request",
			recorded[0].Messages[0].Content,
		)
	}
}

func TestMockLLMReturnsExhaustedError(t *testing.T) {
	mock := NewMockLLMClient()

	_, err := mock.Complete(
		context.Background(),
		LLMRequest{},
	)

	if !errors.Is(err, ErrMockLLMExhausted) {
		t.Fatalf(
			"Complete() error = %v, want ErrMockLLMExhausted",
			err,
		)
	}
}

func TestMockLLMReturnsScriptedError(t *testing.T) {
	expectedError := errors.New("mock provider unavailable")

	mock := NewMockLLMClient(
		MockLLMStep{
			Err: expectedError,
		},
	)

	_, err := mock.Complete(
		context.Background(),
		LLMRequest{},
	)

	if !errors.Is(err, expectedError) {
		t.Fatalf(
			"Complete() error = %v, want scripted error",
			err,
		)
	}
}

func TestMockLLMHonorsCancelledContext(t *testing.T) {
	mock := NewMockLLMClient(
		MockLLMStep{
			Response: LLMResponse{},
		},
	)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := mock.Complete(ctx, LLMRequest{})

	if !errors.Is(err, context.Canceled) {
		t.Fatalf(
			"Complete() error = %v, want context.Canceled",
			err,
		)
	}

	recorded := mock.RecordedRequests()
	if len(recorded) != 0 {
		t.Fatalf(
			"cancelled request was recorded %d time(s), want 0",
			len(recorded),
		)
	}
}
