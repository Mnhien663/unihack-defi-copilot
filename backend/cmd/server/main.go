package main

import (
	"context"
	"log"
	"os"
	"strings"
	"time"

	"unihack-backend/internal/ai"
	"unihack-backend/internal/handlers"
	"unihack-backend/internal/tools"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/gofiber/fiber/v2/middleware/requestid"
	"github.com/gofiber/fiber/v2/middleware/timeout"
)

const (
	defaultAIProvider = "mock"
	chatTimeout       = 30 * time.Second
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

	provider := strings.ToLower(
		strings.TrimSpace(os.Getenv("AI_PROVIDER")),
	)
	if provider == "" {
		provider = defaultAIProvider
	}

	var llmClient ai.LLMClient

	switch provider {
	case "mock":
		llmClient = ai.NewDemoLLMClient()

	case "openai":
		openAIClient, err := ai.NewOpenAIClientFromEnv(nil)
		if err != nil {
			log.Fatalf(
				"Cannot initialize OpenAI client: %v",
				err,
			)
		}

		llmClient = openAIClient

	case "gemini":
		geminiClient, err := ai.NewGeminiClientFromEnv(
			context.Background(),
			nil,
		)
		if err != nil {
			log.Fatalf(
				"Cannot initialize Gemini client: %v",
				err,
			)
		}

		llmClient = geminiClient

	default:
		log.Fatalf(
			"Unsupported AI_PROVIDER %q; expected mock, openai or gemini",
			provider,
		)
	}

	agent, err := ai.NewAgent(
		llmClient,
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
		WriteTimeout: 35 * time.Second,
	})

	app.Use(requestid.New())
	app.Use(recover.New())

	app.Get("/health", func(c *fiber.Ctx) error {
		return c.Status(fiber.StatusOK).JSON(fiber.Map{
			"status":      "ok",
			"service":     "unihack-backend",
			"ai_provider": provider,
			"data_mode":   "mock",
		})
	})

	chatHandler := timeout.NewWithContext(
		injectedChatHandler,
		chatTimeout,
	)

	app.Post("/api/v1/chat", chatHandler)

	// Temporary compatibility route.
	app.Post("/api/chat", chatHandler)

	log.Printf(
		"Fiber server is running with AI provider %s at http://localhost:8080",
		provider,
	)

	if err := app.Listen(":8080"); err != nil {
		log.Printf("Server stopped: %v", err)
	}
}
