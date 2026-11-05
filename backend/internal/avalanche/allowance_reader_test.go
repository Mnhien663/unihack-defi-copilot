package avalanche

import (
	"context"
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
)

const (
	allowanceTestToken   = "0x0000000000000000000000000000000000000010"
	allowanceTestOwner   = "0x0000000000000000000000000000000000000001"
	allowanceTestSpender = "0x0000000000000000000000000000000000000002"
)

type allowanceRPCStub struct {
	chainID       *big.Int
	chainCalls    int
	blockCalls    int
	balanceCalls  int
	contractCalls int
	blocks        []*big.Int
}

func (s *allowanceRPCStub) ChainID(
	ctx context.Context,
) (*big.Int, error) {
	s.chainCalls++
	return s.chainID, nil
}

func (s *allowanceRPCStub) BlockNumber(
	ctx context.Context,
) (uint64, error) {
	s.blockCalls++
	return 12345, nil
}

func (s *allowanceRPCStub) BalanceAt(
	ctx context.Context,
	account common.Address,
	blockNumber *big.Int,
) (*big.Int, error) {
	s.balanceCalls++
	return nil, errors.New("balance must not be read for allowance")
}

func (s *allowanceRPCStub) CallContract(
	ctx context.Context,
	msg ethereum.CallMsg,
	blockNumber *big.Int,
) ([]byte, error) {
	s.contractCalls++

	if msg.To == nil ||
		*msg.To != common.HexToAddress(allowanceTestToken) {
		return nil, errors.New("unexpected token")
	}
	if blockNumber == nil {
		return nil, errors.New("expected pinned block")
	}

	s.blocks = append(s.blocks, new(big.Int).Set(blockNumber))

	switch string(msg.Data) {
	case string(common.FromHex(
		"0xdd62ed3e" +
			"0000000000000000000000000000000000000000000000000000000000000001" +
			"0000000000000000000000000000000000000000000000000000000000000002",
	)):
		// allowance(owner, spender) = 1.234567 tokens.
		return big.NewInt(1234567).FillBytes(make([]byte, 32)), nil

	case string(common.FromHex("0x313ce567")):
		// decimals() = 6.
		return big.NewInt(6).FillBytes(make([]byte, 32)), nil

	default:
		return nil, errors.New("unexpected contract calldata")
	}
}

func TestAllowanceReaderReturnsPinnedAllowance(t *testing.T) {
	client := &allowanceRPCStub{
		chainID: big.NewInt(FujiChainID),
	}
	reader, err := NewRPCAllowanceReader(
		client,
		[]string{allowanceTestToken},
	)
	if err != nil {
		t.Fatal(err)
	}

	result, err := reader.ReadAllowance(
		context.Background(),
		allowanceTestToken,
		allowanceTestOwner,
		allowanceTestSpender,
	)
	if err != nil {
		t.Fatal(err)
	}

	if result.Allowance.Raw != "1234567" ||
		result.Allowance.Formatted != "1.234567" ||
		result.Allowance.Asset.Decimals != 6 {
		t.Fatalf("unexpected allowance: %+v", result.Allowance)
	}

	if result.OwnerAddress != allowanceTestOwner ||
		result.SpenderAddress != allowanceTestSpender ||
		result.Allowance.Asset.Address != allowanceTestToken {
		t.Fatalf("unexpected addresses: %+v", result)
	}

	if client.contractCalls != 2 || client.balanceCalls != 0 {
		t.Fatalf(
			"contract calls=%d, balance calls=%d",
			client.contractCalls,
			client.balanceCalls,
		)
	}

	for _, block := range client.blocks {
		if block.Uint64() != 12345 {
			t.Fatalf("unexpected contract block: %s", block)
		}
	}

	provenance := result.Provenance
	if provenance.Source != "Avalanche RPC" ||
		provenance.ChainID != 43113 ||
		provenance.BlockNumber == nil ||
		*provenance.BlockNumber != 12345 ||
		provenance.ObservedAt.IsZero() ||
		provenance.IsDemo {
		t.Fatalf("unexpected provenance: %+v", provenance)
	}
}

func TestAllowanceReaderBlocksUnsupportedTokenBeforeRPC(t *testing.T) {
	client := &allowanceRPCStub{
		chainID: big.NewInt(FujiChainID),
	}
	reader, err := NewRPCAllowanceReader(
		client,
		[]string{allowanceTestToken},
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = reader.ReadAllowance(
		context.Background(),
		"0x0000000000000000000000000000000000000099",
		allowanceTestOwner,
		allowanceTestSpender,
	)
	if !errors.Is(err, ErrAllowanceUnsupportedToken) {
		t.Fatalf("expected unsupported token, got %v", err)
	}

	if client.chainCalls != 0 ||
		client.blockCalls != 0 ||
		client.contractCalls != 0 {
		t.Fatalf("unsupported token reached RPC: %+v", client)
	}
}

func TestAllowanceReaderBlocksWrongChainBeforeContract(t *testing.T) {
	client := &allowanceRPCStub{
		chainID: big.NewInt(1),
	}
	reader, err := NewRPCAllowanceReader(
		client,
		[]string{allowanceTestToken},
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = reader.ReadAllowance(
		context.Background(),
		allowanceTestToken,
		allowanceTestOwner,
		allowanceTestSpender,
	)
	if !errors.Is(err, ErrWrongChain) {
		t.Fatalf("expected wrong chain, got %v", err)
	}

	if client.chainCalls != 1 ||
		client.blockCalls != 0 ||
		client.contractCalls != 0 {
		t.Fatalf("wrong chain reached contract: %+v", client)
	}
}

func TestAllowanceReaderRejectsInvalidAddressesBeforeRPC(t *testing.T) {
	tests := []struct {
		name    string
		token   string
		owner   string
		spender string
	}{
		{"token", "bad", allowanceTestOwner, allowanceTestSpender},
		{"owner", allowanceTestToken, "bad", allowanceTestSpender},
		{"spender", allowanceTestToken, allowanceTestOwner, "bad"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &allowanceRPCStub{
				chainID: big.NewInt(FujiChainID),
			}
			reader, err := NewRPCAllowanceReader(
				client,
				[]string{allowanceTestToken},
			)
			if err != nil {
				t.Fatal(err)
			}

			_, err = reader.ReadAllowance(
				context.Background(),
				tt.token,
				tt.owner,
				tt.spender,
			)
			if !errors.Is(err, ErrInvalidAddress) {
				t.Fatalf("expected invalid address, got %v", err)
			}
			if client.chainCalls != 0 || client.contractCalls != 0 {
				t.Fatal("invalid address reached RPC")
			}
		})
	}
}

func TestAllowanceReaderHonorsCancelledContext(t *testing.T) {
	client := &allowanceRPCStub{}
	reader, err := NewRPCAllowanceReader(
		client,
		[]string{allowanceTestToken},
	)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = reader.ReadAllowance(
		ctx,
		allowanceTestToken,
		allowanceTestOwner,
		allowanceTestSpender,
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
	if client.chainCalls != 0 {
		t.Fatal("cancelled request reached RPC")
	}
}
