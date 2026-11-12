package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
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
	defer func() {
		if err := db.Close(); err != nil {
			log.Println("PostgreSQL pool close failed")
			return
		}

		log.Println("PostgreSQL pool closed")
	}()

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
	prepareSwapEnabled := os.Getenv("ENABLE_PREPARE_SWAP") == "1"
	if prepareSwapEnabled && dataMode != "data_api" {
		log.Fatal("ENABLE_PREPARE_SWAP=1 requires DATA_MODE=data_api")
	}

	var prepareSwap ai.SwapPreparationFunc
	var swapRevalidator tools.ReviewedSwapRevalidator
	var swapReceiptReader avalanche.ReceiptReader
	var nativeClient tools.ChainClient
	var tokenClient tools.TokenBalanceClient
	var transactionClient tools.TransactionClient
	var transactionSource string
	var allowanceReader avalanche.AllowanceReader
	var swapQuoteTool *tools.GetSwapQuoteTool
	var lendingPositionTool *tools.GetLendingPositionTool
	var liquidationPriceTool *tools.GetLiquidationPriceTool
	var vaultMetricsTool *tools.GetVaultMetricsTool

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

		quoteConfig := avalanche.LFJQuoteConfig{
			RouterAddress: "0x18556DA13313f3532c54711497A8FedAC273220E",
			PoolAddress:   "0xc3b7307184381f65B398d385a71EF0490239a2b3",
			BinStep:       1,
			TokenA: avalanche.Asset{
				ChainID:  uint64(avalanche.FujiChainID),
				Address:  "0xAb231A5744C8E6c45481754928cCfFFFD4aa0732",
				Symbol:   "USDT",
				Decimals: 6,
			},
			TokenB: avalanche.Asset{
				ChainID:  uint64(avalanche.FujiChainID),
				Address:  "0xB6076C93701D6a07266c31066B298AeC6dd65c2d",
				Symbol:   "USDC",
				Decimals: 6,
			},
		}

		quoter, err := avalanche.NewLFJQuoter(rpcClient, quoteConfig)
		if err != nil {
			log.Fatalf("Cannot create LFJ quoter: %v", err)
		}

		swapQuoteTool, err = tools.NewGetSwapQuoteTool(quoter, quoteConfig)
		if err != nil {
			log.Fatalf("Cannot create swap quote tool: %v", err)
		}

		if prepareSwapEnabled {
			simulator, err := avalanche.NewSwapSimulator(rpcClient)
			if err != nil {
				log.Fatalf("Cannot create swap simulator: %v", err)
			}

			preparer, err := avalanche.NewSwapPreparer(
				quoter,
				simulator,
				strings.TrimSpace(os.Getenv("PREPARE_SWAP_MAX_AMOUNT_RAW")),
			)
			if err != nil {
				log.Fatalf("Cannot create swap preparer: %v", err)
			}

			swapRevalidator = preparer
			swapReceiptReader, err = avalanche.NewRPCReceiptReader(rpcClient)
			if err != nil {
				log.Fatalf("Cannot create swap receipt reader: %v", err)
			}
			prepareSwap, err = tools.NewPrepareSwapCallback(preparer)
			if err != nil {
				log.Fatalf("Cannot create prepare swap callback: %v", err)
			}
		}

		lendingReader, err := avalanche.NewAaveLendingReader(
			rpcClient,
			"0x8B9b2AF4afB389b4a70A474dfD4AdCD4a302bb40", // Aave V3 Fuji Pool
			"0xC65cbd1e309Bf0e841Ee6f6E786480598e6a4014", // Aave V3 Fuji Data Provider
		)
		if err != nil {
			log.Fatalf("Cannot create Aave position reader: %v", err)
		}

		lendingPositionTool, err = tools.NewGetLendingPositionTool(lendingReader)
		if err != nil {
			log.Fatalf("Cannot create lending position tool: %v", err)
		}

		liquidationPriceTool, err = tools.NewGetLiquidationPriceTool(lendingReader)
		if err != nil {
			log.Fatalf("Cannot create liquidation price tool: %v", err)
		}

		vaultReader, err := avalanche.NewRPCVaultReader(rpcClient)
		if err != nil {
			log.Fatalf("Cannot create vault reader: %v", err)
		}

		const vaultMetricMaxAge = 5 * time.Minute

		vaultMetricsTool, err = tools.NewGetVaultMetricsTool(vaultReader, vaultMetricMaxAge)
		if err != nil {
			log.Fatalf("Cannot create vault metrics tool: %v", err)
		}

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

	readToolNames := []string{
		tools.NativeBalanceToolName,
		tools.ListTokenBalancesToolName,
		tools.ListTransactionsToolName,
		tools.GetAllowanceToolName,
	}

	if swapQuoteTool != nil {
		if err := registry.Register(swapQuoteTool); err != nil {
			log.Fatalf("Cannot register swap quote tool: %v", err)
		}
		readToolNames = append(readToolNames, tools.GetSwapQuoteToolName)
	}

	if lendingPositionTool != nil {
		if err := registry.Register(lendingPositionTool); err != nil {
			log.Fatalf("Cannot register lending position tool: %v", err)
		}
		readToolNames = append(readToolNames, tools.GetLendingPositionToolName)
	}

	if liquidationPriceTool != nil {
		if err := registry.Register(liquidationPriceTool); err != nil {
			log.Fatalf("Cannot register liquidation price tool: %v", err)
		}
		readToolNames = append(readToolNames, tools.GetLiquidationPriceToolName)
	}

	if vaultMetricsTool != nil {
		if err := registry.Register(vaultMetricsTool); err != nil {
			log.Fatalf("Cannot register vault metrics tool: %v", err)
		}
		readToolNames = append(readToolNames, tools.GetVaultMetricsToolName)
	}

	log.Printf("Read tools registered (%d): %s",
		len(readToolNames), strings.Join(readToolNames, ", "))

	var writeToolNames []string
	if prepareSwap != nil {
		if err := registry.Register(tools.PrepareSwapProposal{}); err != nil {
			log.Fatalf("Cannot register prepare swap proposal: %v", err)
		}
		writeToolNames = append(writeToolNames, tools.PrepareSwapToolName)
	}

	policies := ai.NewStaticPolicyEngine(readToolNames, writeToolNames)

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

	var agent *ai.Agent
	if prepareSwap != nil {
		agent, err = ai.NewAgentWithSwapPreparation(
			llmClient,
			registry,
			memoryStore,
			policies,
			prepareSwap,
		)
	} else {
		agent, err = ai.NewAgent(
			llmClient,
			registry,
			memoryStore,
			policies,
		)
	}
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

	if os.Getenv("ENABLE_SWAP_DECISIONS") == "1" {
		if prepareSwap == nil || swapRevalidator == nil {
			log.Fatal("ENABLE_SWAP_DECISIONS=1 requires ENABLE_PREPARE_SWAP=1 and DATA_MODE=data_api")
		}

		domain := strings.TrimSpace(os.Getenv("SWAP_DECISION_SIGNING_DOMAIN"))
		if domain == "" {
			log.Fatal("SWAP_DECISION_SIGNING_DOMAIN is required when swap decisions are enabled")
		}

		challengeHandler, err := handlers.NewPendingDecisionChallengeHandler(memoryStore, domain)
		if err != nil {
			log.Fatalf("Cannot create pending decision challenge handler: %v", err)
		}

		confirmHandler, err := handlers.NewPendingDecisionHandler(
			memoryStore, swapRevalidator, domain, "confirm",
		)
		if err != nil {
			log.Fatalf("Cannot create pending decision confirm handler: %v", err)
		}

		rejectHandler, err := handlers.NewPendingDecisionHandler(
			memoryStore, nil, domain, "reject",
		)
		if err != nil {
			log.Fatalf("Cannot create pending decision reject handler: %v", err)
		}

		app.Post("/api/v1/pending-actions/:id/challenge", challengeHandler)
		app.Post("/api/v1/pending-actions/:id/confirm", confirmHandler)
		app.Post("/api/v1/pending-actions/:id/reject", rejectHandler)

		if swapReceiptReader == nil {
			log.Fatal("Swap decisions require a Fuji receipt reader")
		}

		for _, operation := range []string{"submit-hash", "refresh-receipt"} {
			challenge, err := handlers.NewPendingSwapReceiptChallengeHandler(
				memoryStore, domain, operation,
			)
			if err != nil {
				log.Fatalf("Cannot create %s challenge handler: %v", operation, err)
			}

			handler, err := handlers.NewPendingSwapReceiptHandler(
				memoryStore, swapReceiptReader, domain, operation,
			)
			if err != nil {
				log.Fatalf("Cannot create %s handler: %v", operation, err)
			}

			path := "/api/v1/pending-actions/:id/" + operation
			app.Post(path+"/challenge", challenge)
			app.Post(path, handler)
		}
	}

	// Temporary compatibility route.
	app.Post("/api/chat", chatHandler)

	log.Printf(
		"Fiber server is running with AI provider %s and data mode %s at http://localhost:8080",
		provider,
		dataMode,
	)

	shutdownSignal := make(chan os.Signal, 1)

	signal.Notify(
		shutdownSignal,
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer signal.Stop(shutdownSignal)

	serverErrors := make(chan error, 1)

	go func() {
		serverErrors <- app.Listen(":8080")
	}()

	select {
	case err := <-serverErrors:
		if err != nil {
			log.Printf("Server stopped unexpectedly: %v", err)
		}
		return

	case receivedSignal := <-shutdownSignal:
		log.Printf(
			"Received %s; shutting down HTTP server",
			receivedSignal,
		)
	}

	if err := app.ShutdownWithTimeout(40 * time.Second); err != nil {
		log.Printf("HTTP shutdown did not complete cleanly: %v", err)
		return
	}

	if err := <-serverErrors; err != nil {
		log.Printf("HTTP listener stopped with error: %v", err)
	}

	log.Println("HTTP server shutdown completed")
}
