package handlers

import (
	"context"
	"errors"

	"github.com/gofiber/fiber/v2"
)

func ErrorHandler(c *fiber.Ctx, err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return sendError(
			c,
			fiber.StatusRequestTimeout,
			"REQUEST_TIMEOUT",
			"request timed out",
		)
	}

	statusCode := fiber.StatusInternalServerError
	errorCode := "INTERNAL_SERVER_ERROR"
	errorMessage := "an unexpected error occurred"

	var fiberError *fiber.Error

	if errors.As(err, &fiberError) {
		statusCode = fiberError.Code
		errorMessage = fiberError.Message

		switch statusCode {
		case fiber.StatusBadRequest:
			errorCode = "BAD_REQUEST"

		case fiber.StatusNotFound:
			errorCode = "NOT_FOUND"

		case fiber.StatusRequestTimeout:
			errorCode = "REQUEST_TIMEOUT"
			errorMessage = "request timed out"

		default:
			errorCode = "HTTP_ERROR"
		}
	}

	return sendError(
		c,
		statusCode,
		errorCode,
		errorMessage,
	)
}
