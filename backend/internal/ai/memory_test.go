package ai

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestInMemoryStoreAppendAndLoad(t *testing.T) {
	store := NewInMemoryStore()
	ctx := context.Background()

	expected := []Message{
		{
			Role:    RoleUser,
			Content: "Show my AVAX balance",
		},
		{
			Role:    RoleAssistant,
			Content: "Your balance is 2.18 AVAX",
		},
	}

	for _, message := range expected {
		if err := store.Append(
			ctx,
			"session-001",
			message,
		); err != nil {
			t.Fatalf("Append() returned error: %v", err)
		}
	}

	actual, err := store.Load(ctx, "session-001")
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}

	if len(actual) != len(expected) {
		t.Fatalf(
			"Load() returned %d messages, want %d",
			len(actual),
			len(expected),
		)
	}

	for index := range expected {
		if !reflect.DeepEqual(actual[index], expected[index]) {
			t.Fatalf(
				"message %d = %+v, want %+v",
				index,
				actual[index],
				expected[index],
			)
		}
	}
}

func TestInMemoryStoreUnknownSessionReturnsEmptySlice(t *testing.T) {
	store := NewInMemoryStore()

	messages, err := store.Load(
		context.Background(),
		"unknown-session",
	)
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}

	if messages == nil {
		t.Fatal("Load() returned nil, want an empty slice")
	}

	if len(messages) != 0 {
		t.Fatalf(
			"Load() returned %d messages, want 0",
			len(messages),
		)
	}
}

func TestInMemoryStoreReturnsCopy(t *testing.T) {
	store := NewInMemoryStore()
	ctx := context.Background()

	err := store.Append(
		ctx,
		"session-001",
		Message{
			Role:    RoleUser,
			Content: "original",
		},
	)
	if err != nil {
		t.Fatalf("Append() returned error: %v", err)
	}

	firstLoad, err := store.Load(ctx, "session-001")
	if err != nil {
		t.Fatalf("first Load() returned error: %v", err)
	}

	firstLoad[0].Content = "modified outside store"

	secondLoad, err := store.Load(ctx, "session-001")
	if err != nil {
		t.Fatalf("second Load() returned error: %v", err)
	}

	if secondLoad[0].Content != "original" {
		t.Fatalf(
			"stored content = %q, want original",
			secondLoad[0].Content,
		)
	}
}

func TestInMemoryStoreRejectsEmptySessionID(t *testing.T) {
	store := NewInMemoryStore()

	err := store.Append(
		context.Background(),
		"   ",
		Message{
			Role:    RoleUser,
			Content: "Hello",
		},
	)

	if !errors.Is(err, ErrEmptySessionID) {
		t.Fatalf(
			"Append() error = %v, want ErrEmptySessionID",
			err,
		)
	}
}

func TestInMemoryStoreHonorsCancelledContext(t *testing.T) {
	store := NewInMemoryStore()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := store.Load(ctx, "session-001")

	if !errors.Is(err, context.Canceled) {
		t.Fatalf(
			"Load() error = %v, want context.Canceled",
			err,
		)
	}
}
