package avalanche

import (
	"context"
	"encoding/json"
	"math/big"
	"os"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

func TestFujiLiveSnapshot(t *testing.T) {
	if os.Getenv("AVALANCHE_LIVE_TEST") != "1" {
		t.Skip("set AVALANCHE_LIVE_TEST=1 to run")
	}

	const token = "0xcCCb83Fbe349fAebDE74Ce633D25B282E6FDd21A"
	const owner = "0x4DFb0d91C07A7C706F65F974953cE8b111D27Ee5"

	// Use owner as spender only to verify allowance reads.
	// This does not test approval for a DeFi protocol.
	const spender = owner

	ctx, cancel := context.WithTimeout(
		context.Background(),
		30*time.Second,
	)
	defer cancel()

	client, err := NewRPCClient(ctx, RPCConfig{
		URL:             "https://api.avax-test.network/ext/bc/C/rpc",
		ExpectedChainID: FujiChainID,
		Timeout:         10 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	blockNumber, err := GetLatestBlockNumber(ctx, client)
	if err != nil {
		t.Fatal(err)
	}

	block := new(big.Int).SetUint64(blockNumber)

	nativeBalance, err := client.BalanceAt(
		ctx,
		common.HexToAddress(owner),
		block,
	)
	if err != nil {
		t.Fatal(err)
	}

	tokenBalance, err := GetERC20Balance(
		ctx, client, token, owner, block,
	)
	if err != nil {
		t.Fatal(err)
	}

	allowance, err := GetERC20Allowance(
		ctx, client, token, owner, spender, block,
	)
	if err != nil {
		t.Fatal(err)
	}

	decimals, err := GetERC20Decimals(
		ctx, client, token, block,
	)
	if err != nil {
		t.Fatal(err)
	}

	snapshot := map[string]any{
		"chain_id":           FujiChainID,
		"rpc_url":            "https://api.avax-test.network/ext/bc/C/rpc",
		"block_number":       blockNumber,
		"block_hex":          "0x" + block.Text(16),
		"observed_at":        time.Now().UTC().Format(time.RFC3339),
		"token_address":      token,
		"owner_address":      owner,
		"spender_address":    spender,
		"native_balance_raw": nativeBalance.String(),
		"token_balance_raw":  tokenBalance.String(),
		"allowance_raw":      allowance.String(),
		"decimals":           decimals,
	}

	output, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		t.Fatal(err)
	}

	t.Logf("Fuji snapshot:\n%s", output)
}

func TestFujiLiveVerifyExecutedSwap(t *testing.T) {
	if os.Getenv("AVALANCHE_LIVE_TEST") != "1" {
		t.Skip("set AVALANCHE_LIVE_TEST=1 to run")
	}

	data, err := os.ReadFile(
		"testdata/swap_fuji_expected_intent.json",
	)
	if err != nil {
		t.Fatal(err)
	}

	var evidence struct {
		TransactionHash string             `json:"transaction_hash"`
		ExpectedIntent  ExpectedSwapIntent `json:"expected_intent"`
	}
	if err := json.Unmarshal(data, &evidence); err != nil {
		t.Fatal(err)
	}

	const expectedHash = "0xcb6292135bf452bd6669b21cc9fb8dac6bbd4b74854f1502e1cad0724232f0f0"
	if evidence.TransactionHash != expectedHash {
		t.Fatal("fixture contains an unexpected transaction hash")
	}

	ctx, cancel := context.WithTimeout(
		context.Background(),
		30*time.Second,
	)
	defer cancel()

	client, err := NewRPCClient(ctx, RPCConfig{
		URL:             "https://api.avax-test.network/ext/bc/C/rpc",
		ExpectedChainID: FujiChainID,
		Timeout:         10 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	reader, err := NewRPCReceiptReader(client)
	if err != nil {
		t.Fatal(err)
	}

	result, err := reader.VerifyTx(
		ctx,
		evidence.TransactionHash,
		evidence.ExpectedIntent,
	)
	if err != nil {
		t.Fatalf("VerifyTx: %v", err)
	}

	if result.Receipt.Status != TransactionStatusSucceeded {
		t.Fatalf("unexpected status: %s", result.Receipt.Status)
	}
	if !result.Verified {
		t.Fatal("swap was not verified")
	}
	if result.AmountOutRaw == nil || *result.AmountOutRaw != "9999" {
		t.Fatalf("unexpected amount out: %v", result.AmountOutRaw)
	}
	if result.Receipt.BlockNumber == nil ||
		*result.Receipt.BlockNumber != 60946687 {
		t.Fatal("unexpected mined block")
	}

	output, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Verified Fuji swap:\n%s", output)
}
