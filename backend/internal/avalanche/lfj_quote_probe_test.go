package avalanche

import (
	"context"
	"math/big"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
)

func TestLFJQuoteProbe(t *testing.T) {
	if os.Getenv("LFJ_QUOTE_PROBE") != "1" {
		t.Skip("set LFJ_QUOTE_PROBE=1 to run against Fuji")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	cfg, err := LoadRPCConfig()
	if err != nil {
		t.Fatal(err)
	}
	client, err := NewRPCClient(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	height, err := client.BlockNumber(ctx)
	if err != nil {
		t.Fatal(err)
	}
	block := new(big.Int).SetUint64(height)

	pool := common.HexToAddress("0xc3b7307184381f65B398d385a71EF0490239a2b3")
	router := common.HexToAddress("0x18556DA13313f3532c54711497A8FedAC273220E")
	usdt := common.HexToAddress("0xAb231A5744C8E6c45481754928cCfFFFD4aa0732")
	usdc := common.HexToAddress("0xB6076C93701D6a07266c31066B298AeC6dd65c2d")

	contractABI, err := abi.JSON(strings.NewReader(`[
		{"type":"function","name":"getTokenX","inputs":[],"outputs":[{"type":"address"}]},
		{"type":"function","name":"getTokenY","inputs":[],"outputs":[{"type":"address"}]},
		{"type":"function","name":"getSwapOut",
		 "inputs":[{"type":"address"},{"type":"uint128"},{"type":"bool"}],
		 "outputs":[{"type":"uint128"},{"type":"uint128"},{"type":"uint128"}]}
	]`))
	if err != nil {
		t.Fatal(err)
	}

	call := func(to common.Address, method string, args ...any) []any {
		t.Helper()

		data, err := contractABI.Pack(method, args...)
		if err != nil {
			t.Fatalf("encode %s: %v", method, err)
		}
		raw, err := client.CallContract(ctx, ethereum.CallMsg{
			To:   &to,
			Data: data,
		}, block)
		if err != nil {
			t.Fatalf("call %s at block %d: %v", method, height, err)
		}
		values, err := contractABI.Unpack(method, raw)
		if err != nil {
			t.Fatalf("decode %s: %v", method, err)
		}
		return values
	}

	tokenX := call(pool, "getTokenX")[0].(common.Address)
	tokenY := call(pool, "getTokenY")[0].(common.Address)
	t.Logf("block=%d tokenX=%s tokenY=%s", height, tokenX.Hex(), tokenY.Hex())

	if !((tokenX == usdt && tokenY == usdc) ||
		(tokenX == usdc && tokenY == usdt)) {
		t.Fatal("pool tokens do not match the expected USDT/USDC pair")
	}

	for _, direction := range []struct {
		name     string
		swapForY bool
	}{
		{"X -> Y", true},
		{"Y -> X", false},
	} {
		values := call(router, "getSwapOut", pool, big.NewInt(1_000_000), direction.swapForY)
		left := values[0].(*big.Int)
		out := values[1].(*big.Int)
		fee := values[2].(*big.Int)

		t.Logf("%s: amountInLeft=%s amountOut=%s fee=%s",
			direction.name, left, out, fee)

		if left.Sign() != 0 || out.Sign() <= 0 {
			t.Errorf("%s did not return a full, positive quote", direction.name)
		}
	}
}
