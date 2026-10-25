package handlers

import (
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/gofiber/fiber/v2"
)

const maxMessageLength = 2000

type ChatRequest struct {
	SessionID     string `json:"session_id"`
	WalletAddress string `json:"wallet_address"`
	Message       string `json:"message"`
	Locale        string `json:"locale"`
}

type ErrorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type ChatResponse struct {
	Message       string       `json:"message"`
	ToolCalls     []any        `json:"tool_calls"`
	PendingAction any          `json:"pending_action"`
	Sources       []any        `json:"sources"`
	RequestID     string       `json:"request_id"`
	Error         *ErrorDetail `json:"error"`
}

func getRequestID(c *fiber.Ctx) string {
	requestID, ok := c.Locals("requestid").(string)
	if !ok {
		return ""
	}

	return requestID
}

func sendError(
	c *fiber.Ctx,
	statusCode int,
	errorCode string,
	errorMessage string,
) error {
	return c.Status(statusCode).JSON(ChatResponse{
		Message:       "",
		ToolCalls:     []any{},
		PendingAction: nil,
		Sources:       []any{},
		RequestID:     getRequestID(c),
		Error: &ErrorDetail{
			Code:    errorCode,
			Message: errorMessage,
		},
	})
}

func validateChatRequest(req *ChatRequest) (string, string) {
	req.SessionID = strings.TrimSpace(req.SessionID)
	req.WalletAddress = strings.TrimSpace(req.WalletAddress)
	req.Message = strings.TrimSpace(req.Message)
	req.Locale = strings.ToLower(strings.TrimSpace(req.Locale))

	if req.SessionID == "" {
		return "MISSING_SESSION_ID", "session_id is required"
	}

	if !common.IsHexAddress(req.WalletAddress) {
		return "INVALID_WALLET_ADDRESS", "wallet_address is invalid"
	}

	if req.Message == "" {
		return "EMPTY_MESSAGE", "message is required"
	}

	if len([]rune(req.Message)) > maxMessageLength {
		return "MESSAGE_TOO_LONG", "message must not exceed 2000 characters"
	}

	if req.Locale != "vi" && req.Locale != "en" {
		return "INVALID_LOCALE", "locale must be vi or en"
	}

	return "", ""
}

func HandleChat(c *fiber.Ctx) error {
	req := new(ChatRequest)

	if err := c.BodyParser(req); err != nil {
		return sendError(
			c,
			fiber.StatusBadRequest,
			"INVALID_JSON",
			"request body must be valid JSON",
		)
	}

	errorCode, errorMessage := validateChatRequest(req)
	if errorCode != "" {
		return sendError(
			c,
			fiber.StatusBadRequest,
			errorCode,
			errorMessage,
		)
	}

	// TODO: Replace this echo response with the AI agent loop.
	return c.Status(fiber.StatusOK).JSON(ChatResponse{
		Message:       req.Message,
		ToolCalls:     []any{},
		PendingAction: nil,
		Sources:       []any{},
		RequestID:     getRequestID(c),
		Error:         nil,
	})
}
