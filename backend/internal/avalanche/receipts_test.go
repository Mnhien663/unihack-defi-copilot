package avalanche

import (
	"context"
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
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
