package handlers

import (
	"context"
	"errors"
	"strings"

	"unihack-backend/internal/ai"
	"unihack-backend/internal/avalanche"

	"github.com/ethereum/go-ethereum/common"
	"github.com/gofiber/fiber/v2"
)

const maxMessageLength = 2000

var ErrNilChatRunner = errors.New("chat runner is nil")

type ChatRequest struct {
	SessionID     string `json:"session_id"`
	WalletAddress string `json:"wallet_address"`
	ChainID       int64  `json:"chain_id"`
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

// ChatRunner is the HTTP boundary for the AI application layer.
type ChatRunner interface {
	Run(
		ctx context.Context,
		input ai.AgentInput,
	) (ai.AgentOutput, error)
}

// echoChatRunner temporarily preserves the old endpoint behavior.
// main.go will be switched to an injected Agent after mock wiring is ready.
type echoChatRunner struct{}

func (echoChatRunner) Run(
	ctx context.Context,
	input ai.AgentInput,
) (ai.AgentOutput, error) {
	if err := ctx.Err(); err != nil {
		return ai.AgentOutput{}, err
	}

	return ai.AgentOutput{
		Message:        input.Message,
		ToolExecutions: []ai.ToolExecution{},
	}, nil
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

	if req.ChainID == 0 {
		return "MISSING_CHAIN_ID", "chain_id is required"
	}

	if req.ChainID != avalanche.FujiChainID {
		return "UNSUPPORTED_CHAIN_ID", "chain_id must be 43113 for Avalanche Fuji"
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

func NewChatHandler(
	runner ChatRunner,
) (fiber.Handler, error) {
	if runner == nil {
		return nil, ErrNilChatRunner
	}

	return func(c *fiber.Ctx) error {
		return handleChat(c, runner)
	}, nil
}

// HandleChat preserves compatibility until main.go injects an Agent.
func HandleChat(c *fiber.Ctx) error {
	return handleChat(c, echoChatRunner{})
}

func handleChat(
	c *fiber.Ctx,
	runner ChatRunner,
) error {
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

	output, err := runner.Run(
		c.UserContext(),
		ai.AgentInput{
			RequestID:     getRequestID(c),
			SessionID:     req.SessionID,
			WalletAddress: req.WalletAddress,
			ChainID:       uint64(req.ChainID),
			Message:       req.Message,
			Locale:        req.Locale,
		},
	)
	if err != nil {
		switch {
		case errors.Is(err, context.DeadlineExceeded):
			return sendError(
				c,
				fiber.StatusRequestTimeout,
				"REQUEST_TIMEOUT",
				"request timed out",
			)

		case errors.Is(err, ai.ErrPolicyDenied):
			return sendError(
				c,
				fiber.StatusForbidden,
				"TOOL_POLICY_DENIED",
				"the requested tool is not allowed",
			)

		default:
			return sendError(
				c,
				fiber.StatusBadGateway,
				"AGENT_ERROR",
				"the AI agent could not complete the request",
			)
		}
	}

	// A confirmation response needs a complete pending_action contract.
	// Until write tools are implemented, fail safely.
	if output.RequiresConfirmation {
		return sendError(
			c,
			fiber.StatusNotImplemented,
			"PENDING_ACTION_NOT_IMPLEMENTED",
			"write actions are not available yet",
		)
	}

	toolCalls := make([]any, 0, len(output.ToolExecutions))
	sources := make([]any, 0, len(output.ToolExecutions))

	for _, execution := range output.ToolExecutions {
		toolCalls = append(toolCalls, execution)

		if execution.Result.Source != "" {
			sources = append(
				sources,
				fiber.Map{
					"name":         execution.Result.Source,
					"chain_id":     execution.Result.ChainID,
					"block_number": execution.Result.BlockNumber,
					"observed_at":  execution.Result.ObservedAt,
				},
			)
		}
	}

	return c.Status(fiber.StatusOK).JSON(ChatResponse{
		Message:       output.Message,
		ToolCalls:     toolCalls,
		PendingAction: nil,
		Sources:       sources,
		RequestID:     getRequestID(c),
		Error:         nil,
	})
}
