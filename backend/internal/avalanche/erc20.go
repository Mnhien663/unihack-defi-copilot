package avalanche

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
)

// ham doc balanceOf
const erc20BalanceABI = `[
	{
		"type": "function",
		"name": "balanceOf",
		"stateMutability": "view",
		"inputs": [
			{"name": "owner", "type": "address"}
		],
		"outputs": [
			{"name": "", "type": "uint256"}
		]
	}
]`

// GetERC20Balance returns the owner's raw token balance.
// A nil blockNumber reads the latest state.
// The caller controls cancellation and timeout through ctx.
func GetERC20Balance(
	ctx context.Context,
	client RPCReader,
	tokenAddress string,
	ownerAddress string,
	blockNumber *big.Int,
) (*big.Int, error) {
	if client == nil {
		return nil, fmt.Errorf("Avalanche RPC client is nil")
	}

	tokenAddress = strings.TrimSpace(tokenAddress)
	ownerAddress = strings.TrimSpace(ownerAddress)

	if !common.IsHexAddress(tokenAddress) {
		return nil, fmt.Errorf("%w: token", ErrInvalidAddress)
	}

	if !common.IsHexAddress(ownerAddress) {
		return nil, fmt.Errorf("%w: owner", ErrInvalidAddress)
	}

	token := common.HexToAddress(tokenAddress)
	owner := common.HexToAddress(ownerAddress)

	contractABI, err := abi.JSON(strings.NewReader(erc20BalanceABI))
	if err != nil {
		return nil, fmt.Errorf("parse ERC-20 balance ABI: %w", err)
	}

	data, err := contractABI.Pack("balanceOf", owner)
	if err != nil {
		return nil, fmt.Errorf("encode ERC-20 balanceOf: %w", err)
	}

	result, err := client.CallContract(
		ctx,
		ethereum.CallMsg{
			To:   &token,
			Data: data,
		},
		blockNumber,
	)
	if err != nil {
		return nil, fmt.Errorf("call ERC-20 balanceOf: %w", err)
	}

	// A single uint256 must occupy exactly one 32-byte ABI word.
	if len(result) != 32 {
		return nil, fmt.Errorf(
			"invalid ERC-20 balanceOf response length: got %d, want 32",
			len(result),
		)
	}

	values, err := contractABI.Unpack("balanceOf", result)
	if err != nil {
		return nil, fmt.Errorf("decode ERC-20 balanceOf: %w", err)
	}

	if len(values) != 1 {
		return nil, fmt.Errorf("invalid ERC-20 balanceOf output count")
	}

	balance, ok := values[0].(*big.Int)
	if !ok || balance == nil {
		return nil, fmt.Errorf("invalid ERC-20 balanceOf output type")
	}

	return balance, nil
}

// ham doc allowance
const erc20AllowanceABI = `[
	{
		"type": "function",
		"name": "allowance",
		"stateMutability": "view",
		"inputs": [
			{"name": "owner", "type": "address"},
			{"name": "spender", "type": "address"}
		],
		"outputs": [
			{"name": "", "type": "uint256"}
		]
	}
]`

// GetERC20Allowance returns the raw amount the spender may use.
// A nil blockNumber reads the latest state.
// The caller controls cancellation and timeout through ctx.
func GetERC20Allowance(
	ctx context.Context,
	client RPCReader,
	tokenAddress string,
	ownerAddress string,
	spenderAddress string,
	blockNumber *big.Int,
) (*big.Int, error) {
	if client == nil {
		return nil, fmt.Errorf("Avalanche RPC client is nil")
	}

	tokenAddress = strings.TrimSpace(tokenAddress)
	ownerAddress = strings.TrimSpace(ownerAddress)
	spenderAddress = strings.TrimSpace(spenderAddress)

	if !common.IsHexAddress(tokenAddress) {
		return nil, fmt.Errorf("%w: token", ErrInvalidAddress)
	}

	if !common.IsHexAddress(ownerAddress) {
		return nil, fmt.Errorf("%w: owner", ErrInvalidAddress)
	}

	if !common.IsHexAddress(spenderAddress) {
		return nil, fmt.Errorf("%w: spender", ErrInvalidAddress)
	}

	token := common.HexToAddress(tokenAddress)
	owner := common.HexToAddress(ownerAddress)
	spender := common.HexToAddress(spenderAddress)

	contractABI, err := abi.JSON(strings.NewReader(erc20AllowanceABI))
	if err != nil {
		return nil, fmt.Errorf("parse ERC-20 allowance ABI: %w", err)
	}

	data, err := contractABI.Pack("allowance", owner, spender)
	if err != nil {
		return nil, fmt.Errorf("encode ERC-20 allowance: %w", err)
	}

	result, err := client.CallContract(
		ctx,
		ethereum.CallMsg{
			To:   &token,
			Data: data,
		},
		blockNumber,
	)
	if err != nil {
		return nil, fmt.Errorf("call ERC-20 allowance: %w", err)
	}

	if len(result) != 32 {
		return nil, fmt.Errorf(
			"invalid ERC-20 allowance response length: got %d, want 32",
			len(result),
		)
	}

	values, err := contractABI.Unpack("allowance", result)
	if err != nil {
		return nil, fmt.Errorf("decode ERC-20 allowance: %w", err)
	}

	if len(values) != 1 {
		return nil, fmt.Errorf("invalid ERC-20 allowance output count")
	}

	allowance, ok := values[0].(*big.Int)
	if !ok || allowance == nil {
		return nil, fmt.Errorf("invalid ERC-20 allowance output type")
	}

	return allowance, nil
}

// ham doc decimals
const erc20DecimalsABI = `[
	{
		"type": "function",
		"name": "decimals",
		"stateMutability": "view",
		"inputs": [],
		"outputs": [
			{"name": "", "type": "uint8"}
		]
	}
]`

// ErrTokenMetadataUnavailable indicates that token metadata could not be read.
var ErrTokenMetadataUnavailable = errors.New("token metadata unavailable")

// GetERC20Decimals reads the token's decimals without guessing a default.
// A nil blockNumber reads the latest state.
// The caller controls cancellation and timeout through ctx.
func GetERC20Decimals(
	ctx context.Context,
	client RPCReader,
	tokenAddress string,
	blockNumber *big.Int,
) (uint8, error) {
	if client == nil {
		return 0, fmt.Errorf("Avalanche RPC client is nil")
	}

	tokenAddress = strings.TrimSpace(tokenAddress)
	if !common.IsHexAddress(tokenAddress) {
		return 0, fmt.Errorf("%w: token", ErrInvalidAddress)
	}

	token := common.HexToAddress(tokenAddress)

	contractABI, err := abi.JSON(strings.NewReader(erc20DecimalsABI))
	if err != nil {
		return 0, fmt.Errorf("parse ERC-20 decimals ABI: %w", err)
	}

	data, err := contractABI.Pack("decimals")
	if err != nil {
		return 0, fmt.Errorf("encode ERC-20 decimals: %w", err)
	}

	result, err := client.CallContract(
		ctx,
		ethereum.CallMsg{
			To:   &token,
			Data: data,
		},
		blockNumber,
	)
	if err != nil {
		return 0, errors.Join(
			ErrTokenMetadataUnavailable,
			fmt.Errorf("call ERC-20 decimals: %w", err),
		)
	}

	if len(result) != 32 {
		return 0, fmt.Errorf(
			"%w: invalid decimals response length: got %d, want 32",
			ErrTokenMetadataUnavailable,
			len(result),
		)
	}

	// uint8 occupies the last byte; preceding bytes must be zero.
	for _, b := range result[:31] {
		if b != 0 {
			return 0, fmt.Errorf(
				"%w: decimals response exceeds uint8",
				ErrTokenMetadataUnavailable,
			)
		}
	}

	values, err := contractABI.Unpack("decimals", result)
	if err != nil {
		return 0, errors.Join(
			ErrTokenMetadataUnavailable,
			fmt.Errorf("decode ERC-20 decimals: %w", err),
		)
	}

	if len(values) != 1 {
		return 0, fmt.Errorf(
			"%w: invalid decimals output count",
			ErrTokenMetadataUnavailable,
		)
	}

	decimals, ok := values[0].(uint8)
	if !ok {
		return 0, fmt.Errorf(
			"%w: invalid decimals output type",
			ErrTokenMetadataUnavailable,
		)
	}

	return decimals, nil
}
