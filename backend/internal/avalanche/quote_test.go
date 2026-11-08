package avalanche

import (
	"context"
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
)

type quoteRPCStub struct {
	reader        *LFJQuoteReader
	contractCalls int
	swapForY      bool
	amountIn      *big.Int
	left          *big.Int
	output        *big.Int
	fee           *big.Int
}

func (s *quoteRPCStub) ChainID(context.Context) (*big.Int, error) {
	return big.NewInt(FujiChainID), nil
}

func (s *quoteRPCStub) BlockNumber(context.Context) (uint64, error) {
	return 12345, nil
}

func (s *quoteRPCStub) BalanceAt(
	context.Context,
	common.Address,
	*big.Int,
) (*big.Int, error) {
	return nil, errors.New("quote must not read wallet balance")
}

func (s *quoteRPCStub) CallContract(
	ctx context.Context,
	msg ethereum.CallMsg,
	block *big.Int,
) ([]byte, error) {
	s.contractCalls++

	if block == nil || block.Cmp(big.NewInt(12345)) != 0 {
		return nil, errors.New("expected pinned block 12345")
	}
	if msg.To == nil || len(msg.Data) < 4 {
		return nil, errors.New("invalid contract call")
	}

	r := s.reader

	// ERC-20 decimals() selector.
	if string(msg.Data[:4]) == string(common.FromHex("0x313ce567")) {
		if *msg.To != r.tokenX && *msg.To != r.tokenY {
			return nil, errors.New("unexpected metadata token")
		}
		return big.NewInt(6).FillBytes(make([]byte, 32)), nil
	}

	method, err := r.abi.MethodById(msg.Data[:4])
	if err != nil {
		return nil, err
	}

	switch method.Name {
	case "getTokenX":
		if *msg.To != r.pair {
			return nil, errors.New("getTokenX sent to wrong address")
		}
		return method.Outputs.Pack(r.tokenX)

	case "getTokenY":
		if *msg.To != r.pair {
			return nil, errors.New("getTokenY sent to wrong address")
		}
		return method.Outputs.Pack(r.tokenY)

	case "getSwapOut":
		if *msg.To != r.router {
			return nil, errors.New("quote sent to wrong router")
		}

		args, err := method.Inputs.Unpack(msg.Data[4:])
		if err != nil {
			return nil, err
		}
		if args[0].(common.Address) != r.pair {
			return nil, errors.New("wrong quote pair")
		}
		s.amountIn = new(big.Int).Set(args[1].(*big.Int))
		s.swapForY = args[2].(bool)

		return method.Outputs.Pack(s.left, s.output, s.fee)

	default:
		return nil, errors.New("unexpected contract method")
	}
}

func newQuoteTestReader(t *testing.T) (*LFJQuoteReader, *quoteRPCStub) {
	t.Helper()

	client := &quoteRPCStub{
		left:   big.NewInt(0),
		output: big.NewInt(999900),
		fee:    big.NewInt(100),
	}
	reader, err := NewLFJQuoteReader(client)
	if err != nil {
		t.Fatal(err)
	}
	client.reader = reader
	return reader, client
}

func quoteTestRequest() SwapQuoteRequest {
	return SwapQuoteRequest{
		TokenPath: []Asset{
			{
				ChainID:  43113,
				Address:  lfjFujiTokenX,
				Decimals: 6,
			},
			{
				ChainID:  43113,
				Address:  lfjFujiTokenY,
				Decimals: 6,
			},
		},
		AmountInRaw: "1000000",
	}
}

func TestLFJQuoteReadsBothDirections(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		name := "X to Y"
		if reverse {
			name = "Y to X"
		}

		t.Run(name, func(t *testing.T) {
			reader, client := newQuoteTestReader(t)
			request := quoteTestRequest()
			if reverse {
				request.TokenPath[0], request.TokenPath[1] =
					request.TokenPath[1], request.TokenPath[0]
			}

			before := time.Now().UTC()
			result, err := reader.QuoteExactInput(
				context.Background(), request,
			)
			after := time.Now().UTC()
			if err != nil {
				t.Fatal(err)
			}

			if client.contractCalls != 5 {
				t.Fatalf("contract calls=%d; want 5", client.contractCalls)
			}
			if client.swapForY != !reverse ||
				client.amountIn.Cmp(big.NewInt(1000000)) != 0 {
				t.Fatal("wrong router quote arguments")
			}

			if result.AmountIn.Raw != "1000000" ||
				result.AmountOut.Raw != "999900" ||
				result.AmountOut.Formatted != "0.9999" ||
				result.MinAmountOut.Raw != "994900" ||
				result.MinAmountOut.Formatted != "0.9949" ||
				result.Fee.Raw != "100" ||
				result.Fee.Formatted != "0.0001" ||
				result.SlippageBps != 50 {
				t.Fatalf("unexpected amounts: %+v", result)
			}

			if result.Fee.Asset.Address !=
				common.HexToAddress(request.TokenPath[0].Address).Hex() {
				t.Fatal("fee must use input token")
			}
			if result.MinAmountOut.Asset.Address !=
				common.HexToAddress(request.TokenPath[1].Address).Hex() {
				t.Fatal("minimum output must use output token")
			}

			if result.RouterAddress != reader.router.Hex() ||
				len(result.Hops) != 1 ||
				result.Hops[0].PoolAddress != reader.pair.Hex() ||
				result.Hops[0].Version != "V2.2" ||
				result.Hops[0].BinStep != 1 {
				t.Fatal("unexpected route")
			}

			p := result.Provenance
			if p.BlockNumber == nil || *p.BlockNumber != 12345 ||
				p.ChainID != 43113 || p.SourceType != "rpc" ||
				p.Mode != "live" || p.IsDemo ||
				p.ObservedAt.Before(before) ||
				p.ObservedAt.After(after) {
				t.Fatalf("unexpected provenance: %+v", p)
			}

			if result.ExpiresAt.Before(before.Add(60*time.Second)) ||
				result.ExpiresAt.After(after.Add(60*time.Second)) {
				t.Fatal("expiry must be 60 seconds from quote start")
			}
		})
	}
}

func TestLFJQuoteRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name   string
		change func(*SwapQuoteRequest)
	}{
		{"zero input", func(r *SwapQuoteRequest) {
			r.AmountInRaw = "0"
		}},
		{"negative input", func(r *SwapQuoteRequest) {
			r.AmountInRaw = "-1"
		}},
		{"fractional raw input", func(r *SwapQuoteRequest) {
			r.AmountInRaw = "1.5"
		}},
		{"uint128 overflow", func(r *SwapQuoteRequest) {
			r.AmountInRaw = new(big.Int).Lsh(big.NewInt(1), 128).String()
		}},
		{"same token", func(r *SwapQuoteRequest) {
			r.TokenPath[1] = r.TokenPath[0]
		}},
		{"unsupported token", func(r *SwapQuoteRequest) {
			r.TokenPath[1].Address =
				"0x0000000000000000000000000000000000000099"
		}},
		{"invalid address", func(r *SwapQuoteRequest) {
			r.TokenPath[0].Address = "bad"
		}},
		{"wrong chain", func(r *SwapQuoteRequest) {
			r.TokenPath[0].ChainID = 43114
		}},
		{"missing path", func(r *SwapQuoteRequest) {
			r.TokenPath = nil
		}},
		{"excess slippage", func(r *SwapQuoteRequest) {
			bps := uint16(501)
			r.SlippageBps = &bps
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader, client := newQuoteTestReader(t)
			request := quoteTestRequest()
			tt.change(&request)

			_, err := reader.QuoteExactInput(
				context.Background(), request,
			)
			if err == nil {
				t.Fatal("expected invalid input to be rejected")
			}
			if client.contractCalls != 0 {
				t.Fatal("invalid input must not reach contract calls")
			}
		})
	}
}

func TestLFJQuoteRejectsIncorrectDecimals(t *testing.T) {
	reader, client := newQuoteTestReader(t)
	request := quoteTestRequest()
	request.TokenPath[0].Decimals = 18

	_, err := reader.QuoteExactInput(context.Background(), request)
	if err == nil {
		t.Fatal("expected decimals mismatch to be rejected")
	}
	if client.amountIn != nil {
		t.Fatal("decimals mismatch must be rejected before router quote")
	}
}

func TestLFJQuoteSlippageAndFloor(t *testing.T) {
	tests := []struct {
		name   string
		bps    uint16
		output int64
		want   string
	}{
		{"explicit zero slippage", 0, 999900, "999900"},
		{"maximum slippage", 500, 999900, "949905"},
		{"floor fractional raw unit", 50, 101, "100"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader, client := newQuoteTestReader(t)
			client.output = big.NewInt(tt.output)

			request := quoteTestRequest()
			bps := tt.bps
			request.SlippageBps = &bps

			result, err := reader.QuoteExactInput(
				context.Background(), request,
			)
			if err != nil {
				t.Fatal(err)
			}
			if result.MinAmountOut.Raw != tt.want ||
				result.SlippageBps != tt.bps {
				t.Fatalf("minOut=%s, bps=%d; want %s, %d",
					result.MinAmountOut.Raw,
					result.SlippageBps,
					tt.want,
					tt.bps,
				)
			}
		})
	}
}

func TestLFJQuoteRejectsInsufficientLiquidity(t *testing.T) {
	tests := []struct {
		name   string
		left   int64
		output int64
	}{
		{"partial input consumption", 1, 999900},
		{"zero output", 0, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader, client := newQuoteTestReader(t)
			client.left = big.NewInt(tt.left)
			client.output = big.NewInt(tt.output)

			_, err := reader.QuoteExactInput(
				context.Background(), quoteTestRequest(),
			)
			if !errors.Is(err, ErrInsufficientLiquidity) {
				t.Fatalf("want insufficient liquidity, got %v", err)
			}
		})
	}
}

func TestValidateQuoteExpiry(t *testing.T) {
	expiry := time.Date(2026, 11, 8, 8, 0, 0, 0, time.UTC)

	tests := []struct {
		name    string
		expires time.Time
		now     time.Time
		wantErr bool
		expired bool
	}{
		{
			name:    "before expiry",
			expires: expiry,
			now:     expiry.Add(-time.Nanosecond),
		},
		{
			name:    "exactly at expiry",
			expires: expiry,
			now:     expiry,
			wantErr: true,
			expired: true,
		},
		{
			name:    "after expiry",
			expires: expiry,
			now:     expiry.Add(time.Nanosecond),
			wantErr: true,
			expired: true,
		},
		{
			name:    "missing expiry",
			now:     expiry,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateQuoteExpiry(
				SwapQuoteResult{ExpiresAt: tt.expires},
				tt.now,
			)
			if (err != nil) != tt.wantErr {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.expired && !errors.Is(err, ErrQuoteExpired) {
				t.Fatalf("want ErrQuoteExpired, got %v", err)
			}
		})
	}
}

func TestLFJQuoteHonorsCancellation(t *testing.T) {
	reader, client := newQuoteTestReader(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := reader.QuoteExactInput(ctx, quoteTestRequest())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
	if client.contractCalls != 0 {
		t.Fatal("cancelled quote must not reach contract calls")
	}
}

func TestLFJQuoteRejectsZeroMinimumOutput(t *testing.T) {
	reader, client := newQuoteTestReader(t)
	client.output = big.NewInt(1)

	_, err := reader.QuoteExactInput(
		context.Background(), quoteTestRequest(),
	)
	if err == nil {
		t.Fatal("expected rejection when minimum output rounds to zero")
	}
}

func TestNewLFJQuoteReaderRejectsNilClient(t *testing.T) {
	if _, err := NewLFJQuoteReader(nil); err == nil {
		t.Fatal("expected nil client to be rejected")
	}
}
