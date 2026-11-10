package avalanche

import (
	"context"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
)

const lfjQuoteABI = `[
	{"type":"function","name":"getTokenX","inputs":[],"outputs":[{"type":"address"}]},
	{"type":"function","name":"getTokenY","inputs":[],"outputs":[{"type":"address"}]},
	{"type":"function","name":"getSwapOut",
	 "inputs":[{"type":"address"},{"type":"uint128"},{"type":"bool"}],
	 "outputs":[{"type":"uint128"},{"type":"uint128"},{"type":"uint128"}]}
]`

type lfjQuoteRPC struct {
	client RPCReader
	abi    abi.ABI
}

func newLFJQuoteRPC(client RPCReader) (*lfjQuoteRPC, error) {
	if client == nil {
		return nil, fmt.Errorf("LFJ quote RPC client is nil")
	}

	contractABI, err := abi.JSON(strings.NewReader(lfjQuoteABI))
	if err != nil {
		return nil, fmt.Errorf("parse LFJ quote ABI: %w", err)
	}

	return &lfjQuoteRPC{client: client, abi: contractABI}, nil
}

func (r *lfjQuoteRPC) call(
	ctx context.Context,
	to common.Address,
	block *big.Int,
	method string,
	args ...any,
) ([]any, error) {
	data, err := r.abi.Pack(method, args...)
	if err != nil {
		return nil, fmt.Errorf("encode LFJ %s: %w", method, err)
	}

	raw, err := r.client.CallContract(ctx, ethereum.CallMsg{
		To:   &to,
		Data: data,
	}, block)
	if err != nil {
		return nil, fmt.Errorf("call LFJ %s: %w", method, err)
	}

	wantBytes := 32 * len(r.abi.Methods[method].Outputs)
	if len(raw) != wantBytes {
		return nil, fmt.Errorf(
			"LFJ %s returned %d bytes; want %d",
			method, len(raw), wantBytes,
		)
	}

	values, err := r.abi.Unpack(method, raw)
	if err != nil {
		return nil, fmt.Errorf("decode LFJ %s: %w", method, err)
	}

	return values, nil
}
