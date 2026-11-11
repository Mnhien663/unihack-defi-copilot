package avalanche

import (
	"context"
	"math/big"
	"testing"
	"time"
)

const testWalletAddress = "0x0000000000000000000000000000000000000001"

func TestGetLatestBlockNumber(t *testing.T) {
	t.Setenv(
		"AVALANCHE_RPC_URL",
		"https://api.avax-test.network/ext/bc/C/rpc",
	)

	client, err := InitClient()
	if err != nil {
		t.Fatalf("InitClient() returned error: %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(
		context.Background(),
		5*time.Second,
	)
	defer cancel()

	blockNumber, err := GetLatestBlockNumber(ctx, client)
	if err != nil {
		t.Fatalf("GetLatestBlockNumber() returned error: %v", err)
	}

	if blockNumber == 0 {
		t.Fatal("GetLatestBlockNumber() returned zero")
	}

	t.Logf("Latest Avalanche Fuji block: %d", blockNumber)
}

func TestGetNativeBalance(t *testing.T) {
	t.Setenv(
		"AVALANCHE_RPC_URL",
		"https://api.avax-test.network/ext/bc/C/rpc",
	)

	client, err := InitClient()
	if err != nil {
		t.Fatalf("InitClient() returned error: %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(
		context.Background(),
		5*time.Second,
	)
	defer cancel()

	balanceWei, err := GetNativeBalance(
		ctx,
		client,
		testWalletAddress,
	)
	if err != nil {
		t.Fatalf("GetNativeBalance() returned error: %v", err)
	}

	if balanceWei.Sign() < 0 {
		t.Fatal("GetNativeBalance() returned a negative balance")
	}

	balanceAVAX, err := WeiToAVAX(balanceWei)
	if err != nil {
		t.Fatalf("WeiToAVAX() returned error: %v", err)
	}

	t.Logf("Wallet balance: %s Wei", balanceWei.String())
	t.Logf("Wallet balance: %s AVAX", balanceAVAX)
}

func TestWeiToAVAX(t *testing.T) {
	wei, ok := new(big.Int).SetString(
		"1500000000000000000",
		10,
	)
	if !ok {
		t.Fatal("could not create Wei value")
	}

	result, err := WeiToAVAX(wei)
	if err != nil {
		t.Fatalf("WeiToAVAX() returned error: %v", err)
	}

	if result != "1.5" {
		t.Fatalf("WeiToAVAX() = %s, want 1.5", result)
	}
}
