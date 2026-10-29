package ai

import (
	"context"
	"errors"
	"strings"
	"sync"
)

var ErrEmptySessionID = errors.New("session ID is empty")

type InMemoryStore struct {
	mu       sync.RWMutex
	messages map[string][]Message
}

func NewInMemoryStore() *InMemoryStore {
	return &InMemoryStore{
		messages: make(map[string][]Message),
	}
}

func (s *InMemoryStore) Load(
	ctx context.Context,
	sessionID string,
) ([]Message, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, ErrEmptySessionID
	}

	s.mu.RLock()
	stored := s.messages[sessionID]

	result := make([]Message, len(stored))
	copy(result, stored)

	s.mu.RUnlock()

	return result, nil
}

func (s *InMemoryStore) Append(
	ctx context.Context,
	sessionID string,
	message Message,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return ErrEmptySessionID
	}

	s.mu.Lock()
	s.messages[sessionID] = append(
		s.messages[sessionID],
		message,
	)
	s.mu.Unlock()

	return nil
}

var _ MemoryStore = (*InMemoryStore)(nil)
