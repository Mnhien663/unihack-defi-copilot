package avalanche

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
)

type TransactionStatus string

const (
	TransactionStatusPending   TransactionStatus = "pending"
	TransactionStatusSucceeded TransactionStatus = "succeeded"
	TransactionStatusFailed    TransactionStatus = "failed"
)

// ReceiptLog preserves raw event data without guessing its meaning.
type ReceiptLog struct {
	Address          string   `json:"address"`
	Topics           []string `json:"topics"`
	Data             string   `json:"data"`
	BlockNumber      uint64   `json:"block_number"`
	TransactionHash  string   `json:"transaction_hash"`
	TransactionIndex uint     `json:"transaction_index"`
	LogIndex         uint     `json:"log_index"`
	Removed          bool     `json:"removed"`
}

type ReceiptResult struct {
	TransactionHash string            `json:"transaction_hash"`
	FromAddress     string            `json:"from_address"`
	ToAddress       *string           `json:"to_address"`
	Input           string            `json:"input"`
	ValueRaw        string            `json:"value_raw"`
	ChainID         uint64            `json:"chain_id"`
	BlockNumber     *uint64           `json:"block_number"`
	BlockHash       *string           `json:"block_hash"`
	Status          TransactionStatus `json:"status"`
	Logs            []ReceiptLog      `json:"logs"`
	Provenance      Provenance        `json:"provenance"`
}

type ReceiptReader interface {
	ReadReceipt(
		ctx context.Context,
		transactionHash string,
	) (ReceiptResult, error)
}

// Separate from RPCReader so existing balance mocks remain unchanged.
type ReceiptRPCClient interface {
	ChainID(context.Context) (*big.Int, error)

	TransactionByHash(
		context.Context,
		common.Hash,
	) (*types.Transaction, bool, error)

	TransactionReceipt(
		context.Context,
		common.Hash,
	) (*types.Receipt, error)
}

type RPCReceiptReader struct {
	client ReceiptRPCClient
}

func NewRPCReceiptReader(
	client ReceiptRPCClient,
) (*RPCReceiptReader, error) {
	if client == nil {
		return nil, errors.New("receipt RPC client is nil")
	}

	return &RPCReceiptReader{client: client}, nil
}

func (r *RPCReceiptReader) ReadReceipt(
	ctx context.Context,
	transactionHash string,
) (ReceiptResult, error) {
	if err := ctx.Err(); err != nil {
		return ReceiptResult{}, err
	}

	hashBytes, err := hexutil.Decode(transactionHash)
	if err != nil || len(hashBytes) != common.HashLength {
		return ReceiptResult{}, errors.New(
			"transaction hash must be 0x-prefixed 32-byte hex",
		)
	}
	hash := common.BytesToHash(hashBytes)

	readCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	chainID, err := r.client.ChainID(readCtx)
	if err != nil {
		return ReceiptResult{}, fmt.Errorf("verify receipt chain: %w", err)
	}
	if chainID == nil || chainID.Cmp(big.NewInt(FujiChainID)) != 0 {
		return ReceiptResult{}, fmt.Errorf(
			"%w: receipt requires Fuji chain ID 43113",
			ErrWrongChain,
		)
	}

	tx, pending, err := r.client.TransactionByHash(readCtx, hash)
	if err != nil {
		return ReceiptResult{}, fmt.Errorf("read transaction: %w", err)
	}
	if tx == nil || tx.Hash() != hash {
		return ReceiptResult{}, errors.New(
			"RPC returned a missing or mismatched transaction",
		)
	}
	if !tx.Protected() || tx.ChainId().Cmp(chainID) != 0 {
		return ReceiptResult{}, fmt.Errorf(
			"%w: transaction chain does not match Fuji",
			ErrWrongChain,
		)
	}

	from, err := types.Sender(types.LatestSignerForChainID(chainID), tx)
	if err != nil {
		return ReceiptResult{}, fmt.Errorf("recover transaction sender: %w", err)
	}

	result := ReceiptResult{
		TransactionHash: hash.Hex(),
		FromAddress:     from.Hex(),
		Input:           hexutil.Encode(tx.Data()),
		ValueRaw:        tx.Value().String(),
		ChainID:         uint64(FujiChainID),
		Status:          TransactionStatusPending,
		Logs:            make([]ReceiptLog, 0),
		Provenance: Provenance{
			Source:     "Avalanche RPC",
			SourceType: "rpc",
			ChainID:    uint64(FujiChainID),
			Mode:       "live",
			IsDemo:     false,
		},
	}
	if to := tx.To(); to != nil {
		address := to.Hex()
		result.ToAddress = &address
	}

	receipt, err := r.client.TransactionReceipt(readCtx, hash)
	if errors.Is(err, ethereum.NotFound) && pending {
		result.Provenance.ObservedAt = time.Now().UTC()
		return result, nil
	}
	if err != nil {
		return ReceiptResult{}, fmt.Errorf("read receipt: %w", err)
	}
	if receipt == nil {
		return ReceiptResult{}, errors.New("RPC returned a nil receipt")
	}
	if receipt.TxHash != hash ||
		receipt.BlockNumber == nil ||
		!receipt.BlockNumber.IsUint64() ||
		receipt.BlockHash == (common.Hash{}) {
		return ReceiptResult{}, errors.New(
			"RPC returned invalid receipt identity or block",
		)
	}

	switch receipt.Status {
	case types.ReceiptStatusSuccessful:
		result.Status = TransactionStatusSucceeded
	case types.ReceiptStatusFailed:
		result.Status = TransactionStatusFailed
	default:
		return ReceiptResult{}, errors.New("RPC returned invalid receipt status")
	}

	block := receipt.BlockNumber.Uint64()
	blockHash := receipt.BlockHash.Hex()
	result.BlockNumber = &block
	result.BlockHash = &blockHash
	result.Provenance.BlockNumber = &block

	for _, entry := range receipt.Logs {
		if entry == nil {
			return ReceiptResult{}, errors.New("RPC returned a nil receipt log")
		}
		if entry.TxHash != hash ||
			entry.BlockHash != receipt.BlockHash ||
			entry.BlockNumber != block ||
			entry.TxIndex != receipt.TransactionIndex ||
			entry.Removed {
			return ReceiptResult{}, errors.New(
				"RPC returned an inconsistent receipt log",
			)
		}

		topics := make([]string, len(entry.Topics))
		for i, topic := range entry.Topics {
			topics[i] = topic.Hex()
		}

		result.Logs = append(result.Logs, ReceiptLog{
			Address:          entry.Address.Hex(),
			Topics:           topics,
			Data:             hexutil.Encode(entry.Data),
			BlockNumber:      entry.BlockNumber,
			TransactionHash:  entry.TxHash.Hex(),
			TransactionIndex: entry.TxIndex,
			LogIndex:         entry.Index,
			Removed:          entry.Removed,
		})
	}

	result.Provenance.ObservedAt = time.Now().UTC()
	return result, nil
}

// VerifyReviewedSwapTransaction compares the transaction returned by RPC
// with the unsigned transaction stored at confirmation time.
// It does not determine whether the transaction was mined successfully.
func VerifyReviewedSwapTransaction(
	requestedHash string,
	reviewed UnsignedTx,
	observed ReceiptResult,
) error {
	wantHash, err := hexutil.Decode(requestedHash)
	if err != nil || len(wantHash) != common.HashLength {
		return errors.New("invalid requested transaction hash")
	}
	gotHash, err := hexutil.Decode(observed.TransactionHash)
	if err != nil || !bytes.Equal(gotHash, wantHash) {
		return errors.New("RPC transaction hash does not match requested hash")
	}

	if reviewed.ChainID != uint64(FujiChainID) ||
		observed.ChainID != reviewed.ChainID {
		return errors.New("transaction chain does not match reviewed swap")
	}

	if !common.IsHexAddress(reviewed.From) ||
		!common.IsHexAddress(observed.FromAddress) ||
		common.HexToAddress(observed.FromAddress) != common.HexToAddress(reviewed.From) {
		return errors.New("transaction sender does not match reviewed swap")
	}
	if observed.ToAddress == nil ||
		!common.IsHexAddress(reviewed.To) ||
		!common.IsHexAddress(*observed.ToAddress) ||
		common.HexToAddress(*observed.ToAddress) != common.HexToAddress(reviewed.To) {
		return errors.New("transaction destination does not match reviewed swap")
	}

	// AllowanceManager swaps in this flow send no native value.
	if reviewed.Value != "0" || observed.ValueRaw != reviewed.Value {
		return errors.New("transaction value does not match reviewed swap")
	}

	wantData, err := hexutil.Decode(reviewed.Data)
	if err != nil || len(wantData) < 4 {
		return errors.New("invalid reviewed swap calldata")
	}
	gotData, err := hexutil.Decode(observed.Input)
	if err != nil || !bytes.Equal(gotData, wantData) {
		return errors.New("transaction calldata does not match reviewed swap")
	}

	return nil
}

var _ ReceiptReader = (*RPCReceiptReader)(nil)
