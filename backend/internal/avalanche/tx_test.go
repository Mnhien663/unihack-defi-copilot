package avalanche

import (
	"encoding/json"
	"math/big"
	"os"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

func swapBuildTestRequest() (BuildSwapRequest, time.Time) {
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	block := uint64(60886568)

	input := Asset{
		ChainID:  43113,
		Address:  lfjFujiTokenX,
		Symbol:   "USDT",
		Decimals: 6,
	}
	output := Asset{
		ChainID:  43113,
		Address:  lfjFujiTokenY,
		Symbol:   "USDC",
		Decimals: 6,
	}

	return BuildSwapRequest{
		OwnerAddress: "0x0000000000000000000000000000000000000001",
		Deadline:     uint64(now.Add(20 * time.Second).Unix()),
		Quote: SwapQuoteResult{
			Protocol:      "LFJ",
			RouterAddress: lfjFujiRouter,
			TokenPath:     []Asset{input, output},
			Hops: []SwapQuoteHop{
				{
					PoolAddress: lfjFujiPair,
					Version:     "V2.2",
					BinStep:     1,
				},
			},
			AmountIn: TokenAmount{
				Asset: input, Raw: "1000000", Formatted: "1",
			},
			AmountOut: TokenAmount{
				Asset: output, Raw: "999900", Formatted: "0.9999",
			},
			MinAmountOut: TokenAmount{
				Asset: output, Raw: "994900", Formatted: "0.9949",
			},
			Fee: TokenAmount{
				Asset: input, Raw: "100", Formatted: "0.0001",
			},
			SlippageBps: 50,
			ExpiresAt:   now.Add(30 * time.Second),
			Provenance: Provenance{
				Source:      "LFJ V2.2 via Avalanche RPC",
				SourceType:  "rpc",
				ChainID:     43113,
				BlockNumber: &block,
				ObservedAt:  now,
				Mode:        "live",
			},
		},
	}, now
}

func TestBuildSwapTxMatchesSummary(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		name := "USDT to USDC"
		if reverse {
			name = "USDC to USDT"
		}

		t.Run(name, func(t *testing.T) {
			builder, err := NewSwapTxBuilder()
			if err != nil {
				t.Fatal(err)
			}
			request, now := swapBuildTestRequest()

			if reverse {
				path := request.Quote.TokenPath
				path[0], path[1] = path[1], path[0]
				request.Quote.AmountIn.Asset = path[0]
				request.Quote.AmountOut.Asset = path[1]
				request.Quote.MinAmountOut.Asset = path[1]
				request.Quote.Fee.Asset = path[0]
			}

			// Caller-provided formatted values must not control the summary.
			request.Quote.AmountIn.Formatted = "incorrect"
			request.Quote.MinAmountOut.Formatted = "incorrect"

			result, err := builder.BuildSwapTx(request, now)
			if err != nil {
				t.Fatal(err)
			}

			tx := result.Prepared.Tx
			summary := result.Summary
			owner := common.HexToAddress(request.OwnerAddress).Hex()

			if tx.ChainID != 43113 ||
				tx.From != owner ||
				tx.To != common.HexToAddress(fujiExecutionManager).Hex() ||
				tx.Value != "0" {
				t.Fatalf("unexpected transaction envelope: %+v", tx)
			}
			if summary.OwnerAddress != owner ||
				summary.RecipientAddress != owner ||
				summary.ManagerAddress != tx.To {
				t.Fatalf("unexpected ownership: %+v", summary)
			}
			if summary.AmountIn.Raw != "1000000" ||
				summary.AmountIn.Formatted != "1" ||
				summary.MinAmountOut.Raw != "994900" ||
				summary.MinAmountOut.Formatted != "0.9949" ||
				summary.Deadline != request.Deadline ||
				!summary.QuoteExpiresAt.Equal(request.Quote.ExpiresAt) {
				t.Fatalf("unexpected summary: %+v", summary)
			}

			raw, err := hexutil.Decode(tx.Data)
			if err != nil {
				t.Fatal(err)
			}
			if len(raw) != 164 {
				t.Fatalf("calldata bytes=%d; want 164", len(raw))
			}

			method, err := builder.abi.MethodById(raw[:4])
			if err != nil {
				t.Fatal(err)
			}
			if method.Name != "executeSwap" {
				t.Fatalf("unexpected method: %s", method.Name)
			}

			args, err := method.Inputs.Unpack(raw[4:])
			if err != nil {
				t.Fatal(err)
			}

			if args[0].(common.Address).Hex() != summary.TokenIn.Address ||
				args[1].(common.Address).Hex() != summary.TokenOut.Address ||
				args[2].(*big.Int).String() != summary.AmountIn.Raw ||
				args[3].(*big.Int).String() != summary.MinAmountOut.Raw ||
				args[4].(*big.Int).Uint64() != summary.Deadline {
				t.Fatal("decoded calldata differs from human-readable summary")
			}

			if summary.AmountIn.Asset != summary.TokenIn ||
				summary.MinAmountOut.Asset != summary.TokenOut {
				t.Fatal("summary amount assets do not match the route")
			}
			if !result.Prepared.Provenance.ObservedAt.Equal(
				request.Quote.Provenance.ObservedAt,
			) {
				t.Fatal("builder must preserve quote observation time")
			}
		})
	}
}

func TestBuildSwapTxRejectsInvalidIntent(t *testing.T) {
	tests := []struct {
		name   string
		change func(*BuildSwapRequest, time.Time)
	}{
		{"invalid owner", func(r *BuildSwapRequest, _ time.Time) {
			r.OwnerAddress = "bad"
		}},
		{"zero owner", func(r *BuildSwapRequest, _ time.Time) {
			r.OwnerAddress = common.Address{}.Hex()
		}},
		{"manager as owner", func(r *BuildSwapRequest, _ time.Time) {
			r.OwnerAddress = fujiExecutionManager
		}},
		{"wrong chain", func(r *BuildSwapRequest, _ time.Time) {
			r.Quote.Provenance.ChainID = 43114
		}},
		{"wrong router", func(r *BuildSwapRequest, _ time.Time) {
			r.Quote.RouterAddress = r.OwnerAddress
		}},
		{"wrong pool", func(r *BuildSwapRequest, _ time.Time) {
			r.Quote.Hops[0].PoolAddress = r.OwnerAddress
		}},
		{"same token", func(r *BuildSwapRequest, _ time.Time) {
			r.Quote.TokenPath[1] = r.Quote.TokenPath[0]
		}},
		{"wrong decimals", func(r *BuildSwapRequest, _ time.Time) {
			r.Quote.TokenPath[0].Decimals = 18
		}},
		{"zero input", func(r *BuildSwapRequest, _ time.Time) {
			r.Quote.AmountIn.Raw = "0"
		}},
		{"negative input", func(r *BuildSwapRequest, _ time.Time) {
			r.Quote.AmountIn.Raw = "-1"
		}},
		{"uint128 overflow", func(r *BuildSwapRequest, _ time.Time) {
			r.Quote.AmountIn.Raw =
				new(big.Int).Lsh(big.NewInt(1), 128).String()
		}},
		{"changed minimum", func(r *BuildSwapRequest, _ time.Time) {
			r.Quote.MinAmountOut.Raw = "1"
		}},
		{"minimum asset changed", func(r *BuildSwapRequest, _ time.Time) {
			r.Quote.MinAmountOut.Asset = r.Quote.AmountIn.Asset
		}},
		{"slippage over cap", func(r *BuildSwapRequest, _ time.Time) {
			r.Quote.SlippageBps = 501
		}},
		{"quote expires now", func(r *BuildSwapRequest, now time.Time) {
			r.Quote.ExpiresAt = now
		}},
		{"missing expiry", func(r *BuildSwapRequest, _ time.Time) {
			r.Quote.ExpiresAt = time.Time{}
		}},
		{"extended lifetime", func(r *BuildSwapRequest, now time.Time) {
			r.Quote.ExpiresAt = now.Add(61 * time.Second)
		}},
		{"deadline expires now", func(r *BuildSwapRequest, now time.Time) {
			r.Deadline = uint64(now.Unix())
		}},
		{"deadline beyond quote", func(r *BuildSwapRequest, _ time.Time) {
			r.Deadline = uint64(r.Quote.ExpiresAt.Unix()) + 1
		}},
		{"future observation", func(r *BuildSwapRequest, now time.Time) {
			r.Quote.Provenance.ObservedAt = now.Add(time.Second)
		}},
		{"snapshot source", func(r *BuildSwapRequest, _ time.Time) {
			r.Quote.Provenance.Mode = "snapshot"
		}},
		{"demo quote", func(r *BuildSwapRequest, _ time.Time) {
			r.Quote.Provenance.IsDemo = true
		}},
		{"missing block", func(r *BuildSwapRequest, _ time.Time) {
			r.Quote.Provenance.BlockNumber = nil
		}},
	}

	builder, err := NewSwapTxBuilder()
	if err != nil {
		t.Fatal(err)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request, now := swapBuildTestRequest()
			tt.change(&request, now)

			result, err := builder.BuildSwapTx(request, now)
			if err == nil {
				t.Fatal("expected invalid intent to be rejected")
			}
			if result.Prepared.Tx.Data != "" {
				t.Fatal("invalid intent must not return transaction calldata")
			}
		})
	}
}

func TestBuildSwapTxAcceptsDeadlineAtQuoteExpiry(t *testing.T) {
	builder, err := NewSwapTxBuilder()
	if err != nil {
		t.Fatal(err)
	}
	request, now := swapBuildTestRequest()
	request.Deadline = uint64(request.Quote.ExpiresAt.Unix())

	if _, err := builder.BuildSwapTx(request, now); err != nil {
		t.Fatal(err)
	}
}

func TestWriteUnsignedSwapExample(t *testing.T) {
	if os.Getenv("WRITE_SWAP_FIXTURE") != "1" {
		t.Skip("fixture generation is opt-in")
	}

	builder, err := NewSwapTxBuilder()
	if err != nil {
		t.Fatal(err)
	}

	request, now := swapBuildTestRequest()
	result, err := builder.BuildSwapTx(request, now)
	if err != nil {
		t.Fatal(err)
	}

	fixture := struct {
		Description string               `json:"description"`
		Synthetic   bool                 `json:"synthetic"`
		ReferenceAt time.Time            `json:"reference_at"`
		Request     BuildSwapRequest     `json:"request"`
		Result      BuiltSwapTransaction `json:"result"`
	}{
		Description: "Synthetic unsigned swap example; not live evidence or a transaction ready for signing.",
		Synthetic:   true,
		ReferenceAt: now,
		Request:     request,
		Result:      result,
	}

	body, err := json.MarshalIndent(fixture, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	body = append(body, '\n')

	const path = "testdata/swap_unsigned_example.json"
	if err := os.WriteFile(path, body, 0644); err != nil {
		t.Fatal(err)
	}

	t.Log("Wrote", path)
}
