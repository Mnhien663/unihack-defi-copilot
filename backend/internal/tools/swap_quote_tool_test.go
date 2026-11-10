package tools

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"unihack-backend/internal/avalanche"
)

type swapQuoteTestQuoter func(
	context.Context,
	avalanche.SwapQuoteRequest,
) (avalanche.SwapQuoteResult, error)

func (f swapQuoteTestQuoter) QuoteExactInput(
	ctx context.Context,
	req avalanche.SwapQuoteRequest,
) (avalanche.SwapQuoteResult, error) {
	return f(ctx, req)
}

func TestGetSwapQuoteToolContract(t *testing.T) {
	cfg := avalanche.LFJQuoteConfig{
		RouterAddress: "0x18556DA13313f3532c54711497A8FedAC273220E",
		PoolAddress:   "0xc3b7307184381f65B398d385a71EF0490239a2b3",
		BinStep:       1,
		TokenA: avalanche.Asset{
			ChainID:  43113,
			Address:  "0xAb231A5744C8E6c45481754928cCfFFFD4aa0732",
			Symbol:   "USDT",
			Decimals: 6,
		},
		TokenB: avalanche.Asset{
			ChainID:  43113,
			Address:  "0xB6076C93701D6a07266c31066B298AeC6dd65c2d",
			Symbol:   "USDC",
			Decimals: 6,
		},
	}

	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	block := uint64(60841002)

	for _, tt := range []struct {
		name      string
		reverse   bool
		raw       string
		slippage  int
		quoteAge  time.Duration
		wantMin   string
		wantCode  string
		wantCalls int
	}{
		{"USDT to USDC", false, "1000000", 50, 0, "994900", "", 1},
		{"USDC to USDT", true, "1000000", 100, 0, "989901", "", 1},
		{"zero slippage", false, "1000000", 0, 0, "999900", "", 1},
		{"zero input", false, "0", 50, 0, "", "INVALID_AMOUNT", 0},
		{"expired at boundary", false, "1000000", 50, 30 * time.Second, "", "QUOTE_EXPIRED", 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			in, out := cfg.TokenA, cfg.TokenB
			if tt.reverse {
				in, out = out, in
			}

			calls := 0
			quoter := swapQuoteTestQuoter(func(
				_ context.Context,
				req avalanche.SwapQuoteRequest,
			) (avalanche.SwapQuoteResult, error) {
				calls++
				if len(req.TokenPath) != 2 ||
					req.TokenPath[0] != in ||
					req.TokenPath[1] != out ||
					req.AmountInRaw != tt.raw {
					t.Fatalf("unexpected quoter request: %+v", req)
				}

				return avalanche.SwapQuoteResult{
					Protocol:      "LFJ",
					RouterAddress: cfg.RouterAddress,
					TokenPath:     []avalanche.Asset{in, out},
					Hops: []avalanche.SwapQuoteHop{
						{PoolAddress: cfg.PoolAddress, Version: "V2.2", BinStep: cfg.BinStep},
					},
					AmountIn:  avalanche.TokenAmount{Asset: in, Raw: "1000000", Formatted: "1"},
					AmountOut: avalanche.TokenAmount{Asset: out, Raw: "999900", Formatted: "0.9999"},
					Fee:       avalanche.TokenAmount{Asset: in, Raw: "100", Formatted: "0.0001"},
					Provenance: avalanche.Provenance{
						Source:      "quote_test_fixture",
						SourceType:  "mock",
						ChainID:     43113,
						BlockNumber: &block,
						ObservedAt:  now.Add(-tt.quoteAge),
						IsDemo:      true,
					},
				}, nil
			})

			tool, err := NewGetSwapQuoteTool(quoter, cfg)
			if err != nil {
				t.Fatal(err)
			}
			tool.now = func() time.Time { return now }

			args, err := json.Marshal(map[string]any{
				"chain_id":      43113,
				"token_in":      in.Address,
				"token_out":     out.Address,
				"amount_in_raw": tt.raw,
				"slippage_bps":  tt.slippage,
			})
			if err != nil {
				t.Fatal(err)
			}

			result, err := tool.Execute(context.Background(), args)
			if err != nil {
				t.Fatal(err)
			}
			if calls != tt.wantCalls {
				t.Fatalf("quoter calls = %d; want %d", calls, tt.wantCalls)
			}

			if tt.wantCode != "" {
				if result.Error == nil || result.Error.Code != tt.wantCode {
					t.Fatalf("want %s, got %+v", tt.wantCode, result.Error)
				}
				return
			}
			if result.Error != nil {
				t.Fatalf("unexpected tool error: %+v", result.Error)
			}

			var data struct {
				avalanche.SwapQuoteResult
				MinOut      avalanche.TokenAmount `json:"min_out"`
				SlippageBPS int                   `json:"slippage_bps"`
				ExpiresAt   time.Time             `json:"expires_at"`
			}
			if err := json.Unmarshal(result.Data, &data); err != nil {
				t.Fatal(err)
			}

			if data.AmountIn.Raw != tt.raw ||
				data.AmountIn.Asset != in ||
				data.AmountOut.Raw != "999900" ||
				data.AmountOut.Asset != out ||
				data.Fee.Raw != "100" ||
				data.Fee.Asset != in ||
				data.MinOut.Raw != tt.wantMin ||
				data.MinOut.Asset != out ||
				data.SlippageBPS != tt.slippage {
				t.Fatalf("unexpected quote amounts: %s", result.Data)
			}

			if len(data.TokenPath) != 2 ||
				data.TokenPath[0] != in ||
				data.TokenPath[1] != out ||
				len(data.Hops) != 1 ||
				data.Hops[0].PoolAddress != cfg.PoolAddress ||
				!data.ExpiresAt.Equal(now.Add(30*time.Second)) ||
				result.Source != "quote_test_fixture" ||
				result.ChainID != 43113 ||
				result.BlockNumber == nil ||
				*result.BlockNumber != block ||
				!result.ObservedAt.Equal(now) {
				t.Fatalf("unexpected route, expiry, or provenance: %s", result.Data)
			}
		})
	}
}
