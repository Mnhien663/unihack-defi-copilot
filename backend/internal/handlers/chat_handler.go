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
			"error": "Dữ liệu không hợp lệ, vui lòng kiểm tra lại",
		})
	}

	// TODO: (Nhiệm vụ các ngày sau)
	// Tại đây, bạn sẽ lấy chuỗi text (req.Message) để gửi qua API của các LLM (ví dụ như Google AI Studio).

	return c.JSON(fiber.Map{
		"status":           "success",
		"received_message": req.Message,
		"reply":            "Backend đã nhận được tin nhắn và đang chờ kết nối AI Model!",
	})
}
