package main

import (
	"log"
	"time"

	"unihack-backend/internal/ai"
	"unihack-backend/internal/handlers"
	"unihack-backend/internal/tools"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/gofiber/fiber/v2/middleware/requestid"
	"github.com/gofiber/fiber/v2/middleware/timeout"
)

func main() {
	registry := tools.NewRegistry()

	nativeBalanceTool, err := tools.NewNativeBalanceTool(
		tools.NewMockChainClient(),
	)
	if err != nil {
		log.Fatalf(
			"Cannot create native balance tool: %v",
			err,
		)
	}

	if err := registry.Register(nativeBalanceTool); err != nil {
		log.Fatalf(
			"Cannot register native balance tool: %v",
			err,
		)
	}

	policies := ai.NewStaticPolicyEngine(
		[]string{
			tools.NativeBalanceToolName,
		},
		nil,
	)

	agent, err := ai.NewAgent(
		ai.NewDemoLLMClient(),
		registry,
		ai.NewInMemoryStore(),
		policies,
	)
	if err != nil {
		log.Fatalf("Cannot create AI agent: %v", err)
	}

	injectedChatHandler, err := handlers.NewChatHandler(agent)
	if err != nil {
		log.Fatalf("Cannot create chat handler: %v", err)
	}

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
			"mode":    "mock",
		})
	})

	chatHandler := timeout.NewWithContext(
		injectedChatHandler,
		10*time.Second,
	)

	app.Post("/api/v1/chat", chatHandler)

	// Temporary compatibility route.
	app.Post("/api/chat", chatHandler)

	log.Println(
		"Fiber server is running in mock mode at http://localhost:8080",
	)

	if err := app.Listen(":8080"); err != nil {
		log.Printf("Server stopped: %v", err)
	}
}
