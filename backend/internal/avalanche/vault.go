package avalanche

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

const (
	demoFujiVault = "0x896C05deae3d6e35021B12DD361401736Bb8aAda"
	demoFujiAsset = "0xcCCb83Fbe349fAebDE74Ce633D25B282E6FDd21A"
)

// VaultReadRequest uses the wallet as the deposit share receiver.
type VaultReadRequest struct {
	VaultAddress     string `json:"vault_address"`
	WalletAddress    string `json:"wallet_address"`
	DepositAssetsRaw string `json:"deposit_assets_raw"`
}

// VaultMetricsResult supplements the existing wallet-position DTO.
// Preview is an estimate, not a guarantee of executable deposit output.
type VaultMetricsResult struct {
	Position      VaultPositionResult `json:"position"`
	TotalAssets   TokenAmount         `json:"total_assets"`
	TotalShares   TokenAmount         `json:"total_shares"`
	BlockTime     *time.Time          `json:"block_time,omitempty"`
	DepositAssets TokenAmount         `json:"deposit_assets"`
	PreviewShares TokenAmount         `json:"preview_shares"`
	MaxDeposit    TokenAmount         `json:"max_deposit"`
	MaxWithdraw   TokenAmount         `json:"max_withdraw"`
	MaxRedeem     TokenAmount         `json:"max_redeem"`
	APY           *string             `json:"apy"`
	TVLUSD        *string             `json:"tvl_usd"`
}

type VaultMetricsReader interface {
	ReadVault(
		ctx context.Context,
		request VaultReadRequest,
	) (VaultMetricsResult, error)
}

var ErrUnsupportedVault = errors.New("vault is not configured")

// RPCVaultReader supports the configured DemoVault only.
type RPCVaultReader struct {
	client RPCReader
	vault  common.Address
	asset  common.Address
	abi    abi.ABI
}

func NewRPCVaultReader(client RPCReader) (*RPCVaultReader, error) {
	if client == nil {
		return nil, errors.New("vault RPC client is nil")
	}

	parsed, err := abi.JSON(strings.NewReader(vaultReadABIJSON))
	if err != nil {
		return nil, fmt.Errorf("parse vault ABI: %w", err)
	}

	return &RPCVaultReader{
		client: client,
		vault:  common.HexToAddress(demoFujiVault),
		asset:  common.HexToAddress(demoFujiAsset),
		abi:    parsed,
	}, nil
}

const vaultReadABIJSON = `[
	{
		"type":"function","name":"asset","stateMutability":"view",
		"inputs":[],"outputs":[{"type":"address"}]
	},
	{
		"type":"function","name":"totalAssets","stateMutability":"view",
		"inputs":[],"outputs":[{"type":"uint256"}]
	},
	{
		"type":"function","name":"totalSupply","stateMutability":"view",
		"inputs":[],"outputs":[{"type":"uint256"}]
	},
	{
		"type":"function","name":"balanceOf","stateMutability":"view",
		"inputs":[{"name":"account","type":"address"}],
		"outputs":[{"type":"uint256"}]
	},
	{
		"type":"function","name":"convertToAssets","stateMutability":"view",
		"inputs":[{"name":"shares","type":"uint256"}],
		"outputs":[{"type":"uint256"}]
	},
	{
		"type":"function","name":"previewDeposit","stateMutability":"view",
		"inputs":[{"name":"assets","type":"uint256"}],
		"outputs":[{"type":"uint256"}]
	},
	{
		"type":"function","name":"maxDeposit","stateMutability":"view",
		"inputs":[{"name":"receiver","type":"address"}],
		"outputs":[{"type":"uint256"}]
	},
	{
		"type":"function","name":"maxWithdraw","stateMutability":"view",
		"inputs":[{"name":"owner","type":"address"}],
		"outputs":[{"type":"uint256"}]
	},
	{
		"type":"function","name":"maxRedeem","stateMutability":"view",
		"inputs":[{"name":"owner","type":"address"}],
		"outputs":[{"type":"uint256"}]
	}
]`

func (r *RPCVaultReader) callVaultMethod(
	ctx context.Context,
	block *big.Int,
	method string,
	args ...interface{},
) ([]interface{}, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if block == nil {
		return nil, errors.New("vault read requires a pinned block")
	}

	data, err := r.abi.Pack(method, args...)
	if err != nil {
		return nil, fmt.Errorf("encode vault %s: %w", method, err)
	}

	raw, err := r.client.CallContract(
		ctx,
		ethereum.CallMsg{To: &r.vault, Data: data},
		block,
	)
	if err != nil {
		return nil, fmt.Errorf("call vault %s: %w", method, err)
	}
	if len(raw) != 32 {
		return nil, fmt.Errorf(
			"vault %s returned %d bytes; expected 32",
			method, len(raw),
		)
	}

	values, err := r.abi.Unpack(method, raw)
	if err != nil {
		return nil, fmt.Errorf("decode vault %s: %w", method, err)
	}
	return values, nil
}

func (r *RPCVaultReader) ReadVault(
	ctx context.Context,
	request VaultReadRequest,
) (VaultMetricsResult, error) {
	if err := ctx.Err(); err != nil {
		return VaultMetricsResult{}, err
	}

	vaultAddress := strings.TrimSpace(request.VaultAddress)
	walletAddress := strings.TrimSpace(request.WalletAddress)

	if !common.IsHexAddress(vaultAddress) ||
		!common.IsHexAddress(walletAddress) {
		return VaultMetricsResult{}, ErrInvalidAddress
	}

	if common.HexToAddress(vaultAddress) != r.vault {
		return VaultMetricsResult{}, ErrUnsupportedVault
	}

	wallet := common.HexToAddress(walletAddress)
	if wallet == (common.Address{}) {
		return VaultMetricsResult{}, fmt.Errorf(
			"%w: vault wallet cannot be zero",
			ErrInvalidAddress,
		)
	}

	deposit, err := ParseRawAmount(request.DepositAssetsRaw)
	if err != nil {
		return VaultMetricsResult{}, fmt.Errorf(
			"parse vault preview amount: %w", err,
		)
	}

	readCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	chainID, err := r.client.ChainID(readCtx)
	if err != nil {
		return VaultMetricsResult{}, fmt.Errorf(
			"verify vault chain: %w", err,
		)
	}
	if chainID == nil || chainID.Cmp(big.NewInt(FujiChainID)) != 0 {
		return VaultMetricsResult{}, ErrWrongChain
	}

	block, err := r.client.BlockNumber(readCtx)
	if err != nil {
		return VaultMetricsResult{}, fmt.Errorf(
			"get vault block: %w", err,
		)
	}
	blockNumber := new(big.Int).SetUint64(block)

	assetValues, err := r.callVaultMethod(
		readCtx, blockNumber, "asset",
	)
	if err != nil {
		return VaultMetricsResult{}, err
	}
	underlying, ok := assetValues[0].(common.Address)
	if !ok || underlying != r.asset {
		return VaultMetricsResult{}, errors.New(
			"vault underlying differs from configured DemoToken",
		)
	}

	assetDecimals, err := GetERC20Decimals(
		readCtx, r.client, r.asset.Hex(), blockNumber,
	)
	if err != nil {
		return VaultMetricsResult{}, fmt.Errorf(
			"read vault underlying decimals: %w", err,
		)
	}
	shareDecimals, err := GetERC20Decimals(
		readCtx, r.client, r.vault.Hex(), blockNumber,
	)
	if err != nil {
		return VaultMetricsResult{}, fmt.Errorf(
			"read vault share decimals: %w", err,
		)
	}

	readUint := func(method string, args ...interface{}) (*big.Int, error) {
		values, err := r.callVaultMethod(
			readCtx, blockNumber, method, args...,
		)
		if err != nil {
			return nil, err
		}

		number, ok := values[0].(*big.Int)
		if !ok || number == nil ||
			number.Sign() < 0 || number.BitLen() > 256 {
			return nil, fmt.Errorf(
				"vault %s returned invalid uint256", method,
			)
		}
		return number, nil
	}

	totalAssets, err := readUint("totalAssets")
	if err != nil {
		return VaultMetricsResult{}, err
	}
	totalShares, err := readUint("totalSupply")
	if err != nil {
		return VaultMetricsResult{}, err
	}
	walletShares, err := readUint("balanceOf", wallet)
	if err != nil {
		return VaultMetricsResult{}, err
	}
	walletAssets, err := readUint("convertToAssets", walletShares)
	if err != nil {
		return VaultMetricsResult{}, err
	}
	previewShares, err := readUint("previewDeposit", deposit)
	if err != nil {
		return VaultMetricsResult{}, err
	}
	maxDeposit, err := readUint("maxDeposit", wallet)
	if err != nil {
		return VaultMetricsResult{}, err
	}
	maxWithdraw, err := readUint("maxWithdraw", wallet)
	if err != nil {
		return VaultMetricsResult{}, err
	}
	maxRedeem, err := readUint("maxRedeem", wallet)
	if err != nil {
		return VaultMetricsResult{}, err
	}

	asset := Asset{
		ChainID:  uint64(FujiChainID),
		Address:  r.asset.Hex(),
		Symbol:   "DDEMO",
		Decimals: assetDecimals,
	}
	share := Asset{
		ChainID:  uint64(FujiChainID),
		Address:  r.vault.Hex(),
		Symbol:   "dvDDEMO",
		Decimals: shareDecimals,
	}

	toAmount := func(value *big.Int, token Asset) (TokenAmount, error) {
		formatted, err := FormatTokenAmount(
			value.String(), token.Decimals,
		)
		if err != nil {
			return TokenAmount{}, err
		}
		return TokenAmount{
			Asset:     token,
			Raw:       value.String(),
			Formatted: formatted,
		}, nil
	}

	specs := []struct {
		value *big.Int
		asset Asset
	}{
		{walletShares, share},
		{walletAssets, asset},
		{totalAssets, asset},
		{totalShares, share},
		{deposit, asset},
		{previewShares, share},
		{maxDeposit, asset},
		{maxWithdraw, asset},
		{maxRedeem, share},
	}

	amounts := make([]TokenAmount, len(specs))
	for i, spec := range specs {
		amounts[i], err = toAmount(spec.value, spec.asset)
		if err != nil {
			return VaultMetricsResult{}, err
		}
	}

	if err := readCtx.Err(); err != nil {
		return VaultMetricsResult{}, err
	}

	blockTime := readVaultBlockTime(readCtx, r.client, blockNumber)
	if err := readCtx.Err(); err != nil {
		return VaultMetricsResult{}, err
	}

	return VaultMetricsResult{
		Position: VaultPositionResult{
			Protocol:      "DemoVault ERC-4626",
			VaultAddress:  r.vault.Hex(),
			WalletAddress: wallet.Hex(),
			Shares:        amounts[0],
			Assets:        amounts[1],
			Provenance: Provenance{
				Source:      "DemoVault Fuji",
				SourceType:  "rpc",
				ChainID:     uint64(FujiChainID),
				BlockNumber: &block,
				ObservedAt:  time.Now().UTC(),
				Mode:        "live",
				IsDemo:      true,
			},
		},
		TotalAssets:   amounts[2],
		TotalShares:   amounts[3],
		BlockTime:     blockTime,
		DepositAssets: amounts[4],
		PreviewShares: amounts[5],
		MaxDeposit:    amounts[6],
		MaxWithdraw:   amounts[7],
		MaxRedeem:     amounts[8],
		APY:           nil,
		TVLUSD:        nil,
	}, nil
}

func (r *RPCVaultReader) ReadVaultPosition(
	ctx context.Context,
	vaultAddress string,
	walletAddress string,
) (VaultPositionResult, error) {
	result, err := r.ReadVault(ctx, VaultReadRequest{
		VaultAddress:     vaultAddress,
		WalletAddress:    walletAddress,
		DepositAssetsRaw: "0",
	})
	if err != nil {
		return VaultPositionResult{}, err
	}
	return result.Position, nil
}

type vaultHeaderReader interface {
	HeaderByNumber(context.Context, *big.Int) (*types.Header, error)
}

func readVaultBlockTime(
	ctx context.Context,
	client RPCReader,
	block *big.Int,
) *time.Time {
	reader, ok := client.(vaultHeaderReader)
	if !ok || block == nil {
		return nil
	}

	header, err := reader.HeaderByNumber(ctx, block)
	if err != nil || header == nil || header.Number == nil ||
		header.Number.Cmp(block) != 0 ||
		header.Time == 0 || header.Time > uint64(1<<63-1) {
		return nil
	}

	sourceTime := time.Unix(int64(header.Time), 0).UTC()
	return &sourceTime
}

var _ VaultMetricsReader = (*RPCVaultReader)(nil)
var _ VaultPositionReader = (*RPCVaultReader)(nil)
