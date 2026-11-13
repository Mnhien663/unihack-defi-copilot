package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"unihack-backend/internal/ai"
	"unihack-backend/internal/avalanche"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
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

	if runner.received.RequestID == "" {
		t.Fatal("agent received an empty request ID")
	}

	if runner.received.RequestID != body.RequestID {
		t.Fatalf(
			"agent request ID = %q, response request ID = %q",
			runner.received.RequestID,
			body.RequestID,
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

func TestInjectedChatHandlerReturnsPendingAction(t *testing.T) {
	runner := &mockChatRunner{
		output: ai.AgentOutput{
			Message:        "The action has been prepared but not executed.",
			ToolExecutions: []ai.ToolExecution{},
			PendingAction: &ai.PendingAction{
				RequestID:  "request-write-001",
				ToolCallID: "swap-call-001",
				ToolName:   "prepare_swap",
				Arguments: json.RawMessage(`{
					"chain_id": 43113,
					"amount": "1",
					"token_in": "AVAX",
					"token_out": "DDEMO"
				}`),
				ChainID:              43113,
				WalletAddress:        "0x0000000000000000000000000000000000000001",
				RequiresConfirmation: true,
			},
			RequiresConfirmation: true,
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
			"session_id": "write-session",
			"wallet_address":
			"0x0000000000000000000000000000000000000001",
			"chain_id": 43113,
			"message":
			"Prepare a swap of exactly 1 AVAX to DDEMO.",
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

	if body.PendingAction == nil {
		t.Fatal("response is missing pending_action")
	}

	if body.PendingAction.ToolName != "prepare_swap" {
		t.Fatalf(
			"pending tool = %q, want prepare_swap",
			body.PendingAction.ToolName,
		)
	}

	if body.PendingAction.ToolCallID != "swap-call-001" {
		t.Fatalf(
			"tool call ID = %q, want swap-call-001",
			body.PendingAction.ToolCallID,
		)
	}

	if !body.PendingAction.RequiresConfirmation {
		t.Fatal("pending action must require confirmation")
	}

	if len(body.ToolCalls) != 0 {
		t.Fatalf(
			"executed tool calls = %d, want 0",
			len(body.ToolCalls),
		)
	}

	if body.Error != nil {
		t.Fatalf(
			"response error = %+v, want nil",
			body.Error,
		)
	}
}

func TestChatHandlerMapsIdempotencyErrors(t *testing.T) {
	tests := []struct {
		name       string
		key        string
		agentErr   error
		wantStatus int
		wantCode   string
	}{
		{
			name:       "invalid key",
			key:        "invalid key",
			agentErr:   ai.ErrInvalidIdempotencyKey,
			wantStatus: fiber.StatusBadRequest,
			wantCode:   "INVALID_IDEMPOTENCY_KEY",
		},
		{
			name:       "same key different request",
			key:        "swap-test-key",
			agentErr:   ai.ErrSwapIdempotencyConflict,
			wantStatus: fiber.StatusConflict,
			wantCode:   "IDEMPOTENCY_KEY_CONFLICT",
		},
		{
			name:       "pending swap unavailable",
			key:        "swap-test-key",
			agentErr:   ai.ErrSwapPendingUnavailable,
			wantStatus: fiber.StatusConflict,
			wantCode:   "PENDING_SWAP_UNAVAILABLE",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			runner := &mockChatRunner{err: tc.agentErr}

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
                    "session_id": "swap-test-session",
                    "wallet_address": "0x0000000000000000000000000000000000000001",
                    "chain_id": 43113,
                    "message": "Prepare a swap on Avalanche Fuji",
                    "locale": "en"
                }`),
			)
			request.Header.Set(fiber.HeaderContentType, fiber.MIMEApplicationJSON)
			request.Header.Set("Idempotency-Key", tc.key)

			response, err := app.Test(request)
			if err != nil {
				t.Fatalf("app.Test() returned error: %v", err)
			}
			defer response.Body.Close()

			var body ChatResponse
			if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
				t.Fatalf("could not decode response: %v", err)
			}

			if response.StatusCode != tc.wantStatus {
				t.Errorf("status = %d, want %d", response.StatusCode, tc.wantStatus)
			}
			if body.Error == nil {
				t.Fatal("expected a structured error response")
			}
			if body.Error.Code != tc.wantCode {
				t.Errorf("error.code = %q, want %q", body.Error.Code, tc.wantCode)
			}
			if body.PendingAction != nil {
				t.Error("error response must not include a pending action")
			}
			if runner.receivedCall != 1 {
				t.Errorf("runner call count = %d, want 1", runner.receivedCall)
			}
			if runner.received.IdempotencyKey != tc.key {
				t.Errorf("agent received key %q, want %q", runner.received.IdempotencyKey, tc.key)
			}
		})
	}
}

func TestPendingDecisionSignatureBindsReview(t *testing.T) {
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}

	action := ai.PendingAction{
		PendingActionID:      "action-1",
		WalletAddress:        crypto.PubkeyToAddress(key.PublicKey).Hex(),
		ChainID:              43113,
		NormalizedIntentHash: "review-1",
		ExpiresAt:            "2026-11-20T00:00:00Z",
	}
	const domain = "copilot.example"
	const sessionID = "session-1"

	message, err := pendingDecisionMessage(domain, "confirm", sessionID, action)
	if err != nil {
		t.Fatal(err)
	}
	sig, err := crypto.Sign(accounts.TextHash([]byte(message)), key)
	if err != nil {
		t.Fatal(err)
	}

	// Exercise the 27/28 form accepted from an EOA wallet.
	sig[64] += 27
	signature := hexutil.Encode(sig)
	if !verifyPendingDecisionSignature(message, action.WalletAddress, signature) {
		t.Fatal("valid owner signature was rejected")
	}

	for _, tc := range []struct {
		name   string
		mutate func(*string, *string, *string, *ai.PendingAction)
	}{
		{"decision", func(_ *string, d *string, _ *string, _ *ai.PendingAction) {
			*d = "reject"
		}},
		{"domain", func(d *string, _ *string, _ *string, _ *ai.PendingAction) {
			*d = "other.example"
		}},
		{"session", func(_ *string, _ *string, s *string, _ *ai.PendingAction) {
			*s = "session-2"
		}},
		{"action ID", func(_, _, _ *string, a *ai.PendingAction) {
			a.PendingActionID = "action-2"
		}},
		{"chain", func(_, _, _ *string, a *ai.PendingAction) {
			a.ChainID = 43114
		}},
		{"review hash", func(_, _, _ *string, a *ai.PendingAction) {
			a.NormalizedIntentHash = "review-2"
		}},
		{"expiry", func(_, _, _ *string, a *ai.PendingAction) {
			a.ExpiresAt = "2026-11-20T00:01:00Z"
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := domain
			decision := "confirm"
			session := sessionID
			changed := action
			tc.mutate(&d, &decision, &session, &changed)

			otherMessage, err := pendingDecisionMessage(d, decision, session, changed)
			if err != nil {
				t.Fatal(err)
			}
			if verifyPendingDecisionSignature(otherMessage, action.WalletAddress, signature) {
				t.Fatal("original signature must not authorize a changed message")
			}
		})
	}

	const otherWallet = "0x0000000000000000000000000000000000000002"
	if verifyPendingDecisionSignature(message, otherWallet, signature) {
		t.Fatal("signature must not authenticate a different wallet")
	}
}

type pendingChallengeTestStore struct {
	action      *ai.PendingAction
	sessionID   string
	unavailable bool
	lookupCalls int
	loadCalls   int
}

func (s *pendingChallengeTestStore) LookupPendingSwapOwner(
	_ context.Context,
	actionID, sessionID string,
	chainID uint64,
	reviewHash string,
) (string, error) {
	s.lookupCalls++
	if s.unavailable ||
		actionID != s.action.PendingActionID ||
		sessionID != s.sessionID ||
		chainID != s.action.ChainID ||
		reviewHash != s.action.NormalizedIntentHash {
		return "", nil
	}
	return s.action.WalletAddress, nil
}

func (s *pendingChallengeTestStore) LoadPendingSwapForReview(
	_ context.Context,
	actionID, sessionID, wallet string,
	chainID uint64,
	reviewHash string,
) (*ai.PendingAction, error) {
	s.loadCalls++
	if s.unavailable ||
		actionID != s.action.PendingActionID ||
		sessionID != s.sessionID ||
		wallet != s.action.WalletAddress ||
		chainID != s.action.ChainID ||
		reviewHash != s.action.NormalizedIntentHash {
		return nil, nil
	}
	return s.action, nil
}

func TestPendingDecisionChallengeHandler(t *testing.T) {
	const actionID = "00000000-0000-4000-8000-000000000001"
	const sessionID = "session-test"
	const domain = "copilot.example"

	action := &ai.PendingAction{
		PendingActionID:      actionID,
		WalletAddress:        "0x0000000000000000000000000000000000000001",
		ChainID:              43113,
		NormalizedIntentHash: strings.Repeat("a", 64),
		ExpiresAt:            "2026-11-20T00:00:00Z",
		Arguments:            json.RawMessage(`{"transaction":{"data":"0x1234"}}`),
	}

	for _, tc := range []struct {
		name        string
		session     string
		decision    string
		unavailable bool
		wantStatus  int
		wantCode    string
		wantLookup  int
		wantLoad    int
	}{
		{"valid confirm", sessionID, "confirm", false, 200, "", 1, 1},
		{"valid reject", sessionID, "reject", false, 200, "", 1, 1},
		{"wrong session", "other-session", "confirm", false, 404, "PENDING_ACTION_UNAVAILABLE", 1, 0},
		{"expired action", sessionID, "confirm", true, 404, "PENDING_ACTION_UNAVAILABLE", 1, 0},
		{"invalid decision", sessionID, "submit", false, 400, "INVALID_DECISION_REQUEST", 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &pendingChallengeTestStore{
				action:      action,
				sessionID:   sessionID,
				unavailable: tc.unavailable,
			}

			handler, err := NewPendingDecisionChallengeHandler(store, domain)
			if err != nil {
				t.Fatal(err)
			}

			app := fiber.New()
			app.Post("/pending-actions/:id/challenge", handler)

			payload, err := json.Marshal(pendingDecisionChallengeRequest{
				SessionID:  tc.session,
				ReviewHash: action.NormalizedIntentHash,
				Decision:   tc.decision,
			})
			if err != nil {
				t.Fatal(err)
			}

			req := httptest.NewRequest(
				fiber.MethodPost,
				"/pending-actions/"+actionID+"/challenge",
				strings.NewReader(string(payload)),
			)
			req.Header.Set(fiber.HeaderContentType, fiber.MIMEApplicationJSON)

			res, err := app.Test(req)
			if err != nil {
				t.Fatal(err)
			}
			defer res.Body.Close()

			if res.StatusCode != tc.wantStatus {
				t.Fatalf("status = %d, want %d", res.StatusCode, tc.wantStatus)
			}
			if store.lookupCalls != tc.wantLookup || store.loadCalls != tc.wantLoad {
				t.Fatalf("lookup/load calls = %d/%d, want %d/%d",
					store.lookupCalls, store.loadCalls, tc.wantLookup, tc.wantLoad)
			}

			if tc.wantCode != "" {
				var body ChatResponse
				if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
					t.Fatal(err)
				}
				if body.Error == nil || body.Error.Code != tc.wantCode {
					t.Fatalf("error = %+v, want code %q", body.Error, tc.wantCode)
				}
				return
			}

			var body map[string]string
			if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}

			wantMessage, err := pendingDecisionMessage(domain, tc.decision, sessionID, *action)
			if err != nil {
				t.Fatal(err)
			}
			if len(body) != 2 || body["message"] != wantMessage ||
				body["expires_at"] != action.ExpiresAt {
				t.Fatalf("expected only the exact signing message and expiry, got %+v", body)
			}
			if res.Header.Get("Cache-Control") != "no-store" {
				t.Fatal("challenge response must not be cached")
			}
		})
	}
}

type pendingDecisionTestStore struct {
	pendingChallengeTestStore
	changed      bool
	confirmCalls int
	rejectCalls  int
}

func (s *pendingDecisionTestStore) ConfirmPendingSwap(
	_ context.Context, _, _, _ string, _ uint64, _ string,
) (bool, error) {
	s.confirmCalls++
	return s.changed, nil
}

func (s *pendingDecisionTestStore) RejectPendingSwap(
	_ context.Context, _, _, _ string, _ uint64, _ string,
) (bool, error) {
	s.rejectCalls++
	return s.changed, nil
}

type pendingDecisionTestRevalidator func() (bool, error)

func (f pendingDecisionTestRevalidator) RevalidateReviewedSwap(
	_ context.Context,
	_ avalanche.UnsignedTx,
	_ avalanche.SwapTxSummary,
	_ uint16,
) (bool, error) {
	return f()
}

func TestPendingDecisionHandler(t *testing.T) {
	const actionID = "00000000-0000-4000-8000-000000000001"
	const sessionID = "session-test"
	const domain = "copilot.example"

	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}

	action := &ai.PendingAction{
		PendingActionID:      actionID,
		WalletAddress:        crypto.PubkeyToAddress(key.PublicKey).Hex(),
		ChainID:              43113,
		NormalizedIntentHash: strings.Repeat("a", 64),
		ExpiresAt:            "2026-11-20T00:00:00Z",
		Arguments:            json.RawMessage(`{"transaction":{"data":"0x1234"},"summary":{},"slippage_bps":50}`),
	}

	for _, tc := range []struct {
		name           string
		routeDecision  string
		signedDecision string
		needsReview    bool
		revalidateErr  error
		changed        bool
		wantHTTP       int
		wantCode       string
		wantStatus     string
		wantRevalidate int
		wantConfirm    int
		wantReject     int
	}{
		{
			name:          "confirm signature cannot reject",
			routeDecision: "reject", signedDecision: "confirm",
			changed: true, wantHTTP: 403, wantCode: "INVALID_DECISION_SIGNATURE",
		},
		{
			name:          "changed quote requires review",
			routeDecision: "confirm", signedDecision: "confirm",
			needsReview: true, wantHTTP: 409, wantCode: "SWAP_REVIEW_REQUIRED",
			wantRevalidate: 1,
		},
		{
			name:           "revalidation timeout does not confirm",
			routeDecision:  "confirm",
			signedDecision: "confirm",
			revalidateErr:  context.DeadlineExceeded,
			wantHTTP:       504,
			wantCode:       "PENDING_ACTION_CONFIRM_TIMEOUT",
			wantRevalidate: 1,
		},
		{
			name:          "lost DB transition returns no transaction",
			routeDecision: "confirm", signedDecision: "confirm",
			wantHTTP: 409, wantCode: "PENDING_ACTION_UNAVAILABLE",
			wantRevalidate: 1, wantConfirm: 1,
		},
		{
			name:          "confirmed returns stored transaction",
			routeDecision: "confirm", signedDecision: "confirm",
			changed: true, wantHTTP: 200, wantStatus: "confirmed",
			wantRevalidate: 1, wantConfirm: 1,
		},
		{
			name:          "rejected returns no transaction",
			routeDecision: "reject", signedDecision: "reject",
			changed: true, wantHTTP: 200, wantStatus: "rejected",
			wantReject: 1,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &pendingDecisionTestStore{
				pendingChallengeTestStore: pendingChallengeTestStore{
					action:    action,
					sessionID: sessionID,
				},
				changed: tc.changed,
			}

			revalidateCalls := 0
			revalidator := pendingDecisionTestRevalidator(func() (bool, error) {
				revalidateCalls++
				if store.confirmCalls != 0 || store.rejectCalls != 0 {
					t.Fatal("DB transition ran before revalidation")
				}
				return tc.needsReview, tc.revalidateErr
			})

			handler, err := NewPendingDecisionHandler(
				store, revalidator, domain, tc.routeDecision,
			)
			if err != nil {
				t.Fatal(err)
			}
			app := fiber.New()
			app.Post("/pending-actions/:id/"+tc.routeDecision, handler)

			message, err := pendingDecisionMessage(domain, tc.signedDecision, sessionID, *action)
			if err != nil {
				t.Fatal(err)
			}
			sig, err := crypto.Sign(accounts.TextHash([]byte(message)), key)
			if err != nil {
				t.Fatal(err)
			}
			payload, err := json.Marshal(pendingDecisionRequest{
				SessionID:  sessionID,
				ReviewHash: action.NormalizedIntentHash,
				Signature:  hexutil.Encode(sig),
			})
			if err != nil {
				t.Fatal(err)
			}

			req := httptest.NewRequest(fiber.MethodPost,
				"/pending-actions/"+actionID+"/"+tc.routeDecision,
				strings.NewReader(string(payload)))
			req.Header.Set(fiber.HeaderContentType, fiber.MIMEApplicationJSON)

			res, err := app.Test(req)
			if err != nil {
				t.Fatal(err)
			}
			defer res.Body.Close()

			var body struct {
				ChatResponse
				ActionID  string          `json:"pending_action_id"`
				Status    string          `json:"status"`
				Arguments json.RawMessage `json:"arguments"`
			}
			if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}

			if res.StatusCode != tc.wantHTTP {
				t.Fatalf("HTTP status = %d, want %d; error=%+v",
					res.StatusCode, tc.wantHTTP, body.Error)
			}
			if revalidateCalls != tc.wantRevalidate ||
				store.confirmCalls != tc.wantConfirm ||
				store.rejectCalls != tc.wantReject {
				t.Fatalf("revalidate/confirm/reject calls = %d/%d/%d, want %d/%d/%d",
					revalidateCalls, store.confirmCalls, store.rejectCalls,
					tc.wantRevalidate, tc.wantConfirm, tc.wantReject)
			}

			if tc.wantCode != "" {
				if body.Error == nil || body.Error.Code != tc.wantCode {
					t.Fatalf("error = %+v, want code %q", body.Error, tc.wantCode)
				}
			} else if body.Error != nil || body.ActionID != actionID || body.Status != tc.wantStatus {
				t.Fatalf("unexpected decision response: %+v", body)
			}

			if tc.wantStatus == "confirmed" {
				if string(body.Arguments) != string(action.Arguments) {
					t.Fatalf("arguments = %s, want stored payload %s", body.Arguments, action.Arguments)
				}
			} else if len(body.Arguments) != 0 || body.PendingAction != nil {
				t.Fatal("reject or error response must not return transaction payload")
			}
		})
	}
}

func TestPendingSwapReceiptSignatureBindsOperationAndHash(t *testing.T) {
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}

	const domain = "copilot.example"
	const sessionID = "session-test"
	txHash := "0x" + strings.Repeat("a", 64)
	expiresAt := time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC)

	action := ai.PendingAction{
		PendingActionID:      "action-test",
		WalletAddress:        crypto.PubkeyToAddress(key.PublicKey).Hex(),
		ChainID:              43113,
		NormalizedIntentHash: strings.Repeat("c", 64),
	}

	message, err := pendingSwapReceiptMessage(
		domain, "submit-hash", sessionID, action, txHash, expiresAt,
	)
	if err != nil {
		t.Fatal(err)
	}
	sig, err := crypto.Sign(accounts.TextHash([]byte(message)), key)
	if err != nil {
		t.Fatal(err)
	}
	signature := hexutil.Encode(sig)

	for _, tc := range []struct {
		name      string
		operation string
		hash      string
		expiry    time.Time
		wantValid bool
	}{
		{"original message", "submit-hash", txHash, expiresAt, true},
		{"different hash", "submit-hash", "0x" + strings.Repeat("b", 64), expiresAt, false},
		{"different operation", "refresh-receipt", txHash, expiresAt, false},
		{"extended signature expiry", "submit-hash", txHash, expiresAt.Add(time.Minute), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			text, err := pendingSwapReceiptMessage(
				domain, tc.operation, sessionID, action, tc.hash, tc.expiry,
			)
			if err != nil {
				t.Fatal(err)
			}

			valid := verifyPendingDecisionSignature(text, action.WalletAddress, signature)
			if valid != tc.wantValid {
				t.Fatalf("signature valid = %t, want %t", valid, tc.wantValid)
			}
		})
	}
}

type signedReceiptHTTPTestStore struct {
	action        *ai.PendingAction
	loadCalls     int
	attachCalls   int
	storedHash    string
	finalizeCalls int
	finalizedHash string
}

func (s *signedReceiptHTTPTestStore) LoadConfirmedSwapForReceipt(
	_ context.Context, _, _, _ string, _ uint64, _ string,
) (*ai.PendingAction, string, string, error) {
	s.loadCalls++
	if s.storedHash != "" {
		return s.action, "submitted", s.storedHash, nil
	}
	return s.action, "confirmed", "", nil
}

func (s *signedReceiptHTTPTestStore) AttachVerifiedPendingSwapHash(
	_ context.Context, _, _, _ string, _ uint64, _, _ string,
) (bool, error) {
	s.attachCalls++
	return true, nil
}

func (s *signedReceiptHTTPTestStore) FinalizeVerifiedPendingSwapReceipt(
	_ context.Context, _, _, _ string, _ uint64, _, hash, outcome string,
) (bool, error) {
	s.finalizeCalls++
	s.finalizedHash = hash
	return outcome == "mined", nil
}

type signedReceiptHTTPTestReader func(string) (avalanche.ReceiptResult, error)

func (f signedReceiptHTTPTestReader) ReadReceipt(
	_ context.Context, hash string,
) (avalanche.ReceiptResult, error) {
	return f(hash)
}

func TestPendingSwapReceiptHandlerSubmitHash(t *testing.T) {
	const actionID = "00000000-0000-4000-8000-000000000001"
	const sessionID = "session-test"
	const domain = "copilot.example"
	const operation = "submit-hash"
	const manager = "0x0000000000000000000000000000000000000002"
	const calldata = "0x12345678"

	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}

	wallet := crypto.PubkeyToAddress(key.PublicKey).Hex()
	txHash := "0x" + strings.Repeat("a", 64)
	otherHash := "0x" + strings.Repeat("b", 64)
	payload, err := json.Marshal(struct {
		Transaction avalanche.UnsignedTx `json:"transaction"`
	}{
		Transaction: avalanche.UnsignedTx{
			ChainID: 43113,
			From:    wallet,
			To:      manager,
			Value:   "0",
			Data:    calldata,
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	action := &ai.PendingAction{
		PendingActionID:      actionID,
		WalletAddress:        wallet,
		ChainID:              43113,
		NormalizedIntentHash: strings.Repeat("c", 64),
		Arguments:            payload,
	}

	for _, tc := range []struct {
		name       string
		signedHash string
		expiry     time.Duration
		wantHTTP   int
		wantCode   string
		wantLoads  int
		wantRPC    int
		wantAttach int
	}{
		{
			name: "valid signature but expired", signedHash: txHash,
			expiry: -time.Minute, wantHTTP: 403, wantCode: "RECEIPT_SIGNATURE_EXPIRED",
		},
		{
			name: "signature for a different hash", signedHash: otherHash,
			expiry: time.Minute, wantHTTP: 403, wantCode: "INVALID_RECEIPT_SIGNATURE",
			wantLoads: 1,
		},
		{
			name: "valid signature attaches verified hash", signedHash: txHash,
			expiry: time.Minute, wantHTTP: 200,
			wantLoads: 2, wantRPC: 1, wantAttach: 1,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &signedReceiptHTTPTestStore{action: action}
			rpcCalls := 0
			to := manager

			reader := signedReceiptHTTPTestReader(func(hash string) (avalanche.ReceiptResult, error) {
				rpcCalls++
				if hash != txHash {
					t.Fatalf("RPC hash = %q, want %q", hash, txHash)
				}
				return avalanche.ReceiptResult{
					TransactionHash: hash,
					FromAddress:     wallet,
					ToAddress:       &to,
					Input:           calldata,
					ValueRaw:        "0",
					ChainID:         43113,
					Status:          avalanche.TransactionStatusPending,
				}, nil
			})

			handler, err := NewPendingSwapReceiptHandler(store, reader, domain, operation)
			if err != nil {
				t.Fatal(err)
			}
			app := fiber.New()
			app.Post("/pending-actions/:id/submit-hash", handler)

			expiresAt := time.Now().UTC().Add(tc.expiry)
			message, err := pendingSwapReceiptMessage(
				domain, operation, sessionID, *action, tc.signedHash, expiresAt,
			)
			if err != nil {
				t.Fatal(err)
			}
			sig, err := crypto.Sign(accounts.TextHash([]byte(message)), key)
			if err != nil {
				t.Fatal(err)
			}

			body, err := json.Marshal(map[string]string{
				"session_id":           sessionID,
				"review_hash":          action.NormalizedIntentHash,
				"wallet_address":       wallet,
				"transaction_hash":     txHash,
				"signature":            hexutil.Encode(sig),
				"signature_expires_at": expiresAt.Format(time.RFC3339Nano),
			})
			if err != nil {
				t.Fatal(err)
			}

			req := httptest.NewRequest(fiber.MethodPost,
				"/pending-actions/"+actionID+"/submit-hash",
				strings.NewReader(string(body)))
			req.Header.Set(fiber.HeaderContentType, fiber.MIMEApplicationJSON)

			res, err := app.Test(req)
			if err != nil {
				t.Fatal(err)
			}
			defer res.Body.Close()

			var got struct {
				ChatResponse
				ActionID string `json:"pending_action_id"`
				Hash     string `json:"transaction_hash"`
				Status   string `json:"status"`
			}
			if err := json.NewDecoder(res.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}

			if res.StatusCode != tc.wantHTTP {
				t.Fatalf("HTTP status = %d, want %d; error=%+v",
					res.StatusCode, tc.wantHTTP, got.Error)
			}
			if store.loadCalls != tc.wantLoads || rpcCalls != tc.wantRPC ||
				store.attachCalls != tc.wantAttach {
				t.Fatalf("load/RPC/attach = %d/%d/%d, want %d/%d/%d",
					store.loadCalls, rpcCalls, store.attachCalls,
					tc.wantLoads, tc.wantRPC, tc.wantAttach)
			}

			if store.finalizeCalls != 0 {
				t.Fatal("submit-hash must not finalize a receipt")
			}

			if tc.wantCode != "" {
				if got.Error == nil || got.Error.Code != tc.wantCode {
					t.Fatalf("error = %+v, want code %q", got.Error, tc.wantCode)
				}
				if got.Hash != "" || got.Status != "" {
					t.Fatal("rejected request must not return a submission result")
				}
				return
			}

			if got.Error != nil || got.ActionID != actionID ||
				got.Hash != txHash || got.Status != "submitted" {
				t.Fatalf("unexpected submission response: %+v", got)
			}
		})
	}
}

func TestPendingSwapReceiptHandlerRefreshUsesStoredHash(t *testing.T) {
	const actionID = "00000000-0000-4000-8000-000000000001"
	const sessionID = "session-test"
	const domain = "copilot.example"
	const operation = "refresh-receipt"
	const calldata = "0x12345678"

	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}

	wallet := crypto.PubkeyToAddress(key.PublicKey).Hex()
	manager := "0x0000000000000000000000000000000000000002"
	storedHash := "0x" + strings.Repeat("a", 64)
	blockHash := "0x" + strings.Repeat("b", 64)
	blockNumber := uint64(100)

	payload, err := json.Marshal(struct {
		Transaction avalanche.UnsignedTx `json:"transaction"`
	}{
		Transaction: avalanche.UnsignedTx{
			ChainID: 43113, From: wallet, To: manager,
			Value: "0", Data: calldata,
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	action := &ai.PendingAction{
		PendingActionID:      actionID,
		WalletAddress:        wallet,
		ChainID:              43113,
		NormalizedIntentHash: strings.Repeat("c", 64),
		Arguments:            payload,
	}

	for _, tc := range []struct {
		name        string
		includeHash bool
		wantHTTP    int
	}{
		{"client cannot supply a hash", true, 400},
		{"valid signature refreshes the stored hash", false, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &signedReceiptHTTPTestStore{
				action:     action,
				storedHash: storedHash,
			}
			rpcCalls := 0
			reader := signedReceiptHTTPTestReader(func(hash string) (avalanche.ReceiptResult, error) {
				rpcCalls++
				if hash != storedHash {
					t.Fatalf("RPC hash = %q, want stored hash %q", hash, storedHash)
				}
				return avalanche.ReceiptResult{
					TransactionHash: storedHash,
					FromAddress:     wallet,
					ToAddress:       &manager,
					Input:           calldata,
					ValueRaw:        "0",
					ChainID:         43113,
					BlockNumber:     &blockNumber,
					BlockHash:       &blockHash,
					Status:          avalanche.TransactionStatusSucceeded,
				}, nil
			})

			handler, err := NewPendingSwapReceiptHandler(store, reader, domain, operation)
			if err != nil {
				t.Fatal(err)
			}
			app := fiber.New()
			app.Post("/pending-actions/:id/refresh-receipt", handler)

			expiresAt := time.Now().UTC().Add(time.Minute)
			message, err := pendingSwapReceiptMessage(
				domain, operation, sessionID, *action, storedHash, expiresAt,
			)
			if err != nil {
				t.Fatal(err)
			}
			sig, err := crypto.Sign(accounts.TextHash([]byte(message)), key)
			if err != nil {
				t.Fatal(err)
			}

			fields := map[string]string{
				"session_id":           sessionID,
				"review_hash":          action.NormalizedIntentHash,
				"wallet_address":       wallet,
				"signature":            hexutil.Encode(sig),
				"signature_expires_at": expiresAt.Format(time.RFC3339Nano),
			}
			if tc.includeHash {
				fields["transaction_hash"] = "0x" + strings.Repeat("d", 64)
			}
			body, err := json.Marshal(fields)
			if err != nil {
				t.Fatal(err)
			}

			req := httptest.NewRequest(fiber.MethodPost,
				"/pending-actions/"+actionID+"/refresh-receipt",
				strings.NewReader(string(body)))
			req.Header.Set(fiber.HeaderContentType, fiber.MIMEApplicationJSON)

			res, err := app.Test(req)
			if err != nil {
				t.Fatal(err)
			}
			defer res.Body.Close()

			var got struct {
				ChatResponse
				ActionID string `json:"pending_action_id"`
				Hash     string `json:"transaction_hash"`
				Status   string `json:"status"`
			}
			if err := json.NewDecoder(res.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			if res.StatusCode != tc.wantHTTP {
				t.Fatalf("status = %d, want %d; error=%+v",
					res.StatusCode, tc.wantHTTP, got.Error)
			}
			if store.attachCalls != 0 {
				t.Fatal("refresh must not attach a hash")
			}

			if tc.includeHash {
				if got.Error == nil || got.Error.Code != "INVALID_RECEIPT_REQUEST" ||
					got.Hash != "" || got.Status != "" ||
					store.loadCalls != 0 || rpcCalls != 0 || store.finalizeCalls != 0 {
					t.Fatal("client-supplied hash must be rejected before DB or RPC")
				}
				return
			}

			if got.Error != nil || got.ActionID != actionID ||
				got.Hash != storedHash || got.Status != "mined" {
				t.Fatalf("unexpected receipt response: %+v", got)
			}
			if rpcCalls != 1 || store.finalizeCalls != 1 ||
				store.finalizedHash != storedHash {
				t.Fatalf("RPC/finalize calls = %d/%d; finalized hash=%q",
					rpcCalls, store.finalizeCalls, store.finalizedHash)
			}
		})
	}
}

func TestChatHandlerReturnsLLMRateLimited(t *testing.T) {
	runner := &mockChatRunner{err: ai.ErrLLMRateLimited}
	handler, err := NewChatHandler(runner)
	if err != nil {
		t.Fatal(err)
	}

	app := fiber.New()
	app.Use(requestid.New())
	app.Post("/api/v1/chat", handler)

	req := httptest.NewRequest(fiber.MethodPost, "/api/v1/chat", strings.NewReader(`{
		"session_id": "rate-limit-test",
		"wallet_address": "0x0000000000000000000000000000000000000001",
		"chain_id": 43113,
		"message": "Show my AVAX balance",
		"locale": "en"
	}`))
	req.Header.Set(fiber.HeaderContentType, fiber.MIMEApplicationJSON)

	res, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()

	var body ChatResponse
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}

	if res.StatusCode != fiber.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; error = %+v", res.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "LLM_RATE_LIMITED" {
		t.Fatalf("error = %+v, want LLM_RATE_LIMITED", body.Error)
	}
	if runner.receivedCall != 1 || body.RequestID == "" ||
		body.RequestID != runner.received.RequestID {
		t.Fatal("expected one agent call and matching non-empty request IDs")
	}
	if body.PendingAction != nil {
		t.Fatal("rate-limited response must not contain a pending action")
	}
}

func TestChatHandlerReturnsStructuredAgentError(t *testing.T) {
	const internalError = "database unavailable: internal test detail"

	runner := &mockChatRunner{err: errors.New(internalError)}
	handler, err := NewChatHandler(runner)
	if err != nil {
		t.Fatal(err)
	}

	app := fiber.New()
	app.Use(requestid.New())
	app.Post("/api/v1/chat", handler)

	req := httptest.NewRequest(fiber.MethodPost, "/api/v1/chat", strings.NewReader(`{
		"session_id": "db-unavailable-test",
		"wallet_address": "0x0000000000000000000000000000000000000001",
		"chain_id": 43113,
		"message": "Show my AVAX balance",
		"locale": "en"
	}`))
	req.Header.Set(fiber.HeaderContentType, fiber.MIMEApplicationJSON)

	res, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()

	var body ChatResponse
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if res.StatusCode != fiber.StatusBadGateway {
		t.Fatalf("status = %d, want 502", res.StatusCode)
	}
	if body.Error == nil || body.Error.Code != "AGENT_ERROR" {
		t.Fatalf("error = %+v, want AGENT_ERROR", body.Error)
	}
	if body.Error.Message != "the AI agent could not complete the request" {
		t.Fatalf("unexpected public error message: %q", body.Error.Message)
	}
	if runner.receivedCall != 1 || body.RequestID == "" ||
		body.RequestID != runner.received.RequestID {
		t.Fatal("expected one agent call and matching non-empty request IDs")
	}
	if body.PendingAction != nil {
		t.Fatal("agent error must not return a pending action")
	}
}

func TestChatRejectsOversizedBodyBeforeAgent(t *testing.T) {
	const bodyLimit = 32 * 1024

	handler, err := NewChatHandler(&mockChatRunner{})
	if err != nil {
		t.Fatal(err)
	}

	var routeCalls atomic.Int32
	app := fiber.New(fiber.Config{
		BodyLimit:             bodyLimit,
		ErrorHandler:          ErrorHandler,
		DisableStartupMessage: true,
	})
	app.Use(requestid.New())
	app.Post("/api/v1/chat", func(c *fiber.Ctx) error {
		routeCalls.Add(1)
		return handler(c)
	})

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen on loopback: %v", err)
	}
	defer listener.Close()

	go func() {
		_ = app.Listener(listener)
	}()

	body := `{
		"session_id": "body-limit-test",
		"wallet_address": "0x0000000000000000000000000000000000000001",
		"chain_id": 43113,
		"message": "Hello",
		"locale": "en"
	}`
	body += strings.Repeat(" ", bodyLimit+1-len(body))

	client := &http.Client{Timeout: 3 * time.Second}
	res, err := client.Post(
		"http://"+listener.Addr().String()+"/api/v1/chat",
		fiber.MIMEApplicationJSON,
		strings.NewReader(body),
	)
	if err != nil {
		t.Fatalf("send oversized request: %v", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", res.StatusCode)
	}
	if calls := routeCalls.Load(); calls != 0 {
		t.Fatalf("chat handler calls = %d, want 0", calls)
	}

	var responseBody ChatResponse
	if err := json.NewDecoder(res.Body).Decode(&responseBody); err != nil {
		t.Fatalf("decode 413 response: %v", err)
	}
	if responseBody.Error == nil || responseBody.Error.Code != "REQUEST_BODY_TOO_LARGE" {
		t.Fatalf("error = %+v, want REQUEST_BODY_TOO_LARGE", responseBody.Error)
	}
	if responseBody.PendingAction != nil {
		t.Fatal("oversized request must not return a pending action")
	}
}
