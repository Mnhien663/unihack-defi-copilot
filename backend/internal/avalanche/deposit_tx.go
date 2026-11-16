package avalanche

import (
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

// Metrics must come from the backend vault reader.
// OwnerAddress must come from validated wallet context.
type BuildDepositRequest struct {
	OwnerAddress string             `json:"owner_address"`
	Metrics      VaultMetricsResult `json:"metrics"`
	MinSharesRaw string             `json:"min_shares_raw"`
	Deadline     uint64             `json:"deadline"`
}

type DepositTxSummary struct {
	OwnerAddress     string      `json:"owner_address"`
	RecipientAddress string      `json:"recipient_address"`
	ManagerAddress   string      `json:"manager_address"`
	VaultAddress     string      `json:"vault_address"`
	Assets           TokenAmount `json:"assets"`
	PreviewShares    TokenAmount `json:"preview_shares"`
	MinShares        TokenAmount `json:"min_shares"`
	Deadline         uint64      `json:"deadline"`
	PreviewExpiresAt time.Time   `json:"preview_expires_at"`
}

type BuiltDepositTransaction struct {
	Prepared PreparedTransaction `json:"prepared"`
	Summary  DepositTxSummary    `json:"summary"`
}

type DepositTxBuilder struct {
	manager common.Address
	abi     abi.ABI
}

func NewDepositTxBuilder() (*DepositTxBuilder, error) {
	parsed, err := abi.JSON(strings.NewReader(executionManagerABIJSON))
	if err != nil {
		return nil, fmt.Errorf("parse deposit manager ABI: %w", err)
	}

	method, ok := parsed.Methods["executeDeposit"]
	if !ok ||
		method.Sig != "executeDeposit(address,uint256,uint256,uint256)" {
		return nil, errors.New("unexpected manager executeDeposit ABI")
	}

	return &DepositTxBuilder{
		manager: common.HexToAddress(fujiExecutionManager),
		abi:     parsed,
	}, nil
}

func (b *DepositTxBuilder) BuildDepositTx(
	request BuildDepositRequest,
	now time.Time,
) (BuiltDepositTransaction, error) {
	var empty BuiltDepositTransaction
	if now.IsZero() || now.Unix() <= 0 {
		return empty, errors.New("current time is required")
	}
	now = now.UTC()

	if !common.IsHexAddress(request.OwnerAddress) {
		return empty, ErrInvalidAddress
	}
	owner := common.HexToAddress(request.OwnerAddress)
	for _, forbidden := range []common.Address{
		{},
		b.manager,
		common.HexToAddress(demoFujiVault),
		common.HexToAddress(demoFujiAsset),
		common.HexToAddress("0x78b5aADB9a8570C7B4387F29119201AF53008b7D"),
	} {
		if owner == forbidden {
			return empty, errors.New("invalid deposit owner")
		}
	}

	metrics := request.Metrics
	position := metrics.Position
	if !common.IsHexAddress(position.WalletAddress) ||
		common.HexToAddress(position.WalletAddress) != owner {
		return empty, errors.New("vault preview wallet mismatch")
	}
	if !common.IsHexAddress(position.VaultAddress) ||
		common.HexToAddress(position.VaultAddress) !=
			common.HexToAddress(demoFujiVault) {
		return empty, ErrUnsupportedVault
	}

	p := position.Provenance
	if p.ChainID != uint64(FujiChainID) {
		return empty, ErrWrongChain
	}
	if p.SourceType != "rpc" || p.Mode != "live" || !p.IsDemo ||
		strings.TrimSpace(p.Source) == "" ||
		p.BlockNumber == nil || *p.BlockNumber == 0 ||
		p.ObservedAt.IsZero() || p.ObservedAt.After(now) {
		return empty, errors.New("invalid DemoVault preview provenance")
	}

	expiresAt := p.ObservedAt.Add(60 * time.Second)
	if !now.Before(expiresAt) {
		return empty, errors.New("deposit preview expired")
	}
	if metrics.BlockTime == nil ||
		metrics.BlockTime.IsZero() ||
		metrics.BlockTime.After(now) ||
		now.Sub(*metrics.BlockTime) > 60*time.Second {
		return empty, errors.New("missing or stale vault block time")
	}

	if request.Deadline <= uint64(now.Unix()) ||
		request.Deadline > uint64(expiresAt.Unix()) {
		return empty, errors.New("invalid deposit deadline")
	}

	asset := Asset{
		ChainID:  uint64(FujiChainID),
		Address:  common.HexToAddress(demoFujiAsset).Hex(),
		Symbol:   "DDEMO",
		Decimals: 18,
	}
	share := Asset{
		ChainID:  uint64(FujiChainID),
		Address:  common.HexToAddress(demoFujiVault).Hex(),
		Symbol:   "dvDDEMO",
		Decimals: 18,
	}

	checkAsset := func(actual, expected Asset) error {
		if actual.ChainID != expected.ChainID {
			return ErrWrongChain
		}
		if !common.IsHexAddress(actual.Address) ||
			common.HexToAddress(actual.Address) !=
				common.HexToAddress(expected.Address) ||
			actual.Decimals != expected.Decimals {
			return errors.New("deposit asset metadata mismatch")
		}
		return nil
	}
	for _, check := range []struct {
		actual Asset
		want   Asset
	}{
		{metrics.DepositAssets.Asset, asset},
		{metrics.PreviewShares.Asset, share},
		{metrics.MaxDeposit.Asset, asset},
	} {
		if err := checkAsset(check.actual, check.want); err != nil {
			return empty, err
		}
	}

	readUint := func(raw string, positive bool) (*big.Int, error) {
		value, err := ParseRawAmount(raw)
		if err != nil {
			return nil, err
		}
		if value == nil || value.Sign() < 0 || value.BitLen() > 256 ||
			(positive && value.Sign() == 0) {
			return nil, errors.New("invalid deposit uint256 amount")
		}
		return value, nil
	}

	assets, err := readUint(metrics.DepositAssets.Raw, true)
	if err != nil {
		return empty, err
	}
	preview, err := readUint(metrics.PreviewShares.Raw, true)
	if err != nil {
		return empty, err
	}
	minimum, err := readUint(request.MinSharesRaw, true)
	if err != nil {
		return empty, err
	}
	maximum, err := readUint(metrics.MaxDeposit.Raw, false)
	if err != nil {
		return empty, err
	}
	if assets.Cmp(maximum) > 0 {
		return empty, errors.New("deposit exceeds maxDeposit")
	}
	if minimum.Cmp(preview) > 0 {
		return empty, errors.New("minimum shares exceeds preview")
	}

	toAmount := func(value *big.Int, token Asset) (TokenAmount, error) {
		formatted, err := FormatTokenAmount(value.String(), token.Decimals)
		if err != nil {
			return TokenAmount{}, err
		}
		return TokenAmount{
			Asset:     token,
			Raw:       value.String(),
			Formatted: formatted,
		}, nil
	}

	assetAmount, err := toAmount(assets, asset)
	if err != nil {
		return empty, err
	}
	previewAmount, err := toAmount(preview, share)
	if err != nil {
		return empty, err
	}
	minimumAmount, err := toAmount(minimum, share)
	if err != nil {
		return empty, err
	}

	data, err := b.abi.Pack(
		"executeDeposit",
		common.HexToAddress(asset.Address),
		assets,
		minimum,
		new(big.Int).SetUint64(request.Deadline),
	)
	if err != nil {
		return empty, fmt.Errorf("encode executeDeposit: %w", err)
	}

	return BuiltDepositTransaction{
		Prepared: PreparedTransaction{
			Action:   "deposit",
			Protocol: "DemoVault ERC-4626",
			Tx: UnsignedTx{
				ChainID: uint64(FujiChainID),
				From:    owner.Hex(),
				To:      b.manager.Hex(),
				Value:   "0",
				Data:    hexutil.Encode(data),
			},
			Provenance: p,
		},
		Summary: DepositTxSummary{
			OwnerAddress:     owner.Hex(),
			RecipientAddress: owner.Hex(),
			ManagerAddress:   b.manager.Hex(),
			VaultAddress:     common.HexToAddress(demoFujiVault).Hex(),
			Assets:           assetAmount,
			PreviewShares:    previewAmount,
			MinShares:        minimumAmount,
			Deadline:         request.Deadline,
			PreviewExpiresAt: expiresAt,
		},
	}, nil
}
