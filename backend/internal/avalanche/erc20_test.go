package avalanche

import (
	"bytes"
	"context"
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
)

// Only CallContract is used by these tests.
// The other RPCReader methods must not be called.
type mockContractRPC struct {
	RPCReader
	call func(
		context.Context,
		ethereum.CallMsg,
		*big.Int,
	) ([]byte, error)
}

func (m *mockContractRPC) CallContract(
	ctx context.Context,
	msg ethereum.CallMsg,
	blockNumber *big.Int,
) ([]byte, error) {
	return m.call(ctx, msg, blockNumber)
}

func TestGetERC20Balance(t *testing.T) {
	const tokenAddress = "0x1111111111111111111111111111111111111111"
	const ownerAddress = "0x2222222222222222222222222222222222222222"

	t.Run("reads balance at requested block", func(t *testing.T) {
		block := big.NewInt(123)
		want := big.NewInt(1250000)
		called := false

		client := &mockContractRPC{
			call: func(
				ctx context.Context,
				msg ethereum.CallMsg,
				blockNumber *big.Int,
			) ([]byte, error) {
				called = true

				if msg.To == nil ||
					*msg.To != common.HexToAddress(tokenAddress) {
					t.Fatal("incorrect token address")
				}

				if blockNumber == nil || blockNumber.Cmp(block) != 0 {
					t.Fatal("incorrect block number")
				}

				// balanceOf(address): selector followed by padded owner.
				expectedData := append(
					common.FromHex("0x70a08231"),
					common.LeftPadBytes(
						common.HexToAddress(ownerAddress).Bytes(),
						32,
					)...,
				)

				if !bytes.Equal(msg.Data, expectedData) {
					t.Fatalf("incorrect calldata: %x", msg.Data)
				}

				return common.LeftPadBytes(want.Bytes(), 32), nil
			},
		}

		got, err := GetERC20Balance(
			context.Background(),
			client,
			tokenAddress,
			ownerAddress,
			block,
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !called {
			t.Fatal("expected an RPC call")
		}

		if got == nil || got.Cmp(want) != 0 {
			t.Fatalf("balance = %v, want %s", got, want)
		}
	})

	t.Run("rejects invalid address before RPC", func(t *testing.T) {
		client := &mockContractRPC{
			call: func(
				context.Context,
				ethereum.CallMsg,
				*big.Int,
			) ([]byte, error) {
				t.Fatal("RPC must not be called for an invalid address")
				return nil, nil
			},
		}

		for _, addresses := range [][2]string{
			{"invalid", ownerAddress},
			{tokenAddress, "invalid"},
		} {
			_, err := GetERC20Balance(
				context.Background(),
				client,
				addresses[0],
				addresses[1],
				nil,
			)
			if !errors.Is(err, ErrInvalidAddress) {
				t.Fatalf("expected ErrInvalidAddress, got %v", err)
			}
		}
	})

	t.Run("preserves RPC error", func(t *testing.T) {
		rpcErr := errors.New("RPC unavailable")

		client := &mockContractRPC{
			call: func(
				context.Context,
				ethereum.CallMsg,
				*big.Int,
			) ([]byte, error) {
				return nil, rpcErr
			},
		}

		_, err := GetERC20Balance(
			context.Background(),
			client,
			tokenAddress,
			ownerAddress,
			nil,
		)

		if !errors.Is(err, rpcErr) {
			t.Fatalf("expected original RPC error, got %v", err)
		}
	})

	t.Run("rejects empty response", func(t *testing.T) {
		client := &mockContractRPC{
			call: func(
				context.Context,
				ethereum.CallMsg,
				*big.Int,
			) ([]byte, error) {
				return nil, nil
			},
		}

		_, err := GetERC20Balance(
			context.Background(),
			client,
			tokenAddress,
			ownerAddress,
			nil,
		)

		if err == nil {
			t.Fatal("expected an invalid response error")
		}
	})
}

func TestGetERC20Allowance(t *testing.T) {
	const tokenAddress = "0x1111111111111111111111111111111111111111"
	const ownerAddress = "0x2222222222222222222222222222222222222222"
	const spenderAddress = "0x3333333333333333333333333333333333333333"

	maxUint256 := new(big.Int).Sub(
		new(big.Int).Lsh(big.NewInt(1), 256),
		big.NewInt(1),
	)

	tests := []struct {
		name   string
		amount *big.Int
	}{
		{name: "normal allowance", amount: big.NewInt(1250000)},
		{name: "zero allowance", amount: big.NewInt(0)},
		{name: "maximum allowance", amount: maxUint256},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			block := big.NewInt(123)
			called := false

			client := &mockContractRPC{
				call: func(
					ctx context.Context,
					msg ethereum.CallMsg,
					blockNumber *big.Int,
				) ([]byte, error) {
					called = true

					if msg.To == nil ||
						*msg.To != common.HexToAddress(tokenAddress) {
						t.Fatal("incorrect token address")
					}

					if blockNumber == nil ||
						blockNumber.Cmp(block) != 0 {
						t.Fatal("incorrect block number")
					}

					// allowance(address,address):
					// selector, padded owner, padded spender.
					expectedData := common.FromHex("0xdd62ed3e")
					expectedData = append(
						expectedData,
						common.LeftPadBytes(
							common.HexToAddress(ownerAddress).Bytes(),
							32,
						)...,
					)
					expectedData = append(
						expectedData,
						common.LeftPadBytes(
							common.HexToAddress(spenderAddress).Bytes(),
							32,
						)...,
					)

					if !bytes.Equal(msg.Data, expectedData) {
						t.Fatalf("incorrect calldata: %x", msg.Data)
					}

					return common.LeftPadBytes(
						tt.amount.Bytes(),
						32,
					), nil
				},
			}

			got, err := GetERC20Allowance(
				context.Background(),
				client,
				tokenAddress,
				ownerAddress,
				spenderAddress,
				block,
			)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if !called {
				t.Fatal("expected an RPC call")
			}

			if got == nil || got.Cmp(tt.amount) != 0 {
				t.Fatalf("allowance = %v, want %s", got, tt.amount)
			}
		})
	}

	t.Run("rejects invalid addresses before RPC", func(t *testing.T) {
		client := &mockContractRPC{
			call: func(
				context.Context,
				ethereum.CallMsg,
				*big.Int,
			) ([]byte, error) {
				t.Fatal("RPC must not be called for an invalid address")
				return nil, nil
			},
		}

		for _, addresses := range [][3]string{
			{"invalid", ownerAddress, spenderAddress},
			{tokenAddress, "invalid", spenderAddress},
			{tokenAddress, ownerAddress, "invalid"},
		} {
			_, err := GetERC20Allowance(
				context.Background(),
				client,
				addresses[0],
				addresses[1],
				addresses[2],
				nil,
			)
			if !errors.Is(err, ErrInvalidAddress) {
				t.Fatalf("expected ErrInvalidAddress, got %v", err)
			}
		}
	})

	t.Run("preserves RPC error", func(t *testing.T) {
		rpcErr := errors.New("RPC unavailable")

		client := &mockContractRPC{
			call: func(
				context.Context,
				ethereum.CallMsg,
				*big.Int,
			) ([]byte, error) {
				return nil, rpcErr
			},
		}

		_, err := GetERC20Allowance(
			context.Background(),
			client,
			tokenAddress,
			ownerAddress,
			spenderAddress,
			nil,
		)

		if !errors.Is(err, rpcErr) {
			t.Fatalf("expected original RPC error, got %v", err)
		}
	})

	t.Run("rejects empty response", func(t *testing.T) {
		client := &mockContractRPC{
			call: func(
				context.Context,
				ethereum.CallMsg,
				*big.Int,
			) ([]byte, error) {
				return nil, nil
			},
		}

		_, err := GetERC20Allowance(
			context.Background(),
			client,
			tokenAddress,
			ownerAddress,
			spenderAddress,
			nil,
		)

		if err == nil {
			t.Fatal("expected an invalid response error")
		}
	})
}

func TestGetERC20Decimals(t *testing.T) {
	const tokenAddress = "0x1111111111111111111111111111111111111111"

	encodeWord := func(value int64) []byte {
		return common.LeftPadBytes(big.NewInt(value).Bytes(), 32)
	}

	tests := []struct {
		name     string
		response []byte
		want     uint8
		wantErr  bool
	}{
		{
			name:     "zero decimals is valid",
			response: encodeWord(0),
			want:     0,
		},
		{
			name:     "six decimals",
			response: encodeWord(6),
			want:     6,
		},
		{
			name:     "eighteen decimals",
			response: encodeWord(18),
			want:     18,
		},
		{
			name:     "maximum uint8",
			response: encodeWord(255),
			want:     255,
		},
		{
			name:     "empty response",
			response: nil,
			wantErr:  true,
		},
		{
			name:     "short response",
			response: []byte{18},
			wantErr:  true,
		},
		{
			name:     "oversized response",
			response: make([]byte, 64),
			wantErr:  true,
		},
		{
			name:     "value exceeds uint8",
			response: encodeWord(256),
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			block := big.NewInt(123)
			called := false

			client := &mockContractRPC{
				call: func(
					ctx context.Context,
					msg ethereum.CallMsg,
					blockNumber *big.Int,
				) ([]byte, error) {
					called = true

					if msg.To == nil ||
						*msg.To != common.HexToAddress(tokenAddress) {
						t.Fatal("incorrect token address")
					}

					if blockNumber == nil ||
						blockNumber.Cmp(block) != 0 {
						t.Fatal("incorrect block number")
					}

					// decimals() has no arguments.
					if !bytes.Equal(
						msg.Data,
						common.FromHex("0x313ce567"),
					) {
						t.Fatalf("incorrect calldata: %x", msg.Data)
					}

					return tt.response, nil
				},
			}

			got, err := GetERC20Decimals(
				context.Background(),
				client,
				tokenAddress,
				block,
			)

			if !called {
				t.Fatal("expected an RPC call")
			}

			if tt.wantErr {
				if !errors.Is(err, ErrTokenMetadataUnavailable) {
					t.Fatalf("expected metadata error, got %v", err)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if got != tt.want {
				t.Errorf("decimals = %d, want %d", got, tt.want)
			}
		})
	}

	t.Run("preserves RPC error and reports missing metadata", func(t *testing.T) {
		rpcErr := errors.New("RPC unavailable")

		client := &mockContractRPC{
			call: func(
				context.Context,
				ethereum.CallMsg,
				*big.Int,
			) ([]byte, error) {
				return nil, rpcErr
			},
		}

		_, err := GetERC20Decimals(
			context.Background(),
			client,
			tokenAddress,
			nil,
		)

		if !errors.Is(err, ErrTokenMetadataUnavailable) {
			t.Fatalf("expected metadata error, got %v", err)
		}

		if !errors.Is(err, rpcErr) {
			t.Fatalf("expected original RPC error, got %v", err)
		}
	})

	t.Run("rejects invalid token address before RPC", func(t *testing.T) {
		client := &mockContractRPC{
			call: func(
				context.Context,
				ethereum.CallMsg,
				*big.Int,
			) ([]byte, error) {
				t.Fatal("RPC must not be called for an invalid address")
				return nil, nil
			},
		}

		_, err := GetERC20Decimals(
			context.Background(),
			client,
			"invalid",
			nil,
		)

		if err == nil {
			t.Fatal("expected an invalid address error")
		}
	})
}

func TestERC20ContextErrors(t *testing.T) {
	const token = "0x1111111111111111111111111111111111111111"
	const owner = "0x2222222222222222222222222222222222222222"
	const spender = "0x3333333333333333333333333333333333333333"

	operations := []struct {
		name string
		run  func(context.Context, RPCReader) error
	}{
		{
			name: "balance",
			run: func(ctx context.Context, client RPCReader) error {
				_, err := GetERC20Balance(
					ctx, client, token, owner, nil,
				)
				return err
			},
		},
		{
			name: "allowance",
			run: func(ctx context.Context, client RPCReader) error {
				_, err := GetERC20Allowance(
					ctx, client, token, owner, spender, nil,
				)
				return err
			},
		},
		{
			name: "decimals",
			run: func(ctx context.Context, client RPCReader) error {
				_, err := GetERC20Decimals(
					ctx, client, token, nil,
				)
				return err
			},
		},
	}

	contextCases := []struct {
		name string
		make func() (context.Context, context.CancelFunc)
		want error
	}{
		{
			name: "canceled",
			make: func() (context.Context, context.CancelFunc) {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx, cancel
			},
			want: context.Canceled,
		},
		{
			name: "deadline exceeded",
			make: func() (context.Context, context.CancelFunc) {
				return context.WithDeadline(
					context.Background(),
					time.Now().Add(-time.Second),
				)
			},
			want: context.DeadlineExceeded,
		},
	}

	for _, operation := range operations {
		for _, contextCase := range contextCases {
			t.Run(operation.name+"/"+contextCase.name, func(t *testing.T) {
				ctx, cancel := contextCase.make()
				defer cancel()

				called := false

				client := &mockContractRPC{
					call: func(
						gotCtx context.Context,
						msg ethereum.CallMsg,
						blockNumber *big.Int,
					) ([]byte, error) {
						called = true

						if gotCtx != ctx {
							t.Fatal("caller context was not passed to RPC")
						}

						return nil, gotCtx.Err()
					},
				}

				err := operation.run(ctx, client)

				if !called {
					t.Fatal("expected an RPC call")
				}

				if !errors.Is(err, contextCase.want) {
					t.Fatalf(
						"expected %v, got %v",
						contextCase.want,
						err,
					)
				}

				if operation.name == "decimals" &&
					!errors.Is(err, ErrTokenMetadataUnavailable) {
					t.Fatalf("expected metadata error, got %v", err)
				}
			})
		}
	}
}
