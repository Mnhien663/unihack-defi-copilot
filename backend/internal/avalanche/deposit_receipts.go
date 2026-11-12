package avalanche

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

type ExpectedDepositIntent struct {
	Tx             UnsignedTx `json:"tx"`
	AdapterAddress string     `json:"adapter_address"`
	VaultAddress   string     `json:"vault_address"`
	AssetAddress   string     `json:"asset_address"`
	AssetsRaw      string     `json:"assets_raw"`
	MinSharesRaw   string     `json:"min_shares_raw"`
	Deadline       uint64     `json:"deadline"`
}

type VerifiedDepositResult struct {
	Receipt      ReceiptResult `json:"receipt"`
	Verified     bool          `json:"verified"`
	SharesOutRaw *string       `json:"shares_out_raw"`
}

var (
	ErrInvalidDepositIntent = errors.New("invalid expected deposit intent")
	ErrDepositEventMismatch = errors.New("deposit event does not match intent")
)

// Validates historical intent without checking the current wall clock.
// Expiry must be checked before signing; mined transactions remain verifiable.
func validateExpectedDepositIntent(
	intent ExpectedDepositIntent,
) ([]byte, error) {
	invalid := func(field string) error {
		return fmt.Errorf("%w: %s", ErrInvalidDepositIntent, field)
	}
	if intent.Tx.ChainID != uint64(FujiChainID) {
		return nil, invalid("chain")
	}

	for _, address := range []string{
		intent.Tx.From,
		intent.Tx.To,
		intent.AdapterAddress,
		intent.VaultAddress,
		intent.AssetAddress,
	} {
		if !common.IsHexAddress(address) ||
			common.HexToAddress(address) == (common.Address{}) {
			return nil, invalid("address")
		}
	}

	manager := common.HexToAddress(fujiExecutionManager)
	adapter := common.HexToAddress(fujiVaultExecutionAdapter)
	vault := common.HexToAddress(demoFujiVault)
	asset := common.HexToAddress(demoFujiAsset)
	owner := common.HexToAddress(intent.Tx.From)

	if common.HexToAddress(intent.Tx.To) != manager ||
		common.HexToAddress(intent.AdapterAddress) != adapter ||
		common.HexToAddress(intent.VaultAddress) != vault ||
		common.HexToAddress(intent.AssetAddress) != asset {
		return nil, invalid("configured deposit route")
	}
	for _, forbidden := range []common.Address{
		manager, adapter, vault, asset,
	} {
		if owner == forbidden {
			return nil, invalid("owner")
		}
	}
	if intent.Tx.Value != "0" {
		return nil, invalid("native value")
	}
	if intent.Deadline == 0 {
		return nil, invalid("deadline")
	}

	assets, err := ParseRawAmount(intent.AssetsRaw)
	if err != nil || assets == nil ||
		assets.Sign() <= 0 || assets.BitLen() > 256 {
		return nil, invalid("assets")
	}
	minimum, err := ParseRawAmount(intent.MinSharesRaw)
	if err != nil || minimum == nil ||
		minimum.Sign() <= 0 || minimum.BitLen() > 256 {
		return nil, invalid("minimum shares")
	}

	builder, err := NewDepositTxBuilder()
	if err != nil {
		return nil, err
	}
	expected, err := builder.abi.Pack(
		"executeDeposit",
		asset,
		assets,
		minimum,
		new(big.Int).SetUint64(intent.Deadline),
	)
	if err != nil {
		return nil, err
	}
	actual, err := hexutil.Decode(intent.Tx.Data)
	if err != nil || !bytes.Equal(actual, expected) {
		return nil, invalid("calldata")
	}
	return expected, nil
}

func matchDepositTransaction(
	intent ExpectedDepositIntent,
	actual ReceiptResult,
) error {
	expectedData, err := validateExpectedDepositIntent(intent)
	if err != nil {
		return err
	}
	mismatch := func(field string) error {
		return fmt.Errorf(
			"%w: deposit %s",
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
	if err != nil || value == nil || value.Sign() != 0 {
		return mismatch("native value")
	}
	data, err := hexutil.Decode(actual.Input)
	if err != nil || !bytes.Equal(data, expectedData) {
		return mismatch("calldata")
	}
	return nil
}

func verifyDepositEvents(
	intent ExpectedDepositIntent,
	receipt ReceiptResult,
) (string, error) {
	if _, err := validateExpectedDepositIntent(intent); err != nil {
		return "", err
	}
	builder, err := NewDepositTxBuilder()
	if err != nil {
		return "", err
	}
	vaultEvents, err := abi.JSON(strings.NewReader(`[
		{"type":"event","name":"Deposit","anonymous":false,
		 "inputs":[
			{"name":"sender","type":"address","indexed":true},
			{"name":"owner","type":"address","indexed":true},
			{"name":"assets","type":"uint256","indexed":false},
			{"name":"shares","type":"uint256","indexed":false}
		 ]},
		{"type":"event","name":"Transfer","anonymous":false,
		 "inputs":[
			{"name":"from","type":"address","indexed":true},
			{"name":"to","type":"address","indexed":true},
			{"name":"value","type":"uint256","indexed":false}
		 ]}
	]`))
	if err != nil {
		return "", err
	}

	managerEvent, ok := builder.abi.Events["DepositExecuted"]
	if !ok ||
		managerEvent.Sig !=
			"DepositExecuted(address,address,address,address,uint256,uint256)" ||
		managerEvent.Anonymous ||
		len(managerEvent.Inputs) != 6 {
		return "", errors.New("unexpected manager DepositExecuted ABI")
	}
	for i, input := range managerEvent.Inputs {
		if input.Indexed != (i < 3) {
			return "", errors.New("unexpected DepositExecuted indexed fields")
		}
	}

	depositEvent := vaultEvents.Events["Deposit"]
	transferEvent := vaultEvents.Events["Transfer"]
	owner := common.HexToAddress(intent.Tx.From)
	adapter := common.HexToAddress(intent.AdapterAddress)
	asset := common.HexToAddress(intent.AssetAddress)
	vault := common.HexToAddress(intent.VaultAddress)
	manager := common.HexToAddress(intent.Tx.To)

	topicAddress := func(address common.Address) common.Hash {
		return common.BytesToHash(address.Bytes())
	}
	mismatch := func(reason string) error {
		return fmt.Errorf("%w: %s", ErrDepositEventMismatch, reason)
	}

	// Select events only from their configured emitter.
	// Matching signature events from other contracts do not count.
	findOne := func(
		emitter common.Address,
		event abi.Event,
		topics []common.Hash,
		dataLength int,
	) ([]interface{}, error) {
		var found *ReceiptLog
		for i := range receipt.Logs {
			log := &receipt.Logs[i]
			if !common.IsHexAddress(log.Address) ||
				common.HexToAddress(log.Address) != emitter ||
				len(log.Topics) == 0 {
				continue
			}
			first, err := hexutil.Decode(log.Topics[0])
			if err != nil || !bytes.Equal(first, event.ID.Bytes()) {
				continue
			}
			if found != nil {
				return nil, mismatch("duplicate " + event.Name)
			}
			found = log
		}
		if found == nil {
			return nil, mismatch("missing " + event.Name)
		}
		if found.Removed || len(found.Topics) != len(topics) {
			return nil, mismatch("invalid " + event.Name + " topics")
		}
		for i, expected := range topics {
			raw, err := hexutil.Decode(found.Topics[i])
			if err != nil || !bytes.Equal(raw, expected.Bytes()) {
				return nil, mismatch(event.Name + " indexed fields")
			}
		}
		raw, err := hexutil.Decode(found.Data)
		if err != nil || len(raw) != dataLength {
			return nil, mismatch(event.Name + " data length")
		}
		fields := event.Inputs.NonIndexed()
		values, err := fields.Unpack(raw)
		if err != nil {
			return nil, mismatch(event.Name + " data")
		}
		canonical, err := fields.Pack(values...)
		if err != nil || !bytes.Equal(canonical, raw) {
			return nil, mismatch(event.Name + " non-canonical data")
		}
		return values, nil
	}

	managerValues, err := findOne(
		manager,
		managerEvent,
		[]common.Hash{
			managerEvent.ID,
			topicAddress(owner),
			topicAddress(asset),
			topicAddress(vault),
		},
		96,
	)
	if err != nil {
		return "", err
	}
	actualAdapter, ok := managerValues[0].(common.Address)
	if !ok || actualAdapter != adapter {
		return "", mismatch("adapter")
	}
	assets, ok := managerValues[1].(*big.Int)
	if !ok || assets == nil {
		return "", mismatch("assets")
	}
	shares, ok := managerValues[2].(*big.Int)
	if !ok || shares == nil || shares.Sign() <= 0 {
		return "", mismatch("shares")
	}
	expectedAssets, err := ParseRawAmount(intent.AssetsRaw)
	if err != nil {
		return "", err
	}
	minimum, err := ParseRawAmount(intent.MinSharesRaw)
	if err != nil {
		return "", err
	}
	if assets.Cmp(expectedAssets) != 0 || shares.Cmp(minimum) < 0 {
		return "", mismatch("amount or minimum shares")
	}

	depositValues, err := findOne(
		vault,
		depositEvent,
		[]common.Hash{
			depositEvent.ID,
			topicAddress(adapter),
			topicAddress(owner),
		},
		64,
	)
	if err != nil {
		return "", err
	}
	depositedAssets, ok := depositValues[0].(*big.Int)
	if !ok || depositedAssets == nil ||
		depositedAssets.Cmp(assets) != 0 {
		return "", mismatch("vault deposit assets")
	}
	depositedShares, ok := depositValues[1].(*big.Int)
	if !ok || depositedShares == nil ||
		depositedShares.Cmp(shares) != 0 {
		return "", mismatch("vault deposit shares")
	}

	mintValues, err := findOne(
		vault,
		transferEvent,
		[]common.Hash{
			transferEvent.ID,
			topicAddress(common.Address{}),
			topicAddress(owner),
		},
		32,
	)
	if err != nil {
		return "", err
	}
	mintedShares, ok := mintValues[0].(*big.Int)
	if !ok || mintedShares == nil || mintedShares.Cmp(shares) != 0 {
		return "", mismatch("minted shares")
	}
	return shares.String(), nil
}

func (r *RPCReceiptReader) VerifyDepositTx(
	ctx context.Context,
	transactionHash string,
	intent ExpectedDepositIntent,
) (VerifiedDepositResult, error) {
	var empty VerifiedDepositResult
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	if _, err := validateExpectedDepositIntent(intent); err != nil {
		return empty, err
	}
	receipt, err := r.ReadReceipt(ctx, transactionHash)
	if err != nil {
		return empty, err
	}
	result := VerifiedDepositResult{Receipt: receipt}

	if err := matchDepositTransaction(intent, receipt); err != nil {
		return result, err
	}
	switch receipt.Status {
	case TransactionStatusPending, TransactionStatusFailed:
		return result, nil

	case TransactionStatusSucceeded:
		shares, err := verifyDepositEvents(intent, receipt)
		if err != nil {
			return result, err
		}
		if err := ctx.Err(); err != nil {
			return result, err
		}
		result.Verified = true
		result.SharesOutRaw = &shares
		return result, nil

	default:
		return result, errors.New("unexpected deposit transaction status")
	}
}
