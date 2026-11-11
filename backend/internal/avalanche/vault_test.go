package avalanche

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
)

const vaultTestWallet = "0x0000000000000000000000000000000000000001"

type vaultRPCStub struct {
	reader        *RPCVaultReader
	contractCalls int
	previewInput  *big.Int
	previewOutput *big.Int
	chainID       *big.Int
	empty         bool
	wrongAsset    bool
	failMethod    string
	callErr       error
	shortMethod   string
}

func (s *vaultRPCStub) ChainID(context.Context) (*big.Int, error) {
	if s.chainID != nil {
		return s.chainID, nil
	}
	return big.NewInt(FujiChainID), nil
}

func (s *vaultRPCStub) BlockNumber(context.Context) (uint64, error) {
	return 12345, nil
}

func (s *vaultRPCStub) BalanceAt(
	context.Context,
	common.Address,
	*big.Int,
) (*big.Int, error) {
	return nil, errors.New("vault reader must not read native balance")
}

func (s *vaultRPCStub) CallContract(
	ctx context.Context,
	msg ethereum.CallMsg,
	block *big.Int,
) ([]byte, error) {
	s.contractCalls++

	if block == nil || block.Cmp(big.NewInt(12345)) != 0 {
		return nil, errors.New("expected pinned block")
	}
	if msg.To == nil || len(msg.Data) < 4 {
		return nil, errors.New("invalid contract call")
	}

	r := s.reader

	if string(msg.Data[:4]) == string(common.FromHex("0x313ce567")) {
		switch *msg.To {
		case r.asset:
			return big.NewInt(18).FillBytes(make([]byte, 32)), nil
		case r.vault:
			// Deliberately different to verify separate share units.
			return big.NewInt(6).FillBytes(make([]byte, 32)), nil
		default:
			return nil, errors.New("unexpected decimals target")
		}
	}

	if *msg.To != r.vault {
		return nil, errors.New("vault method sent to wrong contract")
	}

	method, err := r.abi.MethodById(msg.Data[:4])
	if err != nil {
		return nil, err
	}
	args, err := method.Inputs.Unpack(msg.Data[4:])
	if err != nil {
		return nil, err
	}
	if method.Name == s.failMethod {
		return nil, s.callErr
	}
	if method.Name == s.shortMethod {
		return []byte{0x01}, nil
	}
	if method.Name == "asset" && s.wrongAsset {
		return method.Outputs.Pack(common.HexToAddress(
			"0x0000000000000000000000000000000000000099",
		))
	}

	wallet := common.HexToAddress(vaultTestWallet)
	for _, name := range []string{
		"balanceOf", "maxDeposit", "maxWithdraw", "maxRedeem",
	} {
		if method.Name == name && args[0].(common.Address) != wallet {
			return nil, errors.New("wrong owner or receiver")
		}
	}

	if s.empty {
		switch method.Name {
		case "totalAssets", "totalSupply", "balanceOf",
			"convertToAssets", "maxWithdraw", "maxRedeem":
			return method.Outputs.Pack(big.NewInt(0))
		}
	}
	oneAsset := new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)

	switch method.Name {
	case "asset":
		return method.Outputs.Pack(r.asset)

	case "totalAssets":
		return method.Outputs.Pack(
			new(big.Int).Mul(big.NewInt(10), oneAsset),
		)

	case "totalSupply":
		return method.Outputs.Pack(big.NewInt(10_000_000))

	case "balanceOf":
		return method.Outputs.Pack(big.NewInt(2_000_000))

	case "convertToAssets":
		if args[0].(*big.Int).Cmp(big.NewInt(2_000_000)) != 0 {
			return nil, errors.New("wrong shares passed for conversion")
		}
		return method.Outputs.Pack(
			new(big.Int).Mul(big.NewInt(2), oneAsset),
		)

	case "previewDeposit":
		s.previewInput = new(big.Int).Set(args[0].(*big.Int))
		if s.previewOutput != nil {
			return method.Outputs.Pack(s.previewOutput)
		}
		if s.previewInput.Sign() == 0 {
			return method.Outputs.Pack(big.NewInt(0))
		}
		return method.Outputs.Pack(big.NewInt(1_000_000))

	case "maxDeposit":
		max := new(big.Int).Sub(
			new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1),
		)
		return method.Outputs.Pack(max)

	case "maxWithdraw":
		return method.Outputs.Pack(oneAsset)

	case "maxRedeem":
		return method.Outputs.Pack(big.NewInt(1_000_000))

	default:
		return nil, errors.New("unexpected vault method")
	}
}

func newVaultTestReader(t *testing.T) (*RPCVaultReader, *vaultRPCStub) {
	t.Helper()

	client := &vaultRPCStub{}
	reader, err := NewRPCVaultReader(client)
	if err != nil {
		t.Fatal(err)
	}
	client.reader = reader
	return reader, client
}

func vaultTestRequest() VaultReadRequest {
	return VaultReadRequest{
		VaultAddress:     demoFujiVault,
		WalletAddress:    vaultTestWallet,
		DepositAssetsRaw: "1000000000000000000",
	}
}

func TestVaultReaderPreservesUnitsAndLimits(t *testing.T) {
	reader, client := newVaultTestReader(t)

	result, err := reader.ReadVault(
		context.Background(), vaultTestRequest(),
	)
	if err != nil {
		t.Fatal(err)
	}

	if client.contractCalls != 11 {
		t.Fatalf("contract calls=%d; want 11", client.contractCalls)
	}
	if client.previewInput.String() != "1000000000000000000" {
		t.Fatal("preview input lost precision")
	}

	if result.Position.Shares.Asset.Decimals != 6 ||
		result.Position.Assets.Asset.Decimals != 18 ||
		result.Position.Shares.Formatted != "2" ||
		result.Position.Assets.Formatted != "2" ||
		result.TotalAssets.Formatted != "10" ||
		result.TotalShares.Formatted != "10" ||
		result.PreviewShares.Formatted != "1" {
		t.Fatalf("unexpected units or amounts: %+v", result)
	}

	// Conversion is not an immediate withdrawal limit.
	if result.MaxWithdraw.Formatted != "1" ||
		result.MaxRedeem.Formatted != "1" {
		t.Fatal("withdrawal limits were replaced by conversion amounts")
	}

	max := new(big.Int).Sub(
		new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1),
	).String()
	if result.MaxDeposit.Raw != max {
		t.Fatal("maximum deposit lost precision")
	}

	p := result.Position.Provenance
	if p.BlockNumber == nil || *p.BlockNumber != 12345 ||
		p.ChainID != 43113 || p.SourceType != "rpc" ||
		p.Mode != "live" || !p.IsDemo || p.ObservedAt.IsZero() {
		t.Fatalf("unexpected provenance: %+v", p)
	}

	body, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"apy":null`) ||
		!strings.Contains(string(body), `"tvl_usd":null`) {
		t.Fatalf("unknown yield/value must be null: %s", body)
	}
}

func TestVaultPositionReaderUsesZeroPreview(t *testing.T) {
	reader, client := newVaultTestReader(t)

	result, err := reader.ReadVaultPosition(
		context.Background(), demoFujiVault, vaultTestWallet,
	)
	if err != nil {
		t.Fatal(err)
	}
	if client.previewInput == nil || client.previewInput.Sign() != 0 {
		t.Fatal("position-only read must use zero preview input")
	}
	if result.Shares.Raw != "2000000" {
		t.Fatal("unexpected wallet shares")
	}
}

func TestVaultReaderAcceptsEmptyVault(t *testing.T) {
	reader, client := newVaultTestReader(t)
	client.empty = true

	result, err := reader.ReadVault(
		context.Background(), vaultTestRequest(),
	)
	if err != nil {
		t.Fatal(err)
	}

	for _, amount := range []TokenAmount{
		result.TotalAssets,
		result.TotalShares,
		result.Position.Shares,
		result.Position.Assets,
		result.MaxWithdraw,
		result.MaxRedeem,
	} {
		if amount.Raw != "0" || amount.Formatted != "0" {
			t.Fatalf("expected valid zero amount, got %+v", amount)
		}
	}
}

func TestVaultReaderPreservesZeroPreview(t *testing.T) {
	reader, client := newVaultTestReader(t)
	client.previewOutput = big.NewInt(0)

	result, err := reader.ReadVault(
		context.Background(), vaultTestRequest(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.PreviewShares.Raw != "0" {
		t.Fatal("reader must preserve a zero-share preview")
	}
}

func TestVaultReaderRejectsInvalidRequests(t *testing.T) {
	tests := []struct {
		name   string
		change func(*VaultReadRequest)
	}{
		{"invalid wallet", func(r *VaultReadRequest) {
			r.WalletAddress = "bad"
		}},
		{"zero wallet", func(r *VaultReadRequest) {
			r.WalletAddress = common.Address{}.Hex()
		}},
		{"invalid vault", func(r *VaultReadRequest) {
			r.VaultAddress = "bad"
		}},
		{"unsupported vault", func(r *VaultReadRequest) {
			r.VaultAddress =
				"0x0000000000000000000000000000000000000099"
		}},
		{"negative preview amount", func(r *VaultReadRequest) {
			r.DepositAssetsRaw = "-1"
		}},
		{"fractional raw amount", func(r *VaultReadRequest) {
			r.DepositAssetsRaw = "1.5"
		}},
		{"uint256 overflow", func(r *VaultReadRequest) {
			r.DepositAssetsRaw = new(big.Int).Lsh(
				big.NewInt(1), 256,
			).String()
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader, client := newVaultTestReader(t)
			request := vaultTestRequest()
			tt.change(&request)

			_, err := reader.ReadVault(context.Background(), request)
			if err == nil {
				t.Fatal("expected request to be rejected")
			}
			if client.contractCalls != 0 {
				t.Fatal("invalid request reached contract calls")
			}
		})
	}
}

func TestVaultReaderRejectsWrongChainAndAsset(t *testing.T) {
	t.Run("wrong chain", func(t *testing.T) {
		reader, client := newVaultTestReader(t)
		client.chainID = big.NewInt(43114)

		_, err := reader.ReadVault(
			context.Background(), vaultTestRequest(),
		)
		if !errors.Is(err, ErrWrongChain) {
			t.Fatalf("want ErrWrongChain, got %v", err)
		}
		if client.contractCalls != 0 {
			t.Fatal("wrong chain reached contract calls")
		}
	})

	t.Run("wrong underlying", func(t *testing.T) {
		reader, client := newVaultTestReader(t)
		client.wrongAsset = true

		_, err := reader.ReadVault(
			context.Background(), vaultTestRequest(),
		)
		if err == nil {
			t.Fatal("expected underlying mismatch error")
		}
		if client.contractCalls != 1 {
			t.Fatal("underlying mismatch must stop further reads")
		}
	})
}

func TestVaultReaderPreservesRPCError(t *testing.T) {
	reader, client := newVaultTestReader(t)
	client.failMethod = "previewDeposit"
	client.callErr = context.DeadlineExceeded

	result, err := reader.ReadVault(
		context.Background(), vaultTestRequest(),
	)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want preserved deadline error, got %v", err)
	}
	if result.Position.WalletAddress != "" {
		t.Fatal("RPC failure must not produce partial metrics")
	}
}

func TestVaultReaderRejectsShortResponse(t *testing.T) {
	reader, client := newVaultTestReader(t)
	client.shortMethod = "totalAssets"

	_, err := reader.ReadVault(
		context.Background(), vaultTestRequest(),
	)
	if err == nil {
		t.Fatal("expected short ABI response error")
	}
}

func TestVaultReaderHonorsCancellation(t *testing.T) {
	reader, client := newVaultTestReader(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := reader.ReadVault(ctx, vaultTestRequest())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
	if client.contractCalls != 0 {
		t.Fatal("cancelled request reached contract calls")
	}
}
