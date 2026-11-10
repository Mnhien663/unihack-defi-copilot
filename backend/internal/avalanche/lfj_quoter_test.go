package avalanche

import (
	"context"
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
)

const lfjTestBlock = 60841002

type lfjQuoteTestRPC struct {
	RPCReader  // Any unexpected RPC method fails the test.
	t          *testing.T
	quoterABI  abi.ABI
	cfg        LFJQuoteConfig
	swapForY   bool
	left       int64
	chainCalls int
	swapCalls  int
	calls      int
}

func (s *lfjQuoteTestRPC) ChainID(context.Context) (*big.Int, error) {
	s.chainCalls++
	return big.NewInt(FujiChainID), nil
}

func (s *lfjQuoteTestRPC) BlockNumber(context.Context) (uint64, error) {
	return lfjTestBlock, nil
}

func (s *lfjQuoteTestRPC) CallContract(
	_ context.Context,
	msg ethereum.CallMsg,
	block *big.Int,
) ([]byte, error) {
	s.t.Helper()
	s.calls++

	if block == nil || block.Cmp(big.NewInt(lfjTestBlock)) != 0 {
		s.t.Fatalf("contract call used block %v; want %d", block, lfjTestBlock)
	}
	if msg.To == nil || len(msg.Data) < 4 {
		s.t.Fatalf("invalid contract call: %+v", msg)
	}

	pool := common.HexToAddress(s.cfg.PoolAddress)
	router := common.HexToAddress(s.cfg.RouterAddress)
	tokenX := common.HexToAddress(s.cfg.TokenA.Address)
	tokenY := common.HexToAddress(s.cfg.TokenB.Address)

	if string(msg.Data) == string(common.FromHex("0x313ce567")) {
		if *msg.To != tokenX && *msg.To != tokenY {
			s.t.Fatalf("decimals() called on unexpected address %s", msg.To.Hex())
		}
		return big.NewInt(6).FillBytes(make([]byte, 32)), nil
	}

	method, err := s.quoterABI.MethodById(msg.Data[:4])
	if err != nil {
		s.t.Fatal(err)
	}

	switch method.Name {
	case "getTokenX", "getTokenY":
		if *msg.To != pool {
			s.t.Fatalf("%s called on %s; want pool %s", method.Name, msg.To.Hex(), pool.Hex())
		}
		if method.Name == "getTokenX" {
			return method.Outputs.Pack(tokenX)
		}
		return method.Outputs.Pack(tokenY)

	case "getSwapOut":
		s.swapCalls++
		if *msg.To != router {
			s.t.Fatalf("getSwapOut called on %s; want router %s", msg.To.Hex(), router.Hex())
		}

		args, err := method.Inputs.Unpack(msg.Data[4:])
		if err != nil {
			s.t.Fatal(err)
		}
		if args[0].(common.Address) != pool ||
			args[1].(*big.Int).Cmp(big.NewInt(1_000_000)) != 0 ||
			args[2].(bool) != s.swapForY {
			s.t.Fatalf("unexpected getSwapOut args: %v; want swapForY=%t", args, s.swapForY)
		}

		return method.Outputs.Pack(
			big.NewInt(s.left),
			big.NewInt(999_900),
			big.NewInt(100),
		)

	default:
		s.t.Fatalf("unexpected method %s", method.Name)
		return nil, nil
	}
}

func TestLFJQuoterExactInput(t *testing.T) {
	cfg := LFJQuoteConfig{
		RouterAddress: "0x18556DA13313f3532c54711497A8FedAC273220E",
		PoolAddress:   "0xc3b7307184381f65B398d385a71EF0490239a2b3",
		TokenA: Asset{
			ChainID:  uint64(FujiChainID),
			Address:  "0xAb231A5744C8E6c45481754928cCfFFFD4aa0732",
			Symbol:   "USDT",
			Decimals: 6,
		},
		TokenB: Asset{
			ChainID:  uint64(FujiChainID),
			Address:  "0xB6076C93701D6a07266c31066B298AeC6dd65c2d",
			Symbol:   "USDC",
			Decimals: 6,
		},
		BinStep: 1,
	}

	contractABI, err := abi.JSON(strings.NewReader(lfjQuoteABI))
	if err != nil {
		t.Fatal(err)
	}

	for _, tt := range []struct {
		name     string
		path     []Asset
		raw      string
		swapForY bool
		left     int64
		wantErr  string
		wantRPC  bool
	}{
		{"USDT to USDC", []Asset{cfg.TokenA, cfg.TokenB}, "1000000", true, 0, "", true},
		{"USDC to USDT", []Asset{cfg.TokenB, cfg.TokenA}, "1000000", false, 0, "", true},
		{"partial quote", []Asset{cfg.TokenA, cfg.TokenB}, "1000000", true, 1, "did not consume the full input", true},
		{"zero input", []Asset{cfg.TokenA, cfg.TokenB}, "0", true, 0, "positive uint128", false},
		{"uint128 overflow", []Asset{cfg.TokenA, cfg.TokenB}, "340282366920938463463374607431768211456", true, 0, "positive uint128", false},
		{"same token", []Asset{cfg.TokenA, cfg.TokenA}, "1000000", true, 0, "must differ", false},
		{
			name: "wrong request decimals",
			path: []Asset{
				{ChainID: uint64(FujiChainID), Address: cfg.TokenA.Address, Decimals: 18},
				cfg.TokenB,
			},
			raw:     "1000000",
			wantErr: "metadata mismatch",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rpc := &lfjQuoteTestRPC{
				t: t, quoterABI: contractABI, cfg: cfg,
				swapForY: tt.swapForY, left: tt.left,
			}

			quoter, err := NewLFJQuoter(rpc, cfg)
			if err != nil {
				t.Fatal(err)
			}

			got, err := quoter.QuoteExactInput(context.Background(), SwapQuoteRequest{
				TokenPath: tt.path, AmountInRaw: tt.raw,
			})

			if tt.wantRPC {
				if rpc.chainCalls != 1 || rpc.calls != 5 || rpc.swapCalls != 1 {
					t.Fatalf("chain=%d contract=%d swap=%d; want 1, 5, 1",
						rpc.chainCalls, rpc.calls, rpc.swapCalls)
				}
			} else if rpc.chainCalls != 0 || rpc.calls != 0 {
				t.Fatal("invalid request reached RPC")
			}

			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("want error containing %q, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}

			if got.AmountIn.Asset != tt.path[0] ||
				got.AmountIn.Raw != tt.raw ||
				got.AmountOut.Asset != tt.path[1] ||
				got.AmountOut.Raw != "999900" ||
				got.AmountOut.Formatted != "0.9999" ||
				got.Fee.Raw != "100" ||
				got.Fee.Formatted != "0.0001" ||
				got.Fee.Asset != tt.path[0] {
				t.Fatalf("unexpected quote amounts: %+v", got)
			}
			if len(got.Hops) != 1 ||
				got.Hops[0].PoolAddress != cfg.PoolAddress ||
				got.Hops[0].Version != "V2.2" ||
				got.Hops[0].BinStep != 1 ||
				got.Provenance.BlockNumber == nil ||
				*got.Provenance.BlockNumber != lfjTestBlock ||
				got.Provenance.ObservedAt.IsZero() {
				t.Fatalf("unexpected route or provenance: %+v", got)
			}
		})
	}
}
