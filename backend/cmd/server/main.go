package main

import (
	"context"
	"log"
	"os"
	"strings"
	"time"

	"unihack-backend/internal/ai"
	"unihack-backend/internal/avalanche"
	"unihack-backend/internal/database"
	"unihack-backend/internal/handlers"
	"unihack-backend/internal/tools"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/gofiber/fiber/v2/middleware/requestid"
	"github.com/gofiber/fiber/v2/middleware/timeout"
)

const (
	defaultAIProvider = "mock"
	defaultDataMode   = "mock"
	chatTimeout       = 30 * time.Second
)

func main() {
	db, err := database.ConnectFromEnv(context.Background())
	if err != nil {
		log.Fatalf("Cannot connect to PostgreSQL: %v", err)
	}
	defer db.Close()

	memoryStore, err := database.NewPostgresMemoryStore(db)
	if err != nil {
		log.Fatalf("Cannot create PostgreSQL memory store: %v", err)
	}

	registry := tools.NewRegistry()

	dataMode := strings.ToLower(
		strings.TrimSpace(os.Getenv("DATA_MODE")),
	)
	if dataMode == "" {
		dataMode = defaultDataMode
	}

	var nativeClient tools.ChainClient
	var tokenClient tools.TokenBalanceClient
	var transactionClient tools.TransactionClient
	var transactionSource string
	var allowanceReader avalanche.AllowanceReader

	switch dataMode {
	case "mock":
		mockClient := tools.NewMockChainClient()
		nativeClient = mockClient
		tokenClient = mockClient
		transactionClient = mockClient
		transactionSource = "mock_chain"
		allowanceReader = mockClient

	case "data_api":
		dataAPIClient := avalanche.NewDataAPIClient(
			os.Getenv("AVALANCHE_DATA_API_KEY"),
		)
		nativeClient = dataAPIClient
		tokenClient = dataAPIClient
		transactionClient = dataAPIClient
		transactionSource = "Avalanche Data API"
		rpcClient, err := avalanche.InitClient()
		if err != nil {
			log.Fatalf("Cannot initialize allowance RPC: %v", err)
		}
		defer rpcClient.Close()

		rpcAllowanceReader, err := avalanche.NewRPCAllowanceReader(
			rpcClient,
			[]string{
				// DDEMO on Avalanche Fuji.
				"0xcCCb83Fbe349fAebDE74Ce633D25B282E6FDd21A",
			},
		)
		if err != nil {
			log.Fatalf("Cannot create allowance reader: %v", err)
		}

		allowanceReader = rpcAllowanceReader

	default:
		log.Fatalf(
			"Unsupported DATA_MODE %q; expected mock or data_api",
			dataMode,
		)
	}

	nativeBalanceTool, err := tools.NewNativeBalanceTool(nativeClient)
	if err != nil {
		log.Fatalf("Cannot create native balance tool: %v", err)
	}

	tokenBalanceTool, err := tools.NewListTokenBalancesTool(tokenClient)
	if err != nil {
		log.Fatalf("Cannot create token balance tool: %v", err)
	}

	transactionTool, err := tools.NewListTransactionsTool(
		transactionClient,
		transactionSource,
	)
	if err != nil {
		log.Fatalf("Cannot create transaction history tool: %v", err)
	}

	allowanceTool, err := tools.NewGetAllowanceTool(allowanceReader)
	if err != nil {
		log.Fatalf("Cannot create allowance tool: %v", err)
	}

	for _, tool := range []ai.Tool{
		nativeBalanceTool,
		tokenBalanceTool,
		transactionTool,
		allowanceTool,
	} {
		if err := registry.Register(tool); err != nil {
			log.Fatalf(
				"Cannot register tool %q: %v",
				tool.Definition().Name,
				err,
			)
		}
	}

	policies := ai.NewStaticPolicyEngine(
		[]string{
			tools.NativeBalanceToolName,
			tools.ListTokenBalancesToolName,
			tools.ListTransactionsToolName,
			tools.GetAllowanceToolName,
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
		memoryStore,
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
			"data_mode":   dataMode,
			"storage":     "postgres",
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
		"Fiber server is running with AI provider %s and data mode %s at http://localhost:8080",
		provider,
		dataMode,
	)

	if err := app.Listen(":8080"); err != nil {
		log.Printf("Server stopped: %v", err)
	}
}
