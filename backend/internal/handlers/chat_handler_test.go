package handlers

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/requestid"
)

func TestHandleChat(t *testing.T) {
	testCases := []struct {
		name              string
		requestBody       string
		expectedStatus    int
		expectedErrorCode string
	}{
		{
			name: "valid request",
			requestBody: `{
				"session_id": "demo-session-001",
				"wallet_address": "0x0000000000000000000000000000000000000001",
				"chain_id": 43113,
				"message": "Hello",
				"locale": "en"
			}`,
			expectedStatus:    fiber.StatusOK,
			expectedErrorCode: "",
		},
		{
			name:              "invalid JSON",
			requestBody:       `{"session_id":`,
			expectedStatus:    fiber.StatusBadRequest,
			expectedErrorCode: "INVALID_JSON",
		},
		{
			name: "missing session ID",
			requestBody: `{
				"session_id": "",
				"wallet_address": "0x0000000000000000000000000000000000000001",
				"chain_id": 43113,
				"message": "Hello",
				"locale": "en"
			}`,
			expectedStatus:    fiber.StatusBadRequest,
			expectedErrorCode: "MISSING_SESSION_ID",
		},
		{
			name: "missing chain ID",
			requestBody: `{
				"session_id": "demo-session-001",
				"wallet_address": "0x0000000000000000000000000000000000000001",
				"message": "Hello",
				"locale": "en"
			}`,
			expectedStatus:    fiber.StatusBadRequest,
			expectedErrorCode: "MISSING_CHAIN_ID",
		},
		{
			name: "unsupported chain ID",
			requestBody: `{
				"session_id": "demo-session-001",
				"wallet_address": "0x0000000000000000000000000000000000000001",
				"chain_id": 43114,
				"message": "Hello",
				"locale": "en"
			}`,
			expectedStatus:    fiber.StatusBadRequest,
			expectedErrorCode: "UNSUPPORTED_CHAIN_ID",
		},
		{
			name: "invalid wallet address",
			requestBody: `{
				"session_id": "demo-session-001",
				"wallet_address": "not-a-wallet",
				"chain_id": 43113,
				"message": "Hello",
				"locale": "en"
			}`,
			expectedStatus:    fiber.StatusBadRequest,
			expectedErrorCode: "INVALID_WALLET_ADDRESS",
		},
		{
			name: "empty message",
			requestBody: `{
				"session_id": "demo-session-001",
				"wallet_address": "0x0000000000000000000000000000000000000001",
				"chain_id": 43113,
				"message": "     ",
				"locale": "vi"
			}`,
			expectedStatus:    fiber.StatusBadRequest,
			expectedErrorCode: "EMPTY_MESSAGE",
		},
		{
			name: "message too long",
			requestBody: `{
				"session_id": "demo-session-001",
				"wallet_address": "0x0000000000000000000000000000000000000001",
				"chain_id": 43113,
				"message": "` + strings.Repeat("a", 2001) + `",
				"locale": "en"
			}`,
			expectedStatus:    fiber.StatusBadRequest,
			expectedErrorCode: "MESSAGE_TOO_LONG",
		},
		{
			name: "invalid locale",
			requestBody: `{
				"session_id": "demo-session-001",
				"wallet_address": "0x0000000000000000000000000000000000000001",
				"chain_id": 43113,
				"message": "Hello",
				"locale": "fr"
			}`,
			expectedStatus:    fiber.StatusBadRequest,
			expectedErrorCode: "INVALID_LOCALE",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			app := fiber.New()
			app.Use(requestid.New())
			app.Post("/api/v1/chat", HandleChat)

			request := httptest.NewRequest(
				fiber.MethodPost,
				"/api/v1/chat",
				strings.NewReader(testCase.requestBody),
			)
			request.Header.Set(
				fiber.HeaderContentType,
				fiber.MIMEApplicationJSON,
			)

			response, err := app.Test(request)
			if err != nil {
				t.Fatalf("app.Test returned an error: %v", err)
			}
			defer response.Body.Close()

			if response.StatusCode != testCase.expectedStatus {
				t.Errorf(
					"expected status %d, got %d",
					testCase.expectedStatus,
					response.StatusCode,
				)
			}

			var responseBody ChatResponse

			if err := json.NewDecoder(response.Body).Decode(&responseBody); err != nil {
				t.Fatalf("could not decode response: %v", err)
			}

			if responseBody.RequestID == "" {
				t.Error("expected request_id to be present")
			}

			if testCase.expectedErrorCode == "" {
				if responseBody.Error != nil {
					t.Errorf(
						"expected no error, got %s",
						responseBody.Error.Code,
					)
				}

				if responseBody.Message != "Hello" {
					t.Errorf(
						"expected message Hello, got %s",
						responseBody.Message,
					)
				}

				return
			}

			if responseBody.Error == nil {
				t.Fatal("expected an error response")
			}

			if responseBody.Error.Code != testCase.expectedErrorCode {
				t.Errorf(
					"expected error code %s, got %s",
					testCase.expectedErrorCode,
					responseBody.Error.Code,
				)
			}
		})
	}
}
