package main

import (
	"context"
	"fmt"
	"log"
	"math/big"
	"os"
	"time"

	"unihack-backend/internal/avalanche"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	if len(os.Args) != 2 {
		return fmt.Errorf("usage: go run ./cmd/data-api-spike <wallet-address>")
	}

	walletAddress := os.Args[1]

	client := avalanche.NewDataAPIClient(
		os.Getenv("AVALANCHE_DATA_API_KEY"),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	balance, err := client.GetNativeBalance(ctx, walletAddress)
	if err != nil {
		return fmt.Errorf("Data API spike failed: %w", err)
	}

	balanceWei, ok := new(big.Int).SetString(balance.Balance, 10)
	if !ok {
		return fmt.Errorf("cannot parse raw balance")
	}

	balanceAVAX, err := avalanche.WeiToAVAX(balanceWei)
	if err != nil {
		return fmt.Errorf("convert balance: %w", err)
	}

	fmt.Println("Source: Avalanche Data API")
	fmt.Println("Chain ID:", balance.ChainID)
	fmt.Println("Wallet:", walletAddress)
	fmt.Println("Raw balance (Wei):", balance.Balance)
	fmt.Println("Balance (AVAX):", balanceAVAX)
	fmt.Println("Decimals:", balance.Decimals)
	fmt.Println("Observed at:", time.Now().UTC().Format(time.RFC3339))

	fmt.Println("\nTransaction history:")

	pageToken := ""

	for pageNumber := 1; pageNumber <= 2; pageNumber++ {
		pageCtx, pageCancel := context.WithTimeout(
			context.Background(),
			10*time.Second,
		)

		page, err := client.ListTransactions(
			pageCtx,
			walletAddress,
			1,
			pageToken,
		)

		pageCancel()

		if err != nil {
			return fmt.Errorf(
				"read transaction page %d: %w",
				pageNumber,
				err,
			)
		}

		fmt.Printf(
			"Page %d: %d transaction(s)\n",
			pageNumber,
			len(page.Transactions),
		)

		for _, entry := range page.Transactions {
			tx := entry.NativeTransaction

			fmt.Println("  Transaction:", tx.TxHash)
			fmt.Println("  Block:", tx.BlockNumber)
			fmt.Println("  Status:", tx.TxStatus)
			fmt.Println("  From:", tx.From.Address)
			fmt.Println("  To:", tx.To.Address)
			fmt.Println("  Value (Wei):", tx.Value)
		}

		if page.NextPageToken == "" {
			fmt.Println("No more pages.")
			break
		}

		pageToken = page.NextPageToken
	}

	return nil
}
