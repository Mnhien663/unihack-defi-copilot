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

// ExpectedSwapIntent must come from the trusted, confirmed action.
// Do not construct it from the transaction being verified or from
// unchecked fields submitted alongside its hash.
type ExpectedSwapIntent struct {
	Tx UnsignedTx `json:"tx"`

	AdapterAddress  string `json:"adapter_address"`
	TokenInAddress  string `json:"token_in_address"`
	TokenOutAddress string `json:"token_out_address"`

	AmountInRaw     string `json:"amount_in_raw"`
	MinAmountOutRaw string `json:"min_amount_out_raw"`
	Deadline        uint64 `json:"deadline"`
}

// VerifiedSwapResult keeps transaction status separate from verification.
// A pending or reverted transaction must never have Verified = true.
type VerifiedSwapResult struct {
	Receipt ReceiptResult `json:"receipt"`

	Verified     bool    `json:"verified"`
	AmountOutRaw *string `json:"amount_out_raw"`
}

var (
	ErrInvalidSwapIntent = errors.New(
		"invalid expected swap intent",
	)

	ErrTransactionIntentMismatch = errors.New(
		"transaction does not match expected swap intent",
	)

	ErrSwapEventMismatch = errors.New(
		"swap execution event missing or mismatched",
	)
)

// validateExpectedSwapIntent checks internal consistency.
// It does not establish user confirmation or session ownership.
func validateExpectedSwapIntent(
	intent ExpectedSwapIntent,
) ([]byte, error) {
	invalid := func(reason string) ([]byte, error) {
		return nil, fmt.Errorf("%w: %s", ErrInvalidSwapIntent, reason)
	}

	if intent.Tx.ChainID != uint64(FujiChainID) {
		return invalid("chain must be Fuji")
	}

	for _, address := range []string{
		intent.Tx.From,
		intent.Tx.To,
		intent.AdapterAddress,
		intent.TokenInAddress,
		intent.TokenOutAddress,
	} {
		if !common.IsHexAddress(address) ||
			common.HexToAddress(address) == (common.Address{}) {
			return invalid("invalid or zero address")
		}
	}

	builder, err := NewSwapTxBuilder()
	if err != nil {
		return nil, fmt.Errorf("initialize intent validator: %w", err)
	}

	owner := common.HexToAddress(intent.Tx.From)
	manager := common.HexToAddress(intent.Tx.To)
	adapter := common.HexToAddress(intent.AdapterAddress)
	tokenIn := common.HexToAddress(intent.TokenInAddress)
	tokenOut := common.HexToAddress(intent.TokenOutAddress)

	if manager != builder.manager {
		return invalid("unexpected execution manager")
	}

	// This verifier currently supports the deployed LFJ route only.
	if adapter != common.HexToAddress(
		"0xD5cae0dF0f11a0Cf9818aCc47b11ce183b0E8AF5",
	) {
		return invalid("unexpected LFJ adapter")
	}

	x := common.HexToAddress(lfjFujiTokenX)
	y := common.HexToAddress(lfjFujiTokenY)
	if !((tokenIn == x && tokenOut == y) ||
		(tokenIn == y && tokenOut == x)) {
		return invalid("unsupported token pair")
	}

	for _, excluded := range []common.Address{
		manager,
		adapter,
		tokenIn,
		tokenOut,
		common.HexToAddress(lfjFujiRouter),
		common.HexToAddress(lfjFujiPair),
	} {
		if owner == excluded {
			return invalid("invalid swap owner")
		}
	}

	if intent.Tx.Value != "0" {
		return invalid("native value must be zero")
	}

	amountIn, err := lfjQuoteInput(intent.AmountInRaw)
	if err != nil {
		return invalid("invalid input amount")
	}

	minOut, err := lfjQuoteInput(intent.MinAmountOutRaw)
	if err != nil {
		return invalid("invalid minimum output")
	}

	if intent.Deadline == 0 {
		return invalid("deadline must be nonzero")
	}

	expectedData, err := builder.abi.Pack(
		"executeSwap",
		tokenIn,
		tokenOut,
		amountIn,
		minOut,
		new(big.Int).SetUint64(intent.Deadline),
	)
	if err != nil {
		return nil, fmt.Errorf("encode expected swap intent: %w", err)
	}

	intentData, err := hexutil.Decode(intent.Tx.Data)
	if err != nil || !bytes.Equal(intentData, expectedData) {
		return invalid("calldata does not match intent fields")
	}

	return expectedData, nil
}

// matchSwapTransaction compares actual transaction fields against
// trusted intent data. Receipt success and events are checked separately.
func matchSwapTransaction(
	intent ExpectedSwapIntent,
	actual ReceiptResult,
) error {
	expectedData, err := validateExpectedSwapIntent(intent)
	if err != nil {
		return err
	}

	mismatch := func(field string) error {
		return fmt.Errorf(
			"%w: %s",
			ErrTransactionIntentMismatch,
			field,
		)
	}

	if actual.ChainID != intent.Tx.ChainID {
		return mismatch("chain")
	}

	if !common.IsHexAddress(actual.FromAddress) ||
		common.HexToAddress(actual.FromAddress) !=
			common.HexToAddress(intent.Tx.From) {
		return mismatch("sender")
	}

	if actual.ToAddress == nil ||
		!common.IsHexAddress(*actual.ToAddress) ||
		common.HexToAddress(*actual.ToAddress) !=
			common.HexToAddress(intent.Tx.To) {
		return mismatch("destination")
	}

	value, err := ParseRawAmount(actual.ValueRaw)
	if err != nil || value.Sign() != 0 {
		return mismatch("native value")
	}

	actualData, err := hexutil.Decode(actual.Input)
	if err != nil || !bytes.Equal(actualData, expectedData) {
		return mismatch("calldata")
	}

	return nil
}

func verifySwapExecutedEvent(
	intent ExpectedSwapIntent,
	actual ReceiptResult,
) (string, error) {
	if _, err := validateExpectedSwapIntent(intent); err != nil {
		return "", err
	}

	builder, err := NewSwapTxBuilder()
	if err != nil {
		return "", err
	}

	event, exists := builder.abi.Events["SwapExecuted"]
	if !exists ||
		event.Sig != "SwapExecuted(address,address,address,address,uint256,uint256)" ||
		event.Anonymous ||
		len(event.Inputs) != 6 {
		return "", errors.New("unexpected SwapExecuted ABI")
	}

	// Require the indexed layout used by the deployed Manager.
	for i, input := range event.Inputs {
		if input.Indexed != (i < 3) {
			return "", errors.New("unexpected SwapExecuted indexed layout")
		}
	}

	mismatch := func(reason string) (string, error) {
		return "", fmt.Errorf("%w: %s", ErrSwapEventMismatch, reason)
	}

	expectedTopics := []common.Hash{
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
	}

	amountIn, err := lfjQuoteInput(intent.AmountInRaw)
	if err != nil {
		return "", err
	}
	minOut, err := lfjQuoteInput(intent.MinAmountOutRaw)
	if err != nil {
		return "", err
	}

	found := false
	amountOutRaw := ""

	for _, entry := range actual.Logs {
		if !common.IsHexAddress(entry.Address) ||
			common.HexToAddress(entry.Address) != builder.manager {
			continue
		}
		if len(entry.Topics) == 0 {
			continue
		}

		signature, err := hexutil.Decode(entry.Topics[0])
		if err != nil ||
			!bytes.Equal(signature, event.ID.Bytes()) {
			continue
		}

		if found {
			return mismatch("multiple SwapExecuted events")
		}
		found = true

		if entry.Removed || len(entry.Topics) != 4 {
			return mismatch("invalid event topics or removed log")
		}

		for i, expected := range expectedTopics {
			topic, err := hexutil.Decode(entry.Topics[i])
			if err != nil || !bytes.Equal(topic, expected.Bytes()) {
				return mismatch("owner or token topics differ")
			}
		}

		data, err := hexutil.Decode(entry.Data)
		if err != nil || len(data) != 3*32 {
			return mismatch("invalid event data")
		}

		values, err := event.Inputs.NonIndexed().Unpack(data)
		if err != nil || len(values) != 3 {
			return mismatch("cannot decode event data")
		}

		adapter, adapterOK := values[0].(common.Address)
		spent, spentOK := values[1].(*big.Int)
		received, receivedOK := values[2].(*big.Int)

		if !adapterOK || !spentOK || !receivedOK ||
			spent == nil || received == nil {
			return mismatch("invalid decoded event types")
		}

		// Re-encode to reject noncanonical ABI data.
		canonical, err := event.Inputs.NonIndexed().Pack(
			adapter, spent, received,
		)
		if err != nil || !bytes.Equal(data, canonical) {
			return mismatch("noncanonical event data")
		}

		if adapter != common.HexToAddress(intent.AdapterAddress) {
			return mismatch("adapter differs")
		}
		if spent.Cmp(amountIn) != 0 {
			return mismatch("input amount differs")
		}
		if received.Sign() <= 0 || received.Cmp(minOut) < 0 {
			return mismatch("output is below the confirmed minimum")
		}

		amountOutRaw = received.String()
	}

	if !found {
		return mismatch("SwapExecuted event missing")
	}

	return amountOutRaw, nil
}

// VerifyTx verifies chain execution against a trusted confirmed intent.
// It does not enforce session ownership or transaction-hash uniqueness;
// those checks belong to the action service and database.
func (r *RPCReceiptReader) VerifyTx(
	ctx context.Context,
	transactionHash string,
	intent ExpectedSwapIntent,
) (VerifiedSwapResult, error) {
	if err := ctx.Err(); err != nil {
		return VerifiedSwapResult{}, err
	}

	// Reject inconsistent intent before making RPC calls.
	if _, err := validateExpectedSwapIntent(intent); err != nil {
		return VerifiedSwapResult{}, err
	}

	receipt, err := r.ReadReceipt(ctx, transactionHash)
	if err != nil {
		return VerifiedSwapResult{}, err
	}

	result := VerifiedSwapResult{
		Receipt: receipt,
	}

	if err := matchSwapTransaction(intent, receipt); err != nil {
		return result, err
	}

	switch receipt.Status {
	case TransactionStatusPending:
		// Pending is not successful verification.
		return result, nil

	case TransactionStatusFailed:
		// Preserve the reverted receipt for the caller.
		return result, nil

	case TransactionStatusSucceeded:
		amountOut, err := verifySwapExecutedEvent(intent, receipt)
		if err != nil {
			return result, err
		}

		if err := ctx.Err(); err != nil {
			return result, err
		}

		result.Verified = true
		result.AmountOutRaw = &amountOut
		return result, nil

	default:
		return result, errors.New("unexpected transaction status")
	}
}

// WaitForVerifiedSwap polls pending transactions for at most 30 seconds.
// A timeout means verification is unfinished, not that the swap reverted.
func (r *RPCReceiptReader) WaitForVerifiedSwap(
	ctx context.Context,
	transactionHash string,
	intent ExpectedSwapIntent,
) (VerifiedSwapResult, error) {
	waitCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	delay := 250 * time.Millisecond
	const maxDelay = 2 * time.Second

	var last VerifiedSwapResult

	for {
		if err := waitCtx.Err(); err != nil {
			return last, err
		}

		result, err := r.VerifyTx(
			waitCtx,
			transactionHash,
			intent,
		)
		if err != nil {
			// Preserve the latest known receipt on RPC errors/timeouts.
			if result.Receipt.TransactionHash != "" {
				last = result
			}
			return last, err
		}
		last = result

		if err := waitCtx.Err(); err != nil {
			return last, err
		}

		if result.Receipt.Status != TransactionStatusPending {
			// Either verified success or a mined reverted transaction.
			return result, nil
		}

		timer := time.NewTimer(delay)
		select {
		case <-waitCtx.Done():
			timer.Stop()
			return last, waitCtx.Err()
		case <-timer.C:
		}

		if delay < maxDelay {
			delay *= 2
			if delay > maxDelay {
				delay = maxDelay
			}
		}
	}
}
