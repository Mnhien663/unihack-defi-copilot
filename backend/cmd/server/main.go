package main

import (
	"log"
	"unihack-backend/internal/handlers"

	"github.com/gofiber/fiber/v2"
)

func main() {
	app := fiber.New()

	app.Post("/api/chat", handlers.HandleChat)

	log.Println("Server Fiber đang chạy tại http://localhost:8080")
	log.Fatal(app.Listen(":8080"))
}
