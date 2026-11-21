package avalanche

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/rpc"
)

type DepositSimulationResult struct {
	Transaction  BuiltDepositTransaction `json:"transaction"`
	Requirements DepositRequirements     `json:"requirements"`
	Status       string                  `json:"status"`

	SimulatedSharesRaw *string    `json:"simulated_shares_raw"`
	GasEstimate        *uint64    `json:"gas_estimate"`
	GasLimit           *uint64    `json:"gas_limit"`
	GasPriceWei        *string    `json:"gas_price_wei"`
	EstimatedFeeWei    *string    `json:"estimated_fee_wei"`
	RevertName         *string    `json:"revert_name"`
	Provenance         Provenance `json:"provenance"`
}

type DepositSimulator struct {
	client       SwapSimulationRPC
	builder      *DepositTxBuilder
	requirements *DepositRequirementsReader
}

func NewDepositSimulator(
	client SwapSimulationRPC,
) (*DepositSimulator, error) {
	if client == nil {
		return nil, errors.New("deposit simulation RPC client is nil")
	}
	builder, err := NewDepositTxBuilder()
	if err != nil {
		return nil, err
	}
	requirements, err := NewDepositRequirementsReader(client)
	if err != nil {
		return nil, err
	}
	return &DepositSimulator{
		client:       client,
		builder:      builder,
		requirements: requirements,
	}, nil
}

func (s *DepositSimulator) SimulateDeposit(
	ctx context.Context,
	request BuildDepositRequest,
) (DepositSimulationResult, error) {
	var empty DepositSimulationResult
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	readCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	built, err := s.builder.BuildDepositTx(request, time.Now().UTC())
	if err != nil {
		return empty, err
	}
	requirements, err := s.requirements.ReadDepositRequirements(
		readCtx, request,
	)
	if err != nil {
		return empty, err
	}

	result := DepositSimulationResult{
		Transaction:  built,
		Requirements: requirements,
		Status:       "blocked",
		Provenance:   requirements.Provenance,
	}
	finish := func() (DepositSimulationResult, error) {
		if err := readCtx.Err(); err != nil {
			return empty, err
		}
		if _, err := s.builder.BuildDepositTx(
			request, time.Now().UTC(),
		); err != nil {
			return empty, err
		}
		return result, nil
	}
	if len(requirements.Blockers) > 0 {
		return finish()
	}

	data, err := hexutil.Decode(built.Prepared.Tx.Data)
	if err != nil {
		return empty, err
	}
	target := common.HexToAddress(built.Prepared.Tx.To)
	msg := ethereum.CallMsg{
		From:  common.HexToAddress(built.Prepared.Tx.From),
		To:    &target,
		Value: big.NewInt(0),
		Data:  data,
	}
	block := new(big.Int).SetUint64(
		*requirements.Provenance.BlockNumber,
	)

	raw, err := s.client.CallContract(readCtx, msg, block)
	if err != nil {
		if name, reverted := decodeDepositRevert(err, s.builder.abi); reverted {
			result.Status = "reverted"
			result.RevertName = &name
			return finish()
		}
		return empty, fmt.Errorf("simulate deposit: %w", err)
	}
	if len(raw) != 32 {
		return empty, errors.New("invalid simulated deposit output length")
	}
	values, err := s.builder.abi.Methods["executeDeposit"].Outputs.Unpack(raw)
	if err != nil {
		return empty, err
	}
	if len(values) != 1 {
		return empty, errors.New("invalid simulated deposit output")
	}
	shares, ok := values[0].(*big.Int)
	minimum, err := ParseRawAmount(built.Summary.MinShares.Raw)
	if err != nil {
		return empty, err
	}
	if !ok || shares == nil || shares.Sign() <= 0 ||
		shares.BitLen() > 256 || shares.Cmp(minimum) < 0 {
		return empty, errors.New("simulated shares below intended minimum")
	}

	// Gas estimation uses current node state, not the pinned call block.
	gas, err := s.client.EstimateGas(readCtx, msg)
	if err != nil {
		if name, reverted := decodeDepositRevert(err, s.builder.abi); reverted {
			result.Status = "reverted"
			result.RevertName = &name
			return finish()
		}
		return empty, fmt.Errorf("estimate deposit gas: %w", err)
	}
	if gas == 0 {
		return empty, errors.New("zero deposit gas estimate")
	}

	buffered := new(big.Int).Mul(
		new(big.Int).SetUint64(gas), big.NewInt(120),
	)
	buffered.Add(buffered, big.NewInt(99))
	buffered.Quo(buffered, big.NewInt(100))
	if !buffered.IsUint64() {
		return empty, errors.New("deposit gas limit overflows uint64")
	}
	gasLimit := buffered.Uint64()

	gasPrice, err := s.client.SuggestGasPrice(readCtx)
	if err != nil {
		return empty, err
	}
	if gasPrice == nil || gasPrice.Sign() <= 0 ||
		gasPrice.BitLen() > 256 {
		return empty, errors.New("invalid deposit gas price")
	}
	fee := new(big.Int).Mul(
		new(big.Int).SetUint64(gasLimit), gasPrice,
	)
	native, err := ParseRawAmount(requirements.NativeBalanceWei)
	if err != nil {
		return empty, err
	}
	affordable := native.Cmp(fee) >= 0
	result.Requirements.GasAffordable = &affordable

	sharesRaw := shares.String()
	priceRaw := gasPrice.String()
	feeRaw := fee.String()
	result.SimulatedSharesRaw = &sharesRaw
	result.GasEstimate = &gas
	result.GasLimit = &gasLimit
	result.GasPriceWei = &priceRaw
	result.EstimatedFeeWei = &feeRaw

	if affordable {
		result.Status = "succeeded"
	} else {
		result.Requirements.Blockers = append(
			result.Requirements.Blockers,
			"INSUFFICIENT_GAS_BALANCE",
		)
	}

	// Combined simulation and current-state gas estimate have no single block.
	result.Provenance = Provenance{
		Source:     "Fuji RPC DemoVault deposit simulation",
		SourceType: "rpc",
		ChainID:    uint64(FujiChainID),
		ObservedAt: time.Now().UTC(),
		Mode:       "live",
		IsDemo:     true,
	}
	return finish()
}

func decodeDepositRevert(
	err error,
	definition abi.ABI,
) (string, bool) {
	if errors.Is(err, context.Canceled) ||
		errors.Is(err, context.DeadlineExceeded) {
		return "", false
	}
	var rpcErr rpc.Error
	var dataErr rpc.DataError
	if !errors.As(err, &rpcErr) || rpcErr.ErrorCode() != 3 ||
		!errors.As(err, &dataErr) {
		return "", false
	}
	encoded, ok := dataErr.ErrorData().(string)
	if !ok {
		return "", false
	}
	raw, decodeErr := hexutil.Decode(encoded)
	if decodeErr != nil {
		return "", false
	}
	if reason, unpackErr := abi.UnpackRevert(raw); unpackErr == nil {
		return reason, true
	}
	if len(raw) >= 4 {
		for _, item := range definition.Errors {
			if bytes.Equal(raw[:4], item.ID.Bytes()[:4]) {
				if _, unpackErr := item.Inputs.Unpack(raw[4:]); unpackErr == nil {
					return item.Name, true
				}
			}
		}
	}
	return "UnknownRevert", true
}

type DepositPreparationSimulator interface {
	SimulateDeposit(
		context.Context,
		BuildDepositRequest,
	) (DepositSimulationResult, error)
}

type PrepareDepositIntent struct {
	WalletAddress string
	ChainID       uint64
	AssetsRaw     string
	MinSharesRaw  string
}

type PrepareDepositResult struct {
	Status       string                   `json:"status"`
	Blockers     []string                 `json:"blockers"`
	Requirements DepositRequirements      `json:"requirements"`
	Transaction  *BuiltDepositTransaction `json:"transaction,omitempty"`
}

type DepositPreparer struct {
	reader    VaultMetricsReader
	simulator DepositPreparationSimulator
	maxAssets *big.Int
}

func NewDepositPreparer(
	reader VaultMetricsReader,
	simulator DepositPreparationSimulator,
	maxAssetsRaw string,
) (*DepositPreparer, error) {
	if reader == nil || simulator == nil {
		return nil, errors.New("deposit reader and simulator are required")
	}

	maximum, err := ParseRawAmount(maxAssetsRaw)
	if err != nil {
		return nil, fmt.Errorf("parse deposit cap: %w", err)
	}
	if maximum == nil || maximum.Sign() <= 0 || maximum.BitLen() > 256 {
		return nil, errors.New("deposit cap must be a positive uint256")
	}

	return &DepositPreparer{
		reader:    reader,
		simulator: simulator,
		maxAssets: maximum,
	}, nil
}

func (p *DepositPreparer) PrepareDeposit(
	ctx context.Context,
	sessionWallet string,
	intent PrepareDepositIntent,
) (PrepareDepositResult, error) {
	var empty PrepareDepositResult

	if err := ctx.Err(); err != nil {
		return empty, err
	}
	if intent.ChainID != uint64(FujiChainID) {
		return empty, ErrWrongChain
	}

	sessionWallet = strings.TrimSpace(sessionWallet)
	intent.WalletAddress = strings.TrimSpace(intent.WalletAddress)

	if !common.IsHexAddress(sessionWallet) ||
		!common.IsHexAddress(intent.WalletAddress) {
		return empty, ErrInvalidAddress
	}

	owner := common.HexToAddress(sessionWallet)
	if owner == (common.Address{}) ||
		owner != common.HexToAddress(intent.WalletAddress) {
		return empty, errors.New("deposit wallet must match session wallet")
	}

	assets, err := ParseRawAmount(intent.AssetsRaw)
	if err != nil {
		return empty, fmt.Errorf("parse deposit assets: %w", err)
	}
	if assets == nil || assets.Sign() <= 0 || assets.BitLen() > 256 {
		return empty, errors.New("deposit assets must be a positive uint256")
	}
	if assets.Cmp(p.maxAssets) > 0 {
		return empty, errors.New("deposit assets exceed application cap")
	}

	minimum, err := ParseRawAmount(intent.MinSharesRaw)
	if err != nil {
		return empty, fmt.Errorf("parse minimum shares: %w", err)
	}
	if minimum == nil || minimum.Sign() <= 0 || minimum.BitLen() > 256 {
		return empty, errors.New("minimum shares must be a positive uint256")
	}

	metrics, err := p.reader.ReadVault(ctx, VaultReadRequest{
		VaultAddress:     demoFujiVault,
		WalletAddress:    owner.Hex(),
		DepositAssetsRaw: assets.String(),
	})
	if err != nil {
		return empty, fmt.Errorf("read deposit preview: %w", err)
	}

	// Reject a preview for a different amount before simulation.
	previewAssets, err := ParseRawAmount(metrics.DepositAssets.Raw)
	if err != nil || previewAssets == nil ||
		previewAssets.Cmp(assets) != 0 {
		return empty, errors.New("deposit preview amount mismatch")
	}

	deadline := metrics.Position.Provenance.ObservedAt.
		Add(60 * time.Second).Unix()
	if deadline <= time.Now().UTC().Unix() {
		return empty, errors.New("deposit preview expired")
	}

	request := BuildDepositRequest{
		OwnerAddress: owner.Hex(),
		Metrics:      metrics,
		MinSharesRaw: minimum.String(),
		Deadline:     uint64(deadline),
	}

	// The builder checks wallet, token, vault, freshness and minimum shares.
	builder, err := NewDepositTxBuilder()
	if err != nil {
		return empty, err
	}
	expected, err := builder.BuildDepositTx(request, time.Now().UTC())
	if err != nil {
		return empty, err
	}

	simulation, err := p.simulator.SimulateDeposit(ctx, request)
	if err != nil {
		return empty, fmt.Errorf("simulate prepared deposit: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}

	// Recheck expiry and preview validity after waiting for RPC.
	if _, err := builder.BuildDepositTx(request, time.Now().UTC()); err != nil {
		return empty, err
	}

	result := PrepareDepositResult{
		Status:       simulation.Status,
		Blockers:     append([]string{}, simulation.Requirements.Blockers...),
		Requirements: simulation.Requirements,
	}

	switch simulation.Status {
	case "blocked":
		if len(result.Blockers) == 0 {
			return empty, errors.New("blocked deposit has no blocker")
		}
		if len(result.Blockers) == 1 &&
			result.Blockers[0] == "ERC20_APPROVAL_REQUIRED" {
			result.Status = "approval_required"
		}
		return result, nil

	case "reverted":
		return result, nil

	case "succeeded":
		if len(result.Blockers) != 0 ||
			simulation.Requirements.GasAffordable == nil ||
			!*simulation.Requirements.GasAffordable {
			return empty, errors.New("inconsistent successful deposit simulation")
		}

		// Bind the returned transaction to the validated request.
		if simulation.Transaction.Prepared.Tx != expected.Prepared.Tx {
			return empty, errors.New("simulated deposit transaction mismatch")
		}

		result.Status = "ready_for_review"
		result.Transaction = &expected
		return result, nil

	default:
		return empty, errors.New("unknown deposit simulation status")
	}
}

// RevalidateReviewedDeposit checks the exact transaction previously reviewed.
// It does not update the action, sign or broadcast a transaction.
func (p *DepositPreparer) RevalidateReviewedDeposit(
	ctx context.Context,
	reviewedTx UnsignedTx,
	summary DepositTxSummary,
) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}

	now := time.Now().UTC()
	if summary.Deadline <= uint64(now.Unix()) ||
		!summary.PreviewExpiresAt.After(now) {
		return true, nil
	}

	if reviewedTx.ChainID != uint64(FujiChainID) ||
		!common.IsHexAddress(reviewedTx.From) ||
		!common.IsHexAddress(summary.OwnerAddress) ||
		common.HexToAddress(reviewedTx.From) !=
			common.HexToAddress(summary.OwnerAddress) {
		return false, errors.New("invalid reviewed deposit owner or chain")
	}

	assets, err := ParseRawAmount(summary.Assets.Raw)
	if err != nil {
		return false, fmt.Errorf("parse reviewed deposit assets: %w", err)
	}
	if assets == nil || assets.Sign() <= 0 ||
		assets.BitLen() > 256 || assets.Cmp(p.maxAssets) > 0 {
		return false, errors.New("reviewed deposit exceeds amount policy")
	}

	metrics, err := p.reader.ReadVault(ctx, VaultReadRequest{
		VaultAddress:     demoFujiVault,
		WalletAddress:    summary.OwnerAddress,
		DepositAssetsRaw: assets.String(),
	})
	if err != nil {
		return false, fmt.Errorf("refresh deposit preview: %w", err)
	}

	freshAssets, err := ParseRawAmount(metrics.DepositAssets.Raw)
	if err != nil || freshAssets == nil || freshAssets.Cmp(assets) != 0 {
		return false, errors.New("refreshed deposit amount mismatch")
	}

	// Preserve the minimum and deadline the user actually reviewed.
	request := BuildDepositRequest{
		OwnerAddress: summary.OwnerAddress,
		Metrics:      metrics,
		MinSharesRaw: summary.MinShares.Raw,
		Deadline:     summary.Deadline,
	}

	builder, err := NewDepositTxBuilder()
	if err != nil {
		return false, err
	}

	expected, err := builder.BuildDepositTx(request, time.Now().UTC())
	if err != nil {
		return false, fmt.Errorf("validate reviewed deposit: %w", err)
	}

	// A changed transaction requires a new review.
	if expected.Prepared.Tx != reviewedTx {
		return true, nil
	}

	if summary.RecipientAddress != expected.Summary.RecipientAddress ||
		summary.ManagerAddress != expected.Summary.ManagerAddress ||
		summary.VaultAddress != expected.Summary.VaultAddress ||
		summary.Assets != expected.Summary.Assets ||
		summary.MinShares != expected.Summary.MinShares {
		return false, errors.New("reviewed deposit summary mismatch")
	}

	simulation, err := p.simulator.SimulateDeposit(ctx, request)
	if err != nil {
		return false, fmt.Errorf("re-simulate reviewed deposit: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}

	if simulation.Status != "succeeded" ||
		len(simulation.Requirements.Blockers) != 0 ||
		simulation.Requirements.GasAffordable == nil ||
		!*simulation.Requirements.GasAffordable {
		return true, nil
	}

	if simulation.Transaction.Prepared.Tx != reviewedTx {
		return true, nil
	}

	if _, err := builder.BuildDepositTx(request, time.Now().UTC()); err != nil {
		return false, fmt.Errorf("recheck reviewed deposit expiry: %w", err)
	}

	return false, nil
}
