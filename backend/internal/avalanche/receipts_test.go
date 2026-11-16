package avalanche

import (
	"context"
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

type receiptRPCStub struct {
	tx           *types.Transaction
	pending      bool
	receipt      *types.Receipt
	receiptErr   error
	receiptCalls int
}

func (s *receiptRPCStub) ChainID(
	ctx context.Context,
) (*big.Int, error) {
	return big.NewInt(FujiChainID), nil
}

func (s *receiptRPCStub) TransactionByHash(
	ctx context.Context,
	hash common.Hash,
) (*types.Transaction, bool, error) {
	return s.tx, s.pending, nil
}

func (s *receiptRPCStub) TransactionReceipt(
	ctx context.Context,
	hash common.Hash,
) (*types.Receipt, error) {
	s.receiptCalls++
	return s.receipt, s.receiptErr
}

func receiptTestTransaction(t *testing.T) (*types.Transaction, string) {
	t.Helper()

	// Generated only for this offline test; no funded wallet is used.
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}

	to := common.HexToAddress(
		"0x0000000000000000000000000000000000000010",
	)
	tx := types.NewTx(&types.DynamicFeeTx{
		ChainID:   big.NewInt(FujiChainID),
		Nonce:     1,
		GasTipCap: big.NewInt(1),
		GasFeeCap: big.NewInt(2),
		Gas:       50_000,
		To:        &to,
		Value:     big.NewInt(123),
		Data:      []byte{0x12, 0x34},
	})

	signed, err := types.SignTx(
		tx,
		types.LatestSignerForChainID(big.NewInt(FujiChainID)),
		key,
	)
	if err != nil {
		t.Fatal(err)
	}

	return signed, crypto.PubkeyToAddress(key.PublicKey).Hex()
}

func TestReceiptReaderStatuses(t *testing.T) {
	tests := []struct {
		name       string
		pending    bool
		mined      bool
		status     uint64
		receiptErr error
		wantStatus TransactionStatus
		wantErr    error
		wantAnyErr bool
	}{
		{
			name:       "pending without receipt",
			pending:    true,
			receiptErr: ethereum.NotFound,
			wantStatus: TransactionStatusPending,
		},
		{
			name:       "successful receipt",
			mined:      true,
			status:     types.ReceiptStatusSuccessful,
			wantStatus: TransactionStatusSucceeded,
		},
		{
			name:       "failed receipt",
			mined:      true,
			status:     types.ReceiptStatusFailed,
			wantStatus: TransactionStatusFailed,
		},
		{
			name:       "timeout must remain an error",
			pending:    true,
			receiptErr: context.DeadlineExceeded,
			wantErr:    context.DeadlineExceeded,
		},
		{
			name:       "mined transaction with missing receipt",
			receiptErr: ethereum.NotFound,
			wantErr:    ethereum.NotFound,
		},
		{
			name:       "invalid receipt status",
			mined:      true,
			status:     2,
			wantAnyErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx, owner := receiptTestTransaction(t)
			client := &receiptRPCStub{
				tx:         tx,
				pending:    tt.pending,
				receiptErr: tt.receiptErr,
			}

			if tt.mined {
				client.receipt = &types.Receipt{
					TxHash:      tx.Hash(),
					BlockNumber: big.NewInt(12345),
					BlockHash:   common.HexToHash("0x1234"),
					Status:      tt.status,
				}
			}

			reader, err := NewRPCReceiptReader(client)
			if err != nil {
				t.Fatal(err)
			}

			result, err := reader.ReadReceipt(
				context.Background(),
				tx.Hash().Hex(),
			)

			if tt.wantErr != nil || tt.wantAnyErr {
				if err == nil {
					t.Fatal("expected an error")
				}
				if tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
					t.Fatalf("want %v, got %v", tt.wantErr, err)
				}
				if result.Status != "" {
					t.Fatalf("error must not produce a status: %s", result.Status)
				}
				return
			}

			if err != nil {
				t.Fatal(err)
			}
			if result.Status != tt.wantStatus {
				t.Fatalf("status=%s; want %s", result.Status, tt.wantStatus)
			}
			if result.TransactionHash != tx.Hash().Hex() ||
				result.FromAddress != owner ||
				result.ToAddress == nil ||
				*result.ToAddress != tx.To().Hex() ||
				result.Input != "0x1234" ||
				result.ValueRaw != "123" ||
				result.ChainID != 43113 {
				t.Fatalf("unexpected transaction DTO: %+v", result)
			}

			if result.Logs == nil {
				t.Fatal("logs must be an empty array, not nil")
			}

			if tt.mined {
				if result.BlockNumber == nil ||
					*result.BlockNumber != 12345 ||
					result.BlockHash == nil ||
					*result.BlockHash != client.receipt.BlockHash.Hex() {
					t.Fatal("missing mined block information")
				}
				if result.Provenance.BlockNumber == nil ||
					*result.Provenance.BlockNumber != 12345 {
					t.Fatal("missing mined block provenance")
				}
			} else if result.BlockNumber != nil ||
				result.BlockHash != nil ||
				result.Provenance.BlockNumber != nil {
				t.Fatal("pending transaction must not invent a block")
			}

			p := result.Provenance
			if p.Source != "Avalanche RPC" ||
				p.SourceType != "rpc" ||
				p.ChainID != 43113 ||
				p.Mode != "live" ||
				p.IsDemo ||
				p.ObservedAt.IsZero() {
				t.Fatalf("unexpected provenance: %+v", p)
			}
			if client.receiptCalls != 1 {
				t.Fatalf("receipt calls=%d; want 1", client.receiptCalls)
			}
		})
	}
}

func TestReceiptReaderPreservesLogs(t *testing.T) {
	tx, _ := receiptTestTransaction(t)

	blockHash := common.HexToHash("0x1234")
	address := common.HexToAddress(
		"0x0000000000000000000000000000000000000020",
	)
	topic := common.HexToHash("0xabcd")

	client := &receiptRPCStub{
		tx: tx,
		receipt: &types.Receipt{
			TxHash:           tx.Hash(),
			BlockNumber:      big.NewInt(12345),
			BlockHash:        blockHash,
			Status:           types.ReceiptStatusSuccessful,
			TransactionIndex: 2,
			Logs: []*types.Log{
				{
					Address:     address,
					Topics:      []common.Hash{topic},
					Data:        []byte{0x00, 0xff},
					BlockNumber: 12345,
					TxHash:      tx.Hash(),
					TxIndex:     2,
					BlockHash:   blockHash,
					Index:       4,
				},
			},
		},
	}

	reader, err := NewRPCReceiptReader(client)
	if err != nil {
		t.Fatal(err)
	}

	result, err := reader.ReadReceipt(
		context.Background(), tx.Hash().Hex(),
	)
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Logs) != 1 {
		t.Fatalf("logs=%d; want 1", len(result.Logs))
	}

	log := result.Logs[0]
	if log.Address != address.Hex() ||
		len(log.Topics) != 1 ||
		log.Topics[0] != topic.Hex() ||
		log.Data != "0x00ff" ||
		log.BlockNumber != 12345 ||
		log.TransactionHash != tx.Hash().Hex() ||
		log.TransactionIndex != 2 ||
		log.LogIndex != 4 ||
		log.Removed {
		t.Fatalf("log data was changed: %+v", log)
	}
}

func TestReceiptReaderContractCreation(t *testing.T) {
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}

	tx, err := types.SignTx(
		types.NewTx(&types.DynamicFeeTx{
			ChainID:   big.NewInt(FujiChainID),
			GasTipCap: big.NewInt(1),
			GasFeeCap: big.NewInt(2),
			Gas:       100_000,
			// To is nil for contract creation.
			Value: big.NewInt(0),
			Data:  []byte{0x60, 0x00},
		}),
		types.LatestSignerForChainID(big.NewInt(FujiChainID)),
		key,
	)
	if err != nil {
		t.Fatal(err)
	}

	client := &receiptRPCStub{
		tx: tx,
		receipt: &types.Receipt{
			TxHash:      tx.Hash(),
			BlockNumber: big.NewInt(12345),
			BlockHash:   common.HexToHash("0x1234"),
			Status:      types.ReceiptStatusSuccessful,
		},
	}

	reader, err := NewRPCReceiptReader(client)
	if err != nil {
		t.Fatal(err)
	}

	result, err := reader.ReadReceipt(
		context.Background(), tx.Hash().Hex(),
	)
	if err != nil {
		t.Fatal(err)
	}

	if result.ToAddress != nil {
		t.Fatal("contract creation must have a nil to_address")
	}
	if result.Input != "0x6000" ||
		result.ValueRaw != "0" ||
		result.Status != TransactionStatusSucceeded {
		t.Fatalf("unexpected contract creation DTO: %+v", result)
	}
}

func TestReceiptReaderRejectsInconsistentReceipt(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*types.Receipt)
	}{
		{
			name: "wrong transaction hash",
			mutate: func(r *types.Receipt) {
				r.TxHash = common.HexToHash("0x9999")
			},
		},
		{
			name: "missing block number",
			mutate: func(r *types.Receipt) {
				r.BlockNumber = nil
			},
		},
		{
			name: "missing block hash",
			mutate: func(r *types.Receipt) {
				r.BlockHash = common.Hash{}
			},
		},
		{
			name: "nil log",
			mutate: func(r *types.Receipt) {
				r.Logs = []*types.Log{nil}
			},
		},
		{
			name: "log from another transaction",
			mutate: func(r *types.Receipt) {
				r.Logs[0].TxHash = common.HexToHash("0x9999")
			},
		},
		{
			name: "log from another block",
			mutate: func(r *types.Receipt) {
				r.Logs[0].BlockHash = common.HexToHash("0x9999")
			},
		},
		{
			name: "wrong log block number",
			mutate: func(r *types.Receipt) {
				r.Logs[0].BlockNumber++
			},
		},
		{
			name: "wrong log transaction index",
			mutate: func(r *types.Receipt) {
				r.Logs[0].TxIndex++
			},
		},
		{
			name: "removed log",
			mutate: func(r *types.Receipt) {
				r.Logs[0].Removed = true
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx, _ := receiptTestTransaction(t)
			blockHash := common.HexToHash("0x1234")

			receipt := &types.Receipt{
				TxHash:           tx.Hash(),
				BlockNumber:      big.NewInt(12345),
				BlockHash:        blockHash,
				Status:           types.ReceiptStatusSuccessful,
				TransactionIndex: 2,
				Logs: []*types.Log{
					{
						BlockNumber: 12345,
						BlockHash:   blockHash,
						TxHash:      tx.Hash(),
						TxIndex:     2,
					},
				},
			}
			tt.mutate(receipt)

			reader, err := NewRPCReceiptReader(&receiptRPCStub{
				tx:      tx,
				receipt: receipt,
			})
			if err != nil {
				t.Fatal(err)
			}

			result, err := reader.ReadReceipt(
				context.Background(), tx.Hash().Hex(),
			)
			if err == nil {
				t.Fatal("expected inconsistent receipt to be rejected")
			}
			if result.Status != "" {
				t.Fatal("invalid receipt must not produce a transaction status")
			}
		})
	}
}

func TestReceiptReaderHonorsCancellation(t *testing.T) {
	client := &receiptRPCStub{}
	reader, err := NewRPCReceiptReader(client)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := reader.ReadReceipt(
		ctx,
		common.HexToHash("0x1234").Hex(),
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
	if result.Status != "" || client.receiptCalls != 0 {
		t.Fatal("cancelled request must not return a status or read receipt")
	}
}

func validReceiptSwapIntent(t *testing.T) ExpectedSwapIntent {
	t.Helper()

	builder, err := NewSwapTxBuilder()
	if err != nil {
		t.Fatal(err)
	}

	intent := ExpectedSwapIntent{
		Tx: UnsignedTx{
			ChainID: uint64(FujiChainID),
			From:    "0x0000000000000000000000000000000000000001",
			To:      fujiExecutionManager,
			Value:   "0",
		},
		AdapterAddress:  "0xD5cae0dF0f11a0Cf9818aCc47b11ce183b0E8AF5",
		TokenInAddress:  lfjFujiTokenX,
		TokenOutAddress: lfjFujiTokenY,
		AmountInRaw:     "1000000",
		MinAmountOutRaw: "990000",
		// Fixed historical deadline: receipt verification must not
		// reject a mined transaction based on today's wall clock.
		Deadline: 1_700_000_000,
	}

	data, err := builder.abi.Pack(
		"executeSwap",
		common.HexToAddress(intent.TokenInAddress),
		common.HexToAddress(intent.TokenOutAddress),
		big.NewInt(1_000_000),
		big.NewInt(990_000),
		new(big.Int).SetUint64(intent.Deadline),
	)
	if err != nil {
		t.Fatal(err)
	}

	intent.Tx.Data = hexutil.Encode(data)
	return intent
}

func TestValidateExpectedSwapIntent(t *testing.T) {
	t.Run("accepts consistent intent with historical deadline", func(t *testing.T) {
		intent := validReceiptSwapIntent(t)

		data, err := validateExpectedSwapIntent(intent)
		if err != nil {
			t.Fatal(err)
		}
		if hexutil.Encode(data) != intent.Tx.Data {
			t.Fatal("validated calldata differs from expected calldata")
		}
	})

	tests := []struct {
		name   string
		mutate func(*ExpectedSwapIntent)
	}{
		{"wrong chain", func(i *ExpectedSwapIntent) {
			i.Tx.ChainID = 43114
		}},
		{"invalid owner", func(i *ExpectedSwapIntent) {
			i.Tx.From = "invalid"
		}},
		{"zero owner", func(i *ExpectedSwapIntent) {
			i.Tx.From = (common.Address{}).Hex()
		}},
		{"manager as owner", func(i *ExpectedSwapIntent) {
			i.Tx.From = fujiExecutionManager
		}},
		{"wrong manager", func(i *ExpectedSwapIntent) {
			i.Tx.To = i.Tx.From
		}},
		{"wrong adapter", func(i *ExpectedSwapIntent) {
			i.AdapterAddress = i.Tx.From
		}},
		{"same input and output", func(i *ExpectedSwapIntent) {
			i.TokenOutAddress = i.TokenInAddress
		}},
		{"nonzero native value", func(i *ExpectedSwapIntent) {
			i.Tx.Value = "1"
		}},
		{"zero input", func(i *ExpectedSwapIntent) {
			i.AmountInRaw = "0"
		}},
		{"changed input", func(i *ExpectedSwapIntent) {
			i.AmountInRaw = "1000001"
		}},
		{"changed minimum output", func(i *ExpectedSwapIntent) {
			i.MinAmountOutRaw = "990001"
		}},
		{"changed deadline", func(i *ExpectedSwapIntent) {
			i.Deadline++
		}},
		{"zero deadline", func(i *ExpectedSwapIntent) {
			i.Deadline = 0
		}},
		{"malformed calldata", func(i *ExpectedSwapIntent) {
			i.Tx.Data = "0xzz"
		}},
		{"trailing calldata", func(i *ExpectedSwapIntent) {
			i.Tx.Data += "00"
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			intent := validReceiptSwapIntent(t)
			tt.mutate(&intent)

			_, err := validateExpectedSwapIntent(intent)
			if !errors.Is(err, ErrInvalidSwapIntent) {
				t.Fatalf("want ErrInvalidSwapIntent, got %v", err)
			}
		})
	}
}

func TestMatchSwapTransaction(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*ReceiptResult)
		wantErr bool
	}{
		{
			name: "matching transaction",
		},
		{
			name: "wrong chain",
			mutate: func(r *ReceiptResult) {
				r.ChainID = 43114
			},
			wantErr: true,
		},
		{
			name: "different sender",
			mutate: func(r *ReceiptResult) {
				r.FromAddress =
					"0x0000000000000000000000000000000000000002"
			},
			wantErr: true,
		},
		{
			name: "malformed sender",
			mutate: func(r *ReceiptResult) {
				r.FromAddress = "invalid"
			},
			wantErr: true,
		},
		{
			name: "different destination",
			mutate: func(r *ReceiptResult) {
				address :=
					"0x0000000000000000000000000000000000000002"
				r.ToAddress = &address
			},
			wantErr: true,
		},
		{
			name: "contract creation",
			mutate: func(r *ReceiptResult) {
				r.ToAddress = nil
			},
			wantErr: true,
		},
		{
			name: "nonzero native value",
			mutate: func(r *ReceiptResult) {
				r.ValueRaw = "1"
			},
			wantErr: true,
		},
		{
			name: "malformed native value",
			mutate: func(r *ReceiptResult) {
				r.ValueRaw = "invalid"
			},
			wantErr: true,
		},
		{
			name: "different function selector",
			mutate: func(r *ReceiptResult) {
				r.Input = "0x00000000" + r.Input[10:]
			},
			wantErr: true,
		},
		{
			name: "changed deadline in calldata",
			mutate: func(r *ReceiptResult) {
				data, err := hexutil.Decode(r.Input)
				if err != nil {
					t.Fatal(err)
				}
				data[len(data)-1] ^= 1
				r.Input = hexutil.Encode(data)
			},
			wantErr: true,
		},
		{
			name: "extra calldata",
			mutate: func(r *ReceiptResult) {
				r.Input += "00"
			},
			wantErr: true,
		},
		{
			name: "malformed calldata",
			mutate: func(r *ReceiptResult) {
				r.Input = "0xzz"
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			intent := validReceiptSwapIntent(t)
			to := intent.Tx.To

			actual := ReceiptResult{
				ChainID:     intent.Tx.ChainID,
				FromAddress: intent.Tx.From,
				ToAddress:   &to,
				ValueRaw:    intent.Tx.Value,
				Input:       intent.Tx.Data,
			}

			if tt.mutate != nil {
				tt.mutate(&actual)
			}

			err := matchSwapTransaction(intent, actual)
			if tt.wantErr {
				if !errors.Is(err, ErrTransactionIntentMismatch) {
					t.Fatalf(
						"want ErrTransactionIntentMismatch, got %v",
						err,
					)
				}
				return
			}

			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func swapReceiptFixture(
	t *testing.T,
) (ExpectedSwapIntent, *receiptRPCStub) {
	t.Helper()

	intent := validReceiptSwapIntent(t)

	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	intent.Tx.From = crypto.PubkeyToAddress(key.PublicKey).Hex()

	data, err := hexutil.Decode(intent.Tx.Data)
	if err != nil {
		t.Fatal(err)
	}
	to := common.HexToAddress(intent.Tx.To)

	unsigned := types.NewTx(&types.DynamicFeeTx{
		ChainID:   big.NewInt(FujiChainID),
		Nonce:     1,
		GasTipCap: big.NewInt(1),
		GasFeeCap: big.NewInt(2),
		Gas:       500_000,
		To:        &to,
		Value:     big.NewInt(0),
		Data:      data,
	})

	tx, err := types.SignTx(
		unsigned,
		types.LatestSignerForChainID(big.NewInt(FujiChainID)),
		key,
	)
	if err != nil {
		t.Fatal(err)
	}

	builder, err := NewSwapTxBuilder()
	if err != nil {
		t.Fatal(err)
	}
	event := builder.abi.Events["SwapExecuted"]

	eventData, err := event.Inputs.NonIndexed().Pack(
		common.HexToAddress(intent.AdapterAddress),
		big.NewInt(1_000_000),
		big.NewInt(999_900),
	)
	if err != nil {
		t.Fatal(err)
	}

	blockHash := common.HexToHash("0x1234")
	receipt := &types.Receipt{
		TxHash:      tx.Hash(),
		BlockNumber: big.NewInt(12345),
		BlockHash:   blockHash,
		Status:      types.ReceiptStatusSuccessful,
		Logs: []*types.Log{
			{
				Address: to,
				Topics: []common.Hash{
					event.ID,
					common.BytesToHash(
						common.HexToAddress(intent.Tx.From).Bytes(),
					),
					common.BytesToHash(
						common.HexToAddress(intent.TokenInAddress).Bytes(),
					),
					common.BytesToHash(
						common.HexToAddress(intent.TokenOutAddress).Bytes(),
					),
				},
				Data:        eventData,
				BlockNumber: 12345,
				TxHash:      tx.Hash(),
				BlockHash:   blockHash,
			},
		},
	}

	return intent, &receiptRPCStub{
		tx:      tx,
		receipt: receipt,
	}
}

func TestVerifyTx(t *testing.T) {
	tests := []struct {
		name         string
		mutate       func(*ExpectedSwapIntent, *receiptRPCStub)
		wantStatus   TransactionStatus
		wantVerified bool
		wantErr      error
	}{
		{
			name:         "successful matching swap",
			wantStatus:   TransactionStatusSucceeded,
			wantVerified: true,
		},
		{
			name: "pending is not verified",
			mutate: func(_ *ExpectedSwapIntent, s *receiptRPCStub) {
				s.pending = true
				s.receipt = nil
				s.receiptErr = ethereum.NotFound
			},
			wantStatus: TransactionStatusPending,
		},
		{
			name: "reverted is not verified",
			mutate: func(_ *ExpectedSwapIntent, s *receiptRPCStub) {
				s.receipt.Status = types.ReceiptStatusFailed
				s.receipt.Logs = nil
			},
			wantStatus: TransactionStatusFailed,
		},
		{
			name: "different confirmed owner",
			mutate: func(i *ExpectedSwapIntent, _ *receiptRPCStub) {
				i.Tx.From =
					"0x0000000000000000000000000000000000000002"
			},
			wantErr: ErrTransactionIntentMismatch,
		},
		{
			name: "missing swap event",
			mutate: func(_ *ExpectedSwapIntent, s *receiptRPCStub) {
				s.receipt.Logs = nil
			},
			wantErr: ErrSwapEventMismatch,
		},
		{
			name: "event emitted by another contract",
			mutate: func(_ *ExpectedSwapIntent, s *receiptRPCStub) {
				s.receipt.Logs[0].Address =
					common.HexToAddress(lfjFujiRouter)
			},
			wantErr: ErrSwapEventMismatch,
		},
		{
			name: "wrong event owner",
			mutate: func(_ *ExpectedSwapIntent, s *receiptRPCStub) {
				s.receipt.Logs[0].Topics[1] = common.HexToHash("0x02")
			},
			wantErr: ErrSwapEventMismatch,
		},
		{
			name: "wrong event adapter",
			mutate: func(_ *ExpectedSwapIntent, s *receiptRPCStub) {
				// First ABI word contains the adapter address.
				s.receipt.Logs[0].Data[31] ^= 1
			},
			wantErr: ErrSwapEventMismatch,
		},
		{
			name: "wrong event input amount",
			mutate: func(_ *ExpectedSwapIntent, s *receiptRPCStub) {
				// Second ABI word contains amountIn.
				s.receipt.Logs[0].Data[63] ^= 1
			},
			wantErr: ErrSwapEventMismatch,
		},
		{
			name: "output below confirmed minimum",
			mutate: func(_ *ExpectedSwapIntent, s *receiptRPCStub) {
				// Third ABI word contains amountOut.
				big.NewInt(989_999).FillBytes(
					s.receipt.Logs[0].Data[64:96],
				)
			},
			wantErr: ErrSwapEventMismatch,
		},
		{
			name: "output exactly at confirmed minimum",
			mutate: func(_ *ExpectedSwapIntent, s *receiptRPCStub) {
				big.NewInt(990_000).FillBytes(
					s.receipt.Logs[0].Data[64:96],
				)
			},
			wantStatus:   TransactionStatusSucceeded,
			wantVerified: true,
		},
		{
			name: "duplicate swap events",
			mutate: func(_ *ExpectedSwapIntent, s *receiptRPCStub) {
				duplicate := *s.receipt.Logs[0]
				duplicate.Index = 1
				s.receipt.Logs = append(s.receipt.Logs, &duplicate)
			},
			wantErr: ErrSwapEventMismatch,
		},
		{
			name: "truncated event data",
			mutate: func(_ *ExpectedSwapIntent, s *receiptRPCStub) {
				s.receipt.Logs[0].Data =
					s.receipt.Logs[0].Data[:95]
			},
			wantErr: ErrSwapEventMismatch,
		},
		{
			name: "RPC timeout stays an error",
			mutate: func(_ *ExpectedSwapIntent, s *receiptRPCStub) {
				s.receiptErr = context.DeadlineExceeded
			},
			wantErr: context.DeadlineExceeded,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			intent, client := swapReceiptFixture(t)
			if tt.mutate != nil {
				tt.mutate(&intent, client)
			}

			reader, err := NewRPCReceiptReader(client)
			if err != nil {
				t.Fatal(err)
			}

			result, err := reader.VerifyTx(
				context.Background(),
				client.tx.Hash().Hex(),
				intent,
			)

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("want %v, got %v", tt.wantErr, err)
				}
				if result.Verified || result.AmountOutRaw != nil {
					t.Fatal("error must not produce verified output")
				}
				return
			}

			if err != nil {
				t.Fatal(err)
			}
			if result.Receipt.Status != tt.wantStatus {
				t.Fatalf(
					"status = %s, want %s",
					result.Receipt.Status,
					tt.wantStatus,
				)
			}
			if result.Verified != tt.wantVerified {
				t.Fatalf("verified = %v", result.Verified)
			}

			if tt.wantVerified {
				wantAmountOut := "999900"
				if tt.name == "output exactly at confirmed minimum" {
					wantAmountOut = "990000"
				}

				if result.AmountOutRaw == nil ||
					*result.AmountOutRaw != wantAmountOut {
					t.Fatal("incorrect verified output amount")
				}
			} else if result.AmountOutRaw != nil {
				t.Fatal("unverified transaction must not expose verified output")
			}
		})
	}
}

func TestVerifyTxRejectsDifferentHash(t *testing.T) {
	intent, client := swapReceiptFixture(t)

	reader, err := NewRPCReceiptReader(client)
	if err != nil {
		t.Fatal(err)
	}

	// Valid hash format, but it is not the transaction returned by RPC.
	differentHash := common.HexToHash("0x9876").Hex()

	result, err := reader.VerifyTx(
		context.Background(),
		differentHash,
		intent,
	)
	if err == nil {
		t.Fatal("expected mismatched transaction hash to be rejected")
	}
	if result.Verified || result.AmountOutRaw != nil {
		t.Fatal("mismatched hash must not produce verified output")
	}
	if client.receiptCalls != 0 {
		t.Fatal("must reject transaction identity before reading receipt")
	}
}

func TestWaitForVerifiedSwapPendingTimeout(t *testing.T) {
	intent, client := swapReceiptFixture(t)
	client.pending = true
	client.receipt = nil
	client.receiptErr = ethereum.NotFound

	reader, err := NewRPCReceiptReader(client)
	if err != nil {
		t.Fatal(err)
	}

	// Shorter than the first polling delay; no real 30-second wait.
	ctx, cancel := context.WithTimeout(
		context.Background(),
		50*time.Millisecond,
	)
	defer cancel()

	result, err := reader.WaitForVerifiedSwap(
		ctx,
		client.tx.Hash().Hex(),
		intent,
	)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want deadline exceeded, got %v", err)
	}
	if result.Verified || result.AmountOutRaw != nil {
		t.Fatal("timeout must not produce verified output")
	}

	// A heavily delayed test runner may expire before the first RPC.
	if result.Receipt.TransactionHash != "" &&
		result.Receipt.Status != TransactionStatusPending {
		t.Fatalf("unexpected status after timeout: %s", result.Receipt.Status)
	}
	if client.receiptCalls > 1 {
		t.Fatalf(
			"unexpected polling before first delay: %d calls",
			client.receiptCalls,
		)
	}
}

func TestWaitForVerifiedSwapCancelled(t *testing.T) {
	intent, client := swapReceiptFixture(t)

	reader, err := NewRPCReceiptReader(client)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := reader.WaitForVerifiedSwap(
		ctx,
		client.tx.Hash().Hex(),
		intent,
	)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want context canceled, got %v", err)
	}
	if result.Verified {
		t.Fatal("cancelled verification must not succeed")
	}
	if client.receiptCalls != 0 {
		t.Fatal("cancelled context must not trigger receipt queries")
	}
}

func TestWaitForVerifiedSwapStopsWhenMined(t *testing.T) {
	for _, reverted := range []bool{false, true} {
		name := "success"
		if reverted {
			name = "reverted"
		}

		t.Run(name, func(t *testing.T) {
			intent, client := swapReceiptFixture(t)
			if reverted {
				client.receipt.Status = types.ReceiptStatusFailed
				client.receipt.Logs = nil
			}

			reader, err := NewRPCReceiptReader(client)
			if err != nil {
				t.Fatal(err)
			}

			result, err := reader.WaitForVerifiedSwap(
				context.Background(),
				client.tx.Hash().Hex(),
				intent,
			)
			if err != nil {
				t.Fatal(err)
			}
			if client.receiptCalls != 1 {
				t.Fatalf(
					"mined transaction queried %d times",
					client.receiptCalls,
				)
			}

			if reverted {
				if result.Receipt.Status != TransactionStatusFailed ||
					result.Verified ||
					result.AmountOutRaw != nil {
					t.Fatal("incorrect reverted result")
				}
			} else if !result.Verified ||
				result.AmountOutRaw == nil ||
				*result.AmountOutRaw != "999900" {
				t.Fatal("incorrect successful verification")
			}
		})
	}
}
