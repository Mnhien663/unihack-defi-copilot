package handlers

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"

	"unihack-backend/internal/ai"
	"unihack-backend/internal/avalanche"
	"unihack-backend/internal/tools"

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
	Message       string            `json:"message"`
	ToolCalls     []any             `json:"tool_calls"`
	PendingAction *ai.PendingAction `json:"pending_action"`
	Sources       []any             `json:"sources"`
	RequestID     string            `json:"request_id"`
	Error         *ErrorDetail      `json:"error"`
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
			RequestID:      getRequestID(c),
			IdempotencyKey: c.Get("Idempotency-Key"),
			SessionID:      req.SessionID,
			WalletAddress:  req.WalletAddress,
			ChainID:        uint64(req.ChainID),
			Message:        req.Message,
			Locale:         req.Locale,
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
		case errors.Is(err, ai.ErrInvalidIdempotencyKey):
			return sendError(
				c,
				fiber.StatusBadRequest,
				"INVALID_IDEMPOTENCY_KEY",
				"Idempotency-Key must be 1 to 128 characters using only letters, digits, hyphens, or underscores",
			)

		case errors.Is(err, ai.ErrSwapIdempotencyConflict):
			return sendError(
				c,
				fiber.StatusConflict,
				"IDEMPOTENCY_KEY_CONFLICT",
				"this Idempotency-Key was already used for a different request in this session",
			)

		case errors.Is(err, ai.ErrSwapPendingUnavailable):
			return sendError(
				c,
				fiber.StatusConflict,
				"PENDING_SWAP_UNAVAILABLE",
				"the swap action associated with this Idempotency-Key is no longer pending or has expired",
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

	if output.RequiresConfirmation && output.PendingAction == nil {
		return sendError(
			c,
			fiber.StatusBadGateway,
			"INVALID_PENDING_ACTION",
			"the agent returned an incomplete pending action",
		)
	}

	if output.PendingAction != nil &&
		(!output.RequiresConfirmation ||
			!output.PendingAction.RequiresConfirmation) {
		return sendError(
			c,
			fiber.StatusBadGateway,
			"INVALID_PENDING_ACTION",
			"the agent returned an unsafe pending action",
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
		PendingAction: output.PendingAction,
		Sources:       sources,
		RequestID:     getRequestID(c),
		Error:         nil,
	})
}

// verifyPendingDecisionSignature checks an EOA signature over the exact
// message constructed by the server. Never pass a client-provided message.
func verifyPendingDecisionSignature(
	serverMessage string,
	expectedWallet string,
	signatureHex string,
) bool {
	if serverMessage == "" || !common.IsHexAddress(expectedWallet) {
		return false
	}

	wallet := common.HexToAddress(expectedWallet)
	if wallet == (common.Address{}) {
		return false
	}

	// 0x followed by a 65-byte ECDSA signature.
	if len(signatureHex) != 132 {
		return false
	}
	sig, err := hexutil.Decode(signatureHex)
	if err != nil || len(sig) != 65 {
		return false
	}

	switch sig[64] {
	case 27, 28:
		sig[64] -= 27
	case 0, 1:
		// Already normalized.
	default:
		return false
	}

	r := new(big.Int).SetBytes(sig[:32])
	s := new(big.Int).SetBytes(sig[32:64])
	if !crypto.ValidateSignatureValues(sig[64], r, s, true) {
		return false
	}

	digest := accounts.TextHash([]byte(serverMessage))
	pub, err := crypto.SigToPub(digest, sig)
	if err != nil {
		return false
	}

	return crypto.PubkeyToAddress(*pub) == wallet
}

// pendingDecisionMessage builds the exact text the wallet must sign.
// domain comes from server configuration; action and sessionID come from
// the stored record. The client must not supply the message itself.
func pendingDecisionMessage(
	domain string,
	decision string,
	sessionID string,
	action ai.PendingAction,
) (string, error) {
	if decision != "confirm" && decision != "reject" {
		return "", fmt.Errorf("invalid pending action decision")
	}
	if domain == "" || sessionID == "" || action.PendingActionID == "" ||
		action.NormalizedIntentHash == "" || action.ExpiresAt == "" ||
		action.ChainID == 0 || !common.IsHexAddress(action.WalletAddress) ||
		common.HexToAddress(action.WalletAddress) == (common.Address{}) {
		return "", fmt.Errorf("incomplete pending action signing context")
	}

	return fmt.Sprintf(
		"UniHack DeFi Copilot — pending swap decision v1\n"+
			"This signature does not send a transaction.\n"+
			"Domain: %q\nDecision: %s\nAction ID: %q\nSession ID: %q\n"+
			"Wallet: %s\nChain ID: %d\nReview hash: %q\nExpires at: %q",
		domain,
		decision,
		action.PendingActionID,
		sessionID,
		common.HexToAddress(action.WalletAddress).Hex(),
		action.ChainID,
		action.NormalizedIntentHash,
		action.ExpiresAt,
	), nil
}

// pendingSwapReceiptMessage binds a receipt operation to one approved swap
// and one transaction hash. The backend constructs the text the wallet signs.
func pendingSwapReceiptMessage(
	domain, operation, sessionID string,
	action ai.PendingAction,
	transactionHash string,
	signatureExpiresAt time.Time,
) (string, error) {
	if operation != "submit-hash" && operation != "refresh-receipt" {
		return "", errors.New("invalid swap receipt operation")
	}

	hash, err := hexutil.Decode(strings.ToLower(transactionHash))
	if err != nil || len(hash) != common.HashLength {
		return "", errors.New("invalid swap transaction hash")
	}

	if domain == "" || sessionID == "" || action.PendingActionID == "" ||
		action.NormalizedIntentHash == "" || action.ChainID != uint64(ai.FujiChainID) ||
		!common.IsHexAddress(action.WalletAddress) ||
		common.HexToAddress(action.WalletAddress) == (common.Address{}) ||
		signatureExpiresAt.IsZero() {
		return "", errors.New("incomplete swap receipt signing context")
	}

	return fmt.Sprintf(
		"UniHack DeFi Copilot — swap receipt authorization v1\n"+
			"This signature does not send a transaction.\n"+
			"Domain: %q\nOperation: %s\nAction ID: %q\nSession ID: %q\n"+
			"Wallet: %s\nChain ID: %d\nReview hash: %q\n"+
			"Transaction hash: %s\nSignature expires at: %q",
		domain, operation, action.PendingActionID, sessionID,
		common.HexToAddress(action.WalletAddress).Hex(), action.ChainID,
		action.NormalizedIntentHash, common.BytesToHash(hash).Hex(),
		signatureExpiresAt.UTC().Format(time.RFC3339Nano),
	), nil
}

const swapReceiptSignatureTTL = 2 * time.Minute

type pendingSwapReceiptRequest struct {
	SessionID       string `json:"session_id"`
	ReviewHash      string `json:"review_hash"`
	WalletAddress   string `json:"wallet_address"`
	TransactionHash string `json:"transaction_hash"`
}

func loadPendingSwapReceiptContext(
	ctx context.Context,
	store tools.PendingSwapReceiptStore,
	actionID, operation string,
	req pendingSwapReceiptRequest,
) (*ai.PendingAction, string, error) {
	action, _, storedHash, err := store.LoadConfirmedSwapForReceipt(
		ctx, actionID, req.SessionID, req.WalletAddress,
		uint64(ai.FujiChainID), req.ReviewHash,
	)
	if err != nil {
		return nil, "", err
	}
	if action == nil {
		return nil, "", tools.ErrSwapSubmissionUnavailable
	}

	switch operation {
	case "submit-hash":
		if storedHash != "" && !strings.EqualFold(storedHash, req.TransactionHash) {
			return nil, "", tools.ErrSwapSubmissionHashMismatch
		}
		return action, strings.ToLower(req.TransactionHash), nil
	case "refresh-receipt":
		if storedHash == "" {
			return nil, "", tools.ErrSwapSubmissionUnavailable
		}
		return action, storedHash, nil
	default:
		return nil, "", errors.New("invalid swap receipt operation")
	}
}

func NewPendingSwapReceiptChallengeHandler(
	store tools.PendingSwapReceiptStore,
	domain, operation string,
) (fiber.Handler, error) {
	domain = strings.TrimSpace(domain)
	if store == nil || domain == "" ||
		(operation != "submit-hash" && operation != "refresh-receipt") {
		return nil, errors.New("invalid swap receipt challenge configuration")
	}

	return func(c *fiber.Ctx) error {
		c.Set("Cache-Control", "no-store")

		actionID, err := uuid.Parse(c.Params("id"))
		if err != nil || actionID == uuid.Nil {
			return sendError(c, fiber.StatusBadRequest,
				"INVALID_PENDING_ACTION_ID", "invalid pending action ID")
		}

		var req pendingSwapReceiptRequest
		if err := c.BodyParser(&req); err != nil {
			return sendError(c, fiber.StatusBadRequest,
				"INVALID_RECEIPT_REQUEST", "invalid receipt request")
		}

		reviewHash, err := hex.DecodeString(req.ReviewHash)
		if req.SessionID == "" || len(req.SessionID) > 128 ||
			err != nil || len(reviewHash) != 32 ||
			req.ReviewHash != strings.ToLower(req.ReviewHash) ||
			!common.IsHexAddress(req.WalletAddress) {
			return sendError(c, fiber.StatusBadRequest,
				"INVALID_RECEIPT_REQUEST", "invalid session, review hash, or wallet address")
		}

		if operation == "submit-hash" {
			hash, err := hexutil.Decode(strings.ToLower(req.TransactionHash))
			if err != nil || len(hash) != common.HashLength {
				return sendError(c, fiber.StatusBadRequest,
					"INVALID_TRANSACTION_HASH", "invalid transaction hash")
			}
		} else if req.TransactionHash != "" {
			return sendError(c, fiber.StatusBadRequest,
				"INVALID_RECEIPT_REQUEST", "receipt refresh uses the stored transaction hash")
		}

		action, hash, err := loadPendingSwapReceiptContext(
			c.UserContext(), store, actionID.String(), operation, req,
		)
		switch {
		case errors.Is(err, tools.ErrSwapSubmissionUnavailable):
			return sendError(c, fiber.StatusNotFound,
				"PENDING_ACTION_UNAVAILABLE", "pending action is unavailable")
		case errors.Is(err, tools.ErrSwapSubmissionHashMismatch):
			return sendError(c, fiber.StatusConflict,
				"TRANSACTION_HASH_MISMATCH", "action already has a different transaction hash")
		case err != nil:
			return sendError(c, fiber.StatusInternalServerError,
				"PENDING_ACTION_LOOKUP_FAILED", "could not load pending action")
		}

		expiresAt := time.Now().UTC().Add(swapReceiptSignatureTTL)
		message, err := pendingSwapReceiptMessage(
			domain, operation, req.SessionID, *action, hash, expiresAt,
		)
		if err != nil {
			return sendError(c, fiber.StatusInternalServerError,
				"RECEIPT_MESSAGE_FAILED", "could not build receipt message")
		}

		return c.JSON(fiber.Map{
			"message":              message,
			"signature_expires_at": expiresAt.Format(time.RFC3339Nano),
		})
	}, nil
}

type signedPendingSwapReceiptRequest struct {
	pendingSwapReceiptRequest
	Signature          string `json:"signature"`
	SignatureExpiresAt string `json:"signature_expires_at"`
}

func NewPendingSwapReceiptHandler(
	store tools.PendingSwapReceiptFinalizer,
	reader avalanche.ReceiptReader,
	domain, operation string,
) (fiber.Handler, error) {
	domain = strings.TrimSpace(domain)
	if store == nil || reader == nil || domain == "" ||
		(operation != "submit-hash" && operation != "refresh-receipt") {
		return nil, errors.New("invalid swap receipt handler configuration")
	}

	return func(c *fiber.Ctx) error {
		c.Set("Cache-Control", "no-store")

		actionID, err := uuid.Parse(c.Params("id"))
		if err != nil || actionID == uuid.Nil {
			return sendError(c, fiber.StatusBadRequest,
				"INVALID_PENDING_ACTION_ID", "invalid pending action ID")
		}

		var req signedPendingSwapReceiptRequest
		if err := c.BodyParser(&req); err != nil {
			return sendError(c, fiber.StatusBadRequest,
				"INVALID_RECEIPT_REQUEST", "invalid receipt request")
		}

		reviewHash, err := hex.DecodeString(req.ReviewHash)
		if req.SessionID == "" || len(req.SessionID) > 128 ||
			err != nil || len(reviewHash) != 32 ||
			req.ReviewHash != strings.ToLower(req.ReviewHash) ||
			!common.IsHexAddress(req.WalletAddress) ||
			len(req.Signature) != 132 {
			return sendError(c, fiber.StatusBadRequest,
				"INVALID_RECEIPT_REQUEST", "invalid receipt request fields")
		}

		if operation == "submit-hash" {
			hash, err := hexutil.Decode(strings.ToLower(req.TransactionHash))
			if err != nil || len(hash) != common.HashLength {
				return sendError(c, fiber.StatusBadRequest,
					"INVALID_TRANSACTION_HASH", "invalid transaction hash")
			}
		} else if req.TransactionHash != "" {
			return sendError(c, fiber.StatusBadRequest,
				"INVALID_RECEIPT_REQUEST", "receipt refresh uses the stored transaction hash")
		}

		expiresAt, err := time.Parse(time.RFC3339Nano, req.SignatureExpiresAt)
		now := time.Now().UTC()
		if err != nil || expiresAt.After(now.Add(swapReceiptSignatureTTL)) {
			return sendError(c, fiber.StatusBadRequest,
				"INVALID_RECEIPT_SIGNATURE_EXPIRY", "invalid signature expiry")
		}
		if !expiresAt.After(now) {
			return sendError(c, fiber.StatusForbidden,
				"RECEIPT_SIGNATURE_EXPIRED", "receipt signature has expired")
		}

		ctx, cancel := context.WithTimeout(c.UserContext(), 10*time.Second)
		defer cancel()

		id := actionID.String()
		action, hash, err := loadPendingSwapReceiptContext(
			ctx, store, id, operation, req.pendingSwapReceiptRequest,
		)
		switch {
		case errors.Is(err, tools.ErrSwapSubmissionUnavailable):
			return sendError(c, fiber.StatusNotFound,
				"PENDING_ACTION_UNAVAILABLE", "pending action is unavailable")
		case errors.Is(err, tools.ErrSwapSubmissionHashMismatch):
			return sendError(c, fiber.StatusConflict,
				"TRANSACTION_HASH_MISMATCH", "action already has a different transaction hash")
		case err != nil:
			return sendError(c, fiber.StatusInternalServerError,
				"PENDING_ACTION_LOOKUP_FAILED", "could not load pending action")
		}

		message, err := pendingSwapReceiptMessage(
			domain, operation, req.SessionID, *action, hash, expiresAt,
		)
		if err != nil {
			return sendError(c, fiber.StatusInternalServerError,
				"RECEIPT_MESSAGE_FAILED", "could not build receipt message")
		}
		if !verifyPendingDecisionSignature(message, action.WalletAddress, req.Signature) {
			return sendError(c, fiber.StatusForbidden,
				"INVALID_RECEIPT_SIGNATURE", "signature does not authorize this receipt operation")
		}
		if !expiresAt.After(time.Now().UTC()) {
			return sendError(c, fiber.StatusForbidden,
				"RECEIPT_SIGNATURE_EXPIRED", "receipt signature has expired")
		}

		var status string
		if operation == "submit-hash" {
			status, err = tools.RecordReviewedSwapSubmission(
				ctx, store, reader, id, req.SessionID, action.WalletAddress,
				action.ChainID, req.ReviewHash, hash,
			)
		} else {
			status, err = tools.RefreshReviewedSwapReceipt(
				ctx, store, reader, id, req.SessionID, action.WalletAddress,
				action.ChainID, req.ReviewHash,
			)
		}

		switch {
		case errors.Is(err, tools.ErrSwapSubmissionUnavailable):
			return sendError(c, fiber.StatusConflict,
				"PENDING_ACTION_UNAVAILABLE", "pending action is no longer available")
		case errors.Is(err, tools.ErrSwapSubmissionHashMismatch):
			return sendError(c, fiber.StatusConflict,
				"TRANSACTION_HASH_MISMATCH", "action already has a different transaction hash")
		case errors.Is(err, context.DeadlineExceeded):
			return sendError(c, fiber.StatusGatewayTimeout,
				"SWAP_RECEIPT_TIMEOUT", "receipt operation timed out")
		case err != nil:
			return sendError(c, fiber.StatusBadGateway,
				"SWAP_RECEIPT_OPERATION_FAILED", "could not verify or record swap transaction")
		}

		return c.JSON(fiber.Map{
			"pending_action_id": id,
			"transaction_hash":  hash,
			"status":            status,
		})
	}, nil
}

type pendingDecisionReviewStore interface {
	LookupPendingSwapOwner(
		ctx context.Context,
		actionID, sessionID string,
		chainID uint64,
		reviewedIntentHash string,
	) (string, error)

	LoadPendingSwapForReview(
		ctx context.Context,
		actionID, sessionID, authenticatedWallet string,
		chainID uint64,
		reviewedIntentHash string,
	) (*ai.PendingAction, error)
}

type pendingDecisionChallengeRequest struct {
	SessionID  string `json:"session_id"`
	ReviewHash string `json:"review_hash"`
	Decision   string `json:"decision"`
}

func NewPendingDecisionChallengeHandler(
	store pendingDecisionReviewStore,
	domain string,
) (fiber.Handler, error) {
	domain = strings.TrimSpace(domain)
	if store == nil || domain == "" {
		return nil, errors.New("pending decision challenge requires a store and signing domain")
	}

	return func(c *fiber.Ctx) error {
		c.Set("Cache-Control", "no-store")

		actionID, err := uuid.Parse(c.Params("id"))
		if err != nil || actionID == uuid.Nil {
			return sendError(c, fiber.StatusBadRequest,
				"INVALID_PENDING_ACTION_ID", "invalid pending action ID")
		}

		var req pendingDecisionChallengeRequest
		if err := c.BodyParser(&req); err != nil {
			return sendError(c, fiber.StatusBadRequest,
				"INVALID_DECISION_REQUEST", "invalid decision request")
		}

		hash, err := hex.DecodeString(req.ReviewHash)
		if req.SessionID == "" || len(req.SessionID) > 128 ||
			err != nil || len(hash) != 32 ||
			req.ReviewHash != strings.ToLower(req.ReviewHash) ||
			(req.Decision != "confirm" && req.Decision != "reject") {
			return sendError(c, fiber.StatusBadRequest,
				"INVALID_DECISION_REQUEST", "invalid session, review hash, or decision")
		}

		ctx := c.UserContext()
		chainID := uint64(ai.FujiChainID)
		owner, err := store.LookupPendingSwapOwner(
			ctx, actionID.String(), req.SessionID, chainID, req.ReviewHash,
		)
		if err != nil {
			return sendError(c, fiber.StatusInternalServerError,
				"PENDING_ACTION_LOOKUP_FAILED", "could not load pending action")
		}
		if owner == "" {
			return sendError(c, fiber.StatusNotFound,
				"PENDING_ACTION_UNAVAILABLE", "pending action is unavailable")
		}

		action, err := store.LoadPendingSwapForReview(
			ctx, actionID.String(), req.SessionID, owner, chainID, req.ReviewHash,
		)
		if err != nil {
			return sendError(c, fiber.StatusInternalServerError,
				"PENDING_ACTION_LOOKUP_FAILED", "could not load pending action")
		}
		if action == nil {
			return sendError(c, fiber.StatusNotFound,
				"PENDING_ACTION_UNAVAILABLE", "pending action is unavailable")
		}

		message, err := pendingDecisionMessage(domain, req.Decision, req.SessionID, *action)
		if err != nil {
			return sendError(c, fiber.StatusInternalServerError,
				"DECISION_MESSAGE_FAILED", "could not build decision message")
		}

		return c.JSON(struct {
			Message   string `json:"message"`
			ExpiresAt string `json:"expires_at"`
		}{
			Message:   message,
			ExpiresAt: action.ExpiresAt,
		})
	}, nil
}

type pendingDecisionStore interface {
	pendingDecisionReviewStore
	tools.PendingSwapConfirmStore

	RejectPendingSwap(
		ctx context.Context,
		actionID, sessionID, authenticatedWallet string,
		chainID uint64,
		reviewedIntentHash string,
	) (bool, error)
}

type pendingDecisionRequest struct {
	SessionID  string `json:"session_id"`
	ReviewHash string `json:"review_hash"`
	Signature  string `json:"signature"`
}

func NewPendingDecisionHandler(
	store pendingDecisionStore,
	revalidator tools.ReviewedSwapRevalidator,
	domain string,
	decision string,
) (fiber.Handler, error) {
	domain = strings.TrimSpace(domain)
	if store == nil || domain == "" ||
		(decision != "confirm" && decision != "reject") ||
		(decision == "confirm" && revalidator == nil) {
		return nil, errors.New("invalid pending decision handler configuration")
	}

	return func(c *fiber.Ctx) error {
		c.Set("Cache-Control", "no-store")

		actionID, err := uuid.Parse(c.Params("id"))
		if err != nil || actionID == uuid.Nil {
			return sendError(c, fiber.StatusBadRequest,
				"INVALID_PENDING_ACTION_ID", "invalid pending action ID")
		}

		var req pendingDecisionRequest
		if err := c.BodyParser(&req); err != nil {
			return sendError(c, fiber.StatusBadRequest,
				"INVALID_DECISION_REQUEST", "invalid decision request")
		}

		hash, err := hex.DecodeString(req.ReviewHash)
		if req.SessionID == "" || len(req.SessionID) > 128 ||
			err != nil || len(hash) != 32 ||
			req.ReviewHash != strings.ToLower(req.ReviewHash) ||
			len(req.Signature) != 132 {
			return sendError(c, fiber.StatusBadRequest,
				"INVALID_DECISION_REQUEST", "invalid session, review hash, or signature")
		}

		ctx := c.UserContext()
		if decision == "confirm" {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
		}
		id := actionID.String()
		chainID := uint64(ai.FujiChainID)

		owner, err := store.LookupPendingSwapOwner(
			ctx, id, req.SessionID, chainID, req.ReviewHash,
		)
		if err != nil {
			return sendError(c, fiber.StatusInternalServerError,
				"PENDING_ACTION_LOOKUP_FAILED", "could not load pending action")
		}
		if owner == "" {
			return sendError(c, fiber.StatusNotFound,
				"PENDING_ACTION_UNAVAILABLE", "pending action is unavailable")
		}

		action, err := store.LoadPendingSwapForReview(
			ctx, id, req.SessionID, owner, chainID, req.ReviewHash,
		)
		if err != nil {
			return sendError(c, fiber.StatusInternalServerError,
				"PENDING_ACTION_LOOKUP_FAILED", "could not load pending action")
		}
		if action == nil {
			return sendError(c, fiber.StatusNotFound,
				"PENDING_ACTION_UNAVAILABLE", "pending action is unavailable")
		}

		message, err := pendingDecisionMessage(domain, decision, req.SessionID, *action)
		if err != nil {
			return sendError(c, fiber.StatusInternalServerError,
				"DECISION_MESSAGE_FAILED", "could not build decision message")
		}
		if !verifyPendingDecisionSignature(message, action.WalletAddress, req.Signature) {
			return sendError(c, fiber.StatusForbidden,
				"INVALID_DECISION_SIGNATURE", "signature does not authorize this decision")
		}

		if decision == "reject" {
			changed, err := store.RejectPendingSwap(
				ctx, id, req.SessionID, action.WalletAddress, chainID, req.ReviewHash,
			)
			if err != nil {
				return sendError(c, fiber.StatusInternalServerError,
					"PENDING_ACTION_REJECT_FAILED", "could not reject pending action")
			}
			if !changed {
				return sendError(c, fiber.StatusConflict,
					"PENDING_ACTION_UNAVAILABLE", "pending action is no longer available")
			}

			return c.JSON(fiber.Map{
				"pending_action_id": id,
				"status":            "rejected",
			})
		}

		confirmed, err := tools.ConfirmReviewedSwap(
			ctx, store, revalidator, id, req.SessionID,
			action.WalletAddress, chainID, req.ReviewHash,
		)
		switch {
		case errors.Is(err, tools.ErrSwapReviewRequired):
			return sendError(c, fiber.StatusConflict,
				"SWAP_REVIEW_REQUIRED", "prepare and review a fresh swap")
		case errors.Is(err, tools.ErrPendingSwapNotConfirmable):
			return sendError(c, fiber.StatusConflict,
				"PENDING_ACTION_UNAVAILABLE", "pending action is no longer available")
		case errors.Is(err, context.DeadlineExceeded):
			return sendError(c, fiber.StatusGatewayTimeout,
				"PENDING_ACTION_CONFIRM_TIMEOUT", "swap confirmation timed out")
		case err != nil:
			return sendError(c, fiber.StatusBadGateway,
				"PENDING_ACTION_CONFIRM_FAILED", "could not confirm pending action")
		}

		return c.JSON(struct {
			PendingActionID string          `json:"pending_action_id"`
			Status          string          `json:"status"`
			ExpiresAt       string          `json:"expires_at"`
			Arguments       json.RawMessage `json:"arguments"`
		}{
			PendingActionID: confirmed.PendingActionID,
			Status:          "confirmed",
			ExpiresAt:       confirmed.ExpiresAt,
			Arguments:       confirmed.Arguments,
		})
	}, nil
}
