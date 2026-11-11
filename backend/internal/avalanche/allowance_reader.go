package avalanche

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

var ErrAllowanceUnsupportedToken = errors.New(
	"allowance token is not allowlisted",
)

type AllowanceResult struct {
	OwnerAddress   string      `json:"owner_address"`
	SpenderAddress string      `json:"spender_address"`
	Allowance      TokenAmount `json:"allowance"`
	Provenance     Provenance  `json:"provenance"`
}

type AllowanceReader interface {
	ReadAllowance(
		ctx context.Context,
		tokenAddress string,
		ownerAddress string,
		spenderAddress string,
	) (AllowanceResult, error)
}

type RPCAllowanceReader struct {
	client        RPCReader
	allowedTokens map[common.Address]struct{}
}

func NewRPCAllowanceReader(
	client RPCReader,
	tokenAddresses []string,
) (*RPCAllowanceReader, error) {
	if client == nil {
		return nil, errors.New("allowance RPC client is nil")
	}

	if len(tokenAddresses) == 0 {
		return nil, errors.New("allowance token allowlist is empty")
	}

	allowed := make(map[common.Address]struct{}, len(tokenAddresses))

	for _, value := range tokenAddresses {
		value = strings.TrimSpace(value)
		if !common.IsHexAddress(value) {
			return nil, fmt.Errorf(
				"%w: allowlisted token",
				ErrInvalidAddress,
			)
		}

		address := common.HexToAddress(value)
		if address == (common.Address{}) {
			return nil, errors.New("allowlisted token cannot be zero address")
		}

		allowed[address] = struct{}{}
	}

	return &RPCAllowanceReader{
		client:        client,
		allowedTokens: allowed,
	}, nil
}

func (r *RPCAllowanceReader) ReadAllowance(
	ctx context.Context,
	tokenAddress string,
	ownerAddress string,
	spenderAddress string,
) (AllowanceResult, error) {
	if err := ctx.Err(); err != nil {
		return AllowanceResult{}, err
	}

	tokenAddress = strings.TrimSpace(tokenAddress)
	ownerAddress = strings.TrimSpace(ownerAddress)
	spenderAddress = strings.TrimSpace(spenderAddress)

	for _, item := range []struct {
		name  string
		value string
	}{
		{"token", tokenAddress},
		{"owner", ownerAddress},
		{"spender", spenderAddress},
	} {
		if !common.IsHexAddress(item.value) {
			return AllowanceResult{}, fmt.Errorf(
				"%w: %s",
				ErrInvalidAddress,
				item.name,
			)
		}
	}

	token := common.HexToAddress(tokenAddress)
	if _, allowed := r.allowedTokens[token]; !allowed {
		return AllowanceResult{}, ErrAllowanceUnsupportedToken
	}

	readCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	chainID, err := r.client.ChainID(readCtx)
	if err != nil {
		return AllowanceResult{}, fmt.Errorf(
			"verify allowance RPC chain: %w",
			err,
		)
	}
	if chainID == nil || chainID.Cmp(big.NewInt(FujiChainID)) != 0 {
		return AllowanceResult{}, fmt.Errorf(
			"%w: allowance requires Fuji chain ID 43113",
			ErrWrongChain,
		)
	}

	block, err := r.client.BlockNumber(readCtx)
	if err != nil {
		return AllowanceResult{}, fmt.Errorf(
			"get allowance block: %w",
			err,
		)
	}

	// Read allowance and decimals at the same block.
	blockNumber := new(big.Int).SetUint64(block)

	raw, err := GetERC20Allowance(
		readCtx,
		r.client,
		token.Hex(),
		ownerAddress,
		spenderAddress,
		blockNumber,
	)
	if err != nil {
		return AllowanceResult{}, fmt.Errorf(
			"read token allowance: %w",
			err,
		)
	}

	if raw == nil || raw.Sign() < 0 || raw.BitLen() > 256 {
		return AllowanceResult{}, errors.New(
			"allowance is outside uint256 bounds",
		)
	}

	decimals, err := GetERC20Decimals(
		readCtx,
		r.client,
		token.Hex(),
		blockNumber,
	)
	if err != nil {
		return AllowanceResult{}, fmt.Errorf(
			"read allowance token decimals: %w",
			err,
		)
	}

	formatted, err := FormatTokenAmount(raw.String(), decimals)
	if err != nil {
		return AllowanceResult{}, fmt.Errorf(
			"format allowance: %w",
			err,
		)
	}

	return AllowanceResult{
		OwnerAddress:   common.HexToAddress(ownerAddress).Hex(),
		SpenderAddress: common.HexToAddress(spenderAddress).Hex(),
		Allowance: TokenAmount{
			Asset: Asset{
				ChainID:  uint64(FujiChainID),
				Address:  token.Hex(),
				Decimals: decimals,
				// Symbol is intentionally not guessed.
			},
			Raw:       raw.String(),
			Formatted: formatted,
		},
		Provenance: Provenance{
			Source:      "Avalanche RPC",
			SourceType:  "rpc",
			ChainID:     uint64(FujiChainID),
			BlockNumber: &block,
			ObservedAt:  time.Now().UTC(),
			Mode:        "live",
			IsDemo:      false,
		},
	}, nil
}

var _ AllowanceReader = (*RPCAllowanceReader)(nil)
