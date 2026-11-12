package avalanche

import (
	"context"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/core/types"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
)

func depositReceiptFixture(
	t *testing.T,
) (ExpectedDepositIntent, ReceiptResult) {
	t.Helper()
	request, now := depositBuilderFixture()
	builder, err := NewDepositTxBuilder()
	if err != nil {
		t.Fatal(err)
	}
	built, err := builder.BuildDepositTx(request, now)
	if err != nil {
		t.Fatal(err)
	}
	intent := ExpectedDepositIntent{
		Tx:             built.Prepared.Tx,
		AdapterAddress: fujiVaultExecutionAdapter,
		VaultAddress:   demoFujiVault,
		AssetAddress:   demoFujiAsset,
		AssetsRaw:      built.Summary.Assets.Raw,
		MinSharesRaw:   built.Summary.MinShares.Raw,
		Deadline:       request.Deadline,
	}

	assets, _ := new(big.Int).SetString(intent.AssetsRaw, 10)
	shares, _ := new(big.Int).SetString("900000000000000000", 10)
	owner := common.HexToAddress(intent.Tx.From)
	adapter := common.HexToAddress(intent.AdapterAddress)

	topic := func(address string) string {
		return common.BytesToHash(
			common.HexToAddress(address).Bytes(),
		).Hex()
	}
	event := builder.abi.Events["DepositExecuted"]
	managerData, err := event.Inputs.NonIndexed().Pack(
		adapter, assets, shares,
	)
	if err != nil {
		t.Fatal(err)
	}
	uintType, err := abi.NewType("uint256", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	pair := abi.Arguments{{Type: uintType}, {Type: uintType}}
	depositData, err := pair.Pack(assets, shares)
	if err != nil {
		t.Fatal(err)
	}
	mintData, err := (abi.Arguments{{Type: uintType}}).Pack(shares)
	if err != nil {
		t.Fatal(err)
	}

	destination := intent.Tx.To
	block := uint64(123)
	receipt := ReceiptResult{
		FromAddress: owner.Hex(),
		ToAddress:   &destination,
		ChainID:     uint64(FujiChainID),
		ValueRaw:    "0",
		Input:       intent.Tx.Data,
		Status:      TransactionStatusSucceeded,
		BlockNumber: &block,
		Logs: []ReceiptLog{
			{
				Address: intent.Tx.To,
				Topics: []string{
					event.ID.Hex(),
					topic(intent.Tx.From),
					topic(intent.AssetAddress),
					topic(intent.VaultAddress),
				},
				Data: hexutil.Encode(managerData),
			},
			{
				Address: intent.VaultAddress,
				Topics: []string{
					crypto.Keccak256Hash(
						[]byte("Deposit(address,address,uint256,uint256)"),
					).Hex(),
					topic(intent.AdapterAddress),
					topic(intent.Tx.From),
				},
				Data: hexutil.Encode(depositData),
			},
			{
				Address: intent.VaultAddress,
				Topics: []string{
					crypto.Keccak256Hash(
						[]byte("Transfer(address,address,uint256)"),
					).Hex(),
					topic((common.Address{}).Hex()),
					topic(intent.Tx.From),
				},
				Data: hexutil.Encode(mintData),
			},
		},
	}
	return intent, receipt
}

func TestDepositReceiptMatching(t *testing.T) {
	intent, receipt := depositReceiptFixture(t)
	if err := matchDepositTransaction(intent, receipt); err != nil {
		t.Fatal(err)
	}
	shares, err := verifyDepositEvents(intent, receipt)
	if err != nil {
		t.Fatal(err)
	}
	if shares != "900000000000000000" {
		t.Fatalf("unexpected shares: %s", shares)
	}
}

func TestDepositReceiptRejectsTransactionMismatch(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*ReceiptResult)
	}{
		{"chain", func(r *ReceiptResult) {
			r.ChainID = 43114
		}},
		{"sender", func(r *ReceiptResult) {
			r.FromAddress = demoFujiAsset
		}},
		{"destination", func(r *ReceiptResult) {
			address := demoFujiVault
			r.ToAddress = &address
		}},
		{"missing destination", func(r *ReceiptResult) {
			r.ToAddress = nil
		}},
		{"native value", func(r *ReceiptResult) {
			r.ValueRaw = "1"
		}},
		{"calldata", func(r *ReceiptResult) {
			r.Input = "0x1234"
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			intent, receipt := depositReceiptFixture(t)
			test.mutate(&receipt)
			if err := matchDepositTransaction(intent, receipt); err == nil {
				t.Fatal("expected transaction mismatch")
			}
		})
	}
}

func TestDepositReceiptRejectsInvalidEvents(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*ReceiptResult)
	}{
		{"missing manager event", func(r *ReceiptResult) {
			r.Logs = r.Logs[1:]
		}},
		{"missing vault deposit", func(r *ReceiptResult) {
			r.Logs = append(r.Logs[:1], r.Logs[2:]...)
		}},
		{"missing mint", func(r *ReceiptResult) {
			r.Logs = r.Logs[:2]
		}},
		{"wrong manager emitter", func(r *ReceiptResult) {
			r.Logs[0].Address = demoFujiAsset
		}},
		{"wrong vault emitter", func(r *ReceiptResult) {
			r.Logs[1].Address = demoFujiAsset
		}},
		{"duplicate manager event", func(r *ReceiptResult) {
			r.Logs = append(r.Logs, r.Logs[0])
		}},
		{"removed event", func(r *ReceiptResult) {
			r.Logs[0].Removed = true
		}},
		{"wrong owner", func(r *ReceiptResult) {
			r.Logs[0].Topics[1] = common.Hash{}.Hex()
		}},
		{"wrong deposit sender", func(r *ReceiptResult) {
			r.Logs[1].Topics[1] = common.Hash{}.Hex()
		}},
		{"wrong mint recipient", func(r *ReceiptResult) {
			r.Logs[2].Topics[2] = common.Hash{}.Hex()
		}},
		{"wrong mint source", func(r *ReceiptResult) {
			r.Logs[2].Topics[1] = r.Logs[2].Topics[2]
		}},
		{"truncated data", func(r *ReceiptResult) {
			r.Logs[0].Data = "0x01"
		}},
		{"wrong adapter", func(r *ReceiptResult) {
			data, _ := hexutil.Decode(r.Logs[0].Data)
			data[31] ^= 1
			r.Logs[0].Data = hexutil.Encode(data)
		}},
		{"wrong assets", func(r *ReceiptResult) {
			data, _ := hexutil.Decode(r.Logs[0].Data)
			data[63] ^= 1
			r.Logs[0].Data = hexutil.Encode(data)
		}},
		{"shares below minimum", func(r *ReceiptResult) {
			data, _ := hexutil.Decode(r.Logs[0].Data)
			for i := 64; i < 96; i++ {
				data[i] = 0
			}
			r.Logs[0].Data = hexutil.Encode(data)
		}},
		{"vault shares differ", func(r *ReceiptResult) {
			data, _ := hexutil.Decode(r.Logs[1].Data)
			data[63] ^= 1
			r.Logs[1].Data = hexutil.Encode(data)
		}},
		{"mint shares differ", func(r *ReceiptResult) {
			data, _ := hexutil.Decode(r.Logs[2].Data)
			data[31] ^= 1
			r.Logs[2].Data = hexutil.Encode(data)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			intent, receipt := depositReceiptFixture(t)
			test.mutate(&receipt)
			if _, err := verifyDepositEvents(intent, receipt); err == nil {
				t.Fatal("expected event mismatch")
			}
		})
	}
}

type depositReceiptRPCStub struct {
	ReceiptRPCClient
	tx      *types.Transaction
	pending bool
	receipt *types.Receipt
}

func (s *depositReceiptRPCStub) ChainID(
	ctx context.Context,
) (*big.Int, error) {
	return big.NewInt(FujiChainID), nil
}

func (s *depositReceiptRPCStub) TransactionByHash(
	ctx context.Context,
	hash common.Hash,
) (*types.Transaction, bool, error) {
	return s.tx, s.pending, nil
}

func (s *depositReceiptRPCStub) TransactionReceipt(
	ctx context.Context,
	hash common.Hash,
) (*types.Receipt, error) {
	if s.pending {
		return nil, ethereum.NotFound
	}
	return s.receipt, nil
}

func TestVerifyDepositTxStatuses(t *testing.T) {
	for _, scenario := range []string{
		"succeeded", "pending", "failed", "missing event",
	} {
		t.Run(scenario, func(t *testing.T) {
			intent, fixture := depositReceiptFixture(t)

			key, err := crypto.GenerateKey()
			if err != nil {
				t.Fatal(err)
			}
			owner := crypto.PubkeyToAddress(key.PublicKey)
			intent.Tx.From = owner.Hex()

			// Update all owner-indexed fields to match the signer.
			ownerTopic := common.BytesToHash(owner.Bytes()).Hex()
			fixture.Logs[0].Topics[1] = ownerTopic
			fixture.Logs[1].Topics[2] = ownerTopic
			fixture.Logs[2].Topics[2] = ownerTopic

			data, err := hexutil.Decode(intent.Tx.Data)
			if err != nil {
				t.Fatal(err)
			}
			target := common.HexToAddress(intent.Tx.To)
			tx, err := types.SignTx(
				types.NewTx(&types.DynamicFeeTx{
					ChainID:   big.NewInt(FujiChainID),
					Nonce:     1,
					To:        &target,
					Value:     big.NewInt(0),
					Gas:       500000,
					GasFeeCap: big.NewInt(100),
					GasTipCap: big.NewInt(1),
					Data:      data,
				}),
				types.LatestSignerForChainID(big.NewInt(FujiChainID)),
				key,
			)
			if err != nil {
				t.Fatal(err)
			}

			blockHash := common.HexToHash("0x1234")
			receipt := &types.Receipt{
				TxHash:           tx.Hash(),
				BlockNumber:      big.NewInt(123),
				BlockHash:        blockHash,
				TransactionIndex: 0,
				Status:           types.ReceiptStatusSuccessful,
			}
			for i, item := range fixture.Logs {
				topics := make([]common.Hash, len(item.Topics))
				for j, topic := range item.Topics {
					topics[j] = common.HexToHash(topic)
				}
				raw, err := hexutil.Decode(item.Data)
				if err != nil {
					t.Fatal(err)
				}
				receipt.Logs = append(receipt.Logs, &types.Log{
					Address:     common.HexToAddress(item.Address),
					Topics:      topics,
					Data:        raw,
					BlockNumber: 123,
					BlockHash:   blockHash,
					TxHash:      tx.Hash(),
					TxIndex:     0,
					Index:       uint(i),
				})
			}

			stub := &depositReceiptRPCStub{
				tx:      tx,
				receipt: receipt,
			}
			expectedStatus := TransactionStatusSucceeded
			switch scenario {
			case "pending":
				stub.pending = true
				expectedStatus = TransactionStatusPending
			case "failed":
				receipt.Status = types.ReceiptStatusFailed
				receipt.Logs = nil
				expectedStatus = TransactionStatusFailed
			case "missing event":
				receipt.Logs = receipt.Logs[:2]
			}

			reader, err := NewRPCReceiptReader(stub)
			if err != nil {
				t.Fatal(err)
			}
			result, err := reader.VerifyDepositTx(
				context.Background(),
				tx.Hash().Hex(),
				intent,
			)

			if scenario == "missing event" {
				if err == nil || result.Verified {
					t.Fatal("missing event must reject verification")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if result.Receipt.Status != expectedStatus {
				t.Fatalf("unexpected status: %s", result.Receipt.Status)
			}

			if scenario == "succeeded" {
				if !result.Verified ||
					result.SharesOutRaw == nil ||
					*result.SharesOutRaw != "900000000000000000" {
					t.Fatalf("unexpected verified result: %+v", result)
				}
			} else if result.Verified || result.SharesOutRaw != nil {
				t.Fatal("pending/reverted deposit must not be verified")
			}
		})
	}
}
