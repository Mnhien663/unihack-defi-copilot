package main

import (
	"log"
	"time"

	"unihack-backend/internal/handlers"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/gofiber/fiber/v2/middleware/requestid"
	"github.com/gofiber/fiber/v2/middleware/timeout"
)

func main() {
	app := fiber.New(fiber.Config{
		ErrorHandler: handlers.ErrorHandler,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 15 * time.Second,
	})

	app.Use(requestid.New())
	app.Use(recover.New())

	app.Get("/health", func(c *fiber.Ctx) error {
		return c.Status(fiber.StatusOK).JSON(fiber.Map{
			"status":  "ok",
			"service": "unihack-backend",
		})
	})

	chatHandler := timeout.NewWithContext(
		handlers.HandleChat,
		10*time.Second,
	)

	app.Post("/api/v1/chat", chatHandler)

	// Temporary compatibility route. Remove after frontend uses /api/v1/chat.
	app.Post("/api/chat", chatHandler)

	log.Println("Fiber server is running at http://localhost:8080")
	log.Fatal(app.Listen(":8080"))
}
