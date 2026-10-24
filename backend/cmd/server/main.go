package main

import (
	"log"
	"unihack-backend/internal/handlers"

	"github.com/gofiber/fiber/v2"
)

func main() {
	app := fiber.New()

	app.Get("/health", func(c *fiber.Ctx) error {
		return c.Status(fiber.StatusOK).JSON(fiber.Map{
			"status":  "ok",
			"service": "unihack-backend",
		})
	})

	app.Post("/api/chat", handlers.HandleChat)

	log.Println("Fiber server is running at http://localhost:8080")
	log.Fatal(app.Listen(":8080"))
}
