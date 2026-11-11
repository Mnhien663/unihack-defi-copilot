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
