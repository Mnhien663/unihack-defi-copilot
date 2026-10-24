package handlers

import (
	"github.com/gofiber/fiber/v2"
)

type ChatRequest struct {
	Message string `json:"message"`
}

func HandleChat(c *fiber.Ctx) error {
	req := new(ChatRequest)

	if err := c.BodyParser(req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid request body",
		})
	}

	// TODO: Connect req.Message to the selected LLM API.

	return c.JSON(fiber.Map{
		"status":           "success",
		"received_message": req.Message,
		"reply":            "Backend received the message and is ready to connect to an AI model.",
	})
}
