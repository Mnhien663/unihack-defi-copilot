package ai

import (
	"context"
	"errors"
	"sync"
)

var ErrMockLLMExhausted = errors.New("mock LLM has no remaining steps")

type MockLLMStep struct {
	Response LLMResponse
	Err      error
}

type MockLLMClient struct {
	mu       sync.Mutex
	steps    []MockLLMStep
	nextStep int
	requests []LLMRequest
}

func NewMockLLMClient(
	steps ...MockLLMStep,
) *MockLLMClient {
	copiedSteps := make([]MockLLMStep, len(steps))
	copy(copiedSteps, steps)

	return &MockLLMClient{
		steps:    copiedSteps,
		requests: make([]LLMRequest, 0),
	}
}

func (m *MockLLMClient) Complete(
	ctx context.Context,
	request LLMRequest,
) (LLMResponse, error) {
	if err := ctx.Err(); err != nil {
		return LLMResponse{}, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.requests = append(m.requests, request)

	if m.nextStep >= len(m.steps) {
		return LLMResponse{}, ErrMockLLMExhausted
	}

	step := m.steps[m.nextStep]
	m.nextStep++

	if step.Err != nil {
		return LLMResponse{}, step.Err
	}

	return step.Response, nil
}

func (m *MockLLMClient) RecordedRequests() []LLMRequest {
	m.mu.Lock()
	defer m.mu.Unlock()

	result := make([]LLMRequest, len(m.requests))
	copy(result, m.requests)

	return result
}

var _ LLMClient = (*MockLLMClient)(nil)
