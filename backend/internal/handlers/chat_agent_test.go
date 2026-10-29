package handlers

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"unihack-backend/internal/ai"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/requestid"
)

type mockChatRunner struct {
	output       ai.AgentOutput
	err          error
	received     ai.AgentInput
	receivedCall int
}

func (m *mockChatRunner) Run(
	ctx context.Context,
	input ai.AgentInput,
) (ai.AgentOutput, error) {
	m.receivedCall++
	m.received = input

	return m.output, m.err
}

func TestInjectedChatHandlerRunsAgent(t *testing.T) {
	blockNumber := uint64(60034644)

	runner := &mockChatRunner{
		output: ai.AgentOutput{
			Message: "Your balance is 2.181886544100000121 AVAX.",
			ToolExecutions: []ai.ToolExecution{
				{
					Call: ai.ToolCall{
						ID:   "call-001",
						Name: "get_native_balance",
						Arguments: json.RawMessage(`{
							"wallet_address":
							"0x0000000000000000000000000000000000000001",
							"chain_id": 43113
						}`),
					},
					Decision: ai.PolicyDecision{
						Allowed: true,
						Reason:  "read tool is allowlisted",
					},
					Result: ai.ToolResult{
						Data: json.RawMessage(`{
							"balance_avax":
							"2.181886544100000121"
						}`),
						Source:      "mock_chain",
						ChainID:     43113,
						BlockNumber: &blockNumber,
						ObservedAt: time.Date(
							2026,
							time.October,
							28,
							0,
							0,
							0,
							0,
							time.UTC,
						),
					},
				},
			},
		},
	}

	handler, err := NewChatHandler(runner)
	if err != nil {
		t.Fatalf("NewChatHandler() returned error: %v", err)
	}

	app := fiber.New()
	app.Use(requestid.New())
	app.Post("/api/v1/chat", handler)

	request := httptest.NewRequest(
		fiber.MethodPost,
		"/api/v1/chat",
		strings.NewReader(`{
			"session_id": "demo-session-001",
			"wallet_address":
			"0x0000000000000000000000000000000000000001",
			"chain_id": 43113,
			"message": "Show my AVAX balance",
			"locale": "en"
		}`),
	)
	request.Header.Set(
		fiber.HeaderContentType,
		fiber.MIMEApplicationJSON,
	)

	response, err := app.Test(request)
	if err != nil {
		t.Fatalf("app.Test() returned error: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != fiber.StatusOK {
		t.Fatalf(
			"status = %d, want 200",
			response.StatusCode,
		)
	}

	var body ChatResponse

	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("could not decode response: %v", err)
	}

	if body.Message != runner.output.Message {
		t.Fatalf(
			"message = %q, want %q",
			body.Message,
			runner.output.Message,
		)
	}

	if len(body.ToolCalls) != 1 {
		t.Fatalf(
			"tool calls = %d, want 1",
			len(body.ToolCalls),
		)
	}

	if len(body.Sources) != 1 {
		t.Fatalf(
			"sources = %d, want 1",
			len(body.Sources),
		)
	}

	if runner.receivedCall != 1 {
		t.Fatalf(
			"runner call count = %d, want 1",
			runner.receivedCall,
		)
	}

	if runner.received.ChainID != 43113 {
		t.Fatalf(
			"received chain ID = %d, want 43113",
			runner.received.ChainID,
		)
	}

	if runner.received.Message != "Show my AVAX balance" {
		t.Fatalf(
			"received message = %q, want original message",
			runner.received.Message,
		)
	}
}

func TestNewChatHandlerRejectsNilRunner(t *testing.T) {
	_, err := NewChatHandler(nil)

	if err != ErrNilChatRunner {
		t.Fatalf(
			"NewChatHandler(nil) error = %v, want ErrNilChatRunner",
			err,
		)
	}
}
