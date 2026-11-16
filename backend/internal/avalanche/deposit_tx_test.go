package avalanche

import (
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

func depositBuilderFixture() (BuildDepositRequest, time.Time) {
	now := time.Date(2026, 11, 12, 13, 0, 0, 0, time.UTC)
	block := uint64(60946687)
	blockTime := now.Add(-2 * time.Second)

	asset := Asset{
		ChainID:  uint64(FujiChainID),
		Address:  demoFujiAsset,
		Symbol:   "DDEMO",
		Decimals: 18,
	}
	share := Asset{
		ChainID:  uint64(FujiChainID),
		Address:  demoFujiVault,
		Symbol:   "dvDDEMO",
		Decimals: 18,
	}
	owner := "0x37E146a4141C8fd82f0c8c5Bb8035930fD0ea588"

	return BuildDepositRequest{
		OwnerAddress: owner,
		Metrics: VaultMetricsResult{
			Position: VaultPositionResult{
				Protocol:      "DemoVault ERC-4626",
				VaultAddress:  demoFujiVault,
				WalletAddress: owner,
				Provenance: Provenance{
					Source:      "DemoVault Fuji",
					SourceType:  "rpc",
					ChainID:     uint64(FujiChainID),
					BlockNumber: &block,
					ObservedAt:  now.Add(-time.Second),
					Mode:        "live",
					IsDemo:      true,
				},
			},
			BlockTime: &blockTime,
			DepositAssets: TokenAmount{
				Asset: asset,
				Raw:   "1000000000000000000",
			},
			PreviewShares: TokenAmount{
				Asset: share,
				Raw:   "900000000000000000",
			},
			MaxDeposit: TokenAmount{
				Asset: asset,
				Raw:   "2000000000000000000",
			},
		},
		MinSharesRaw: "891000000000000000",
		Deadline:     uint64(now.Add(30 * time.Second).Unix()),
	}, now
}

func TestBuildDepositTxCalldata(t *testing.T) {
	builder, err := NewDepositTxBuilder()
	if err != nil {
		t.Fatal(err)
	}
	request, now := depositBuilderFixture()

	built, err := builder.BuildDepositTx(request, now)
	if err != nil {
		t.Fatal(err)
	}

	tx := built.Prepared.Tx
	if tx.ChainID != uint64(FujiChainID) ||
		tx.From != common.HexToAddress(request.OwnerAddress).Hex() ||
		tx.To != common.HexToAddress(fujiExecutionManager).Hex() ||
		tx.Value != "0" {
		t.Fatalf("unexpected unsigned transaction: %+v", tx)
	}
	if built.Summary.RecipientAddress != tx.From {
		t.Fatal("recipient must be owner")
	}
	if !built.Prepared.Provenance.IsDemo {
		t.Fatal("DemoVault must retain demo labeling")
	}

	raw, err := hexutil.Decode(tx.Data)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) != 4+4*32 {
		t.Fatalf("calldata length = %d", len(raw))
	}

	method, err := builder.abi.MethodById(raw[:4])
	if err != nil {
		t.Fatal(err)
	}
	if method.Sig != "executeDeposit(address,uint256,uint256,uint256)" {
		t.Fatalf("unexpected method: %s", method.Sig)
	}
	values, err := method.Inputs.Unpack(raw[4:])
	if err != nil {
		t.Fatal(err)
	}
	if values[0].(common.Address) != common.HexToAddress(demoFujiAsset) {
		t.Fatal("wrong underlying token")
	}
	for _, check := range []struct {
		index int
		want  string
	}{
		{1, request.Metrics.DepositAssets.Raw},
		{2, request.MinSharesRaw},
		{3, new(big.Int).SetUint64(request.Deadline).String()},
	} {
		if values[check.index].(*big.Int).String() != check.want {
			t.Fatalf("argument %d mismatch", check.index)
		}
	}

	if built.Summary.Assets.Formatted != "1" ||
		built.Summary.PreviewShares.Formatted != "0.9" ||
		built.Summary.MinShares.Formatted != "0.891" {
		t.Fatalf("unexpected formatted amounts: %+v", built.Summary)
	}
}

func TestBuildDepositTxRejectsInvalidInput(t *testing.T) {
	builder, err := NewDepositTxBuilder()
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name   string
		mutate func(*BuildDepositRequest, *time.Time)
	}{
		{"missing current time", func(r *BuildDepositRequest, now *time.Time) {
			*now = time.Time{}
		}},
		{"invalid owner", func(r *BuildDepositRequest, now *time.Time) {
			r.OwnerAddress = "invalid"
		}},
		{"zero owner", func(r *BuildDepositRequest, now *time.Time) {
			r.OwnerAddress = (common.Address{}).Hex()
			r.Metrics.Position.WalletAddress = r.OwnerAddress
		}},
		{"wallet mismatch", func(r *BuildDepositRequest, now *time.Time) {
			r.Metrics.Position.WalletAddress = fujiExecutionManager
		}},
		{"unknown vault", func(r *BuildDepositRequest, now *time.Time) {
			r.Metrics.Position.VaultAddress = demoFujiAsset
		}},
		{"wrong chain", func(r *BuildDepositRequest, now *time.Time) {
			r.Metrics.Position.Provenance.ChainID = 43114
		}},
		{"wrong asset chain", func(r *BuildDepositRequest, now *time.Time) {
			r.Metrics.DepositAssets.Asset.ChainID = 43114
		}},
		{"wrong underlying", func(r *BuildDepositRequest, now *time.Time) {
			r.Metrics.DepositAssets.Asset.Address = demoFujiVault
		}},
		{"wrong decimals", func(r *BuildDepositRequest, now *time.Time) {
			r.Metrics.DepositAssets.Asset.Decimals = 6
		}},
		{"wrong share token", func(r *BuildDepositRequest, now *time.Time) {
			r.Metrics.PreviewShares.Asset.Address = demoFujiAsset
		}},
		{"missing demo label", func(r *BuildDepositRequest, now *time.Time) {
			r.Metrics.Position.Provenance.IsDemo = false
		}},
		{"mock source", func(r *BuildDepositRequest, now *time.Time) {
			r.Metrics.Position.Provenance.Mode = "mock"
		}},
		{"missing block", func(r *BuildDepositRequest, now *time.Time) {
			r.Metrics.Position.Provenance.BlockNumber = nil
		}},
		{"future observation", func(r *BuildDepositRequest, now *time.Time) {
			r.Metrics.Position.Provenance.ObservedAt = now.Add(time.Second)
		}},
		{"expired preview", func(r *BuildDepositRequest, now *time.Time) {
			r.Metrics.Position.Provenance.ObservedAt = now.Add(-60 * time.Second)
		}},
		{"missing block time", func(r *BuildDepositRequest, now *time.Time) {
			r.Metrics.BlockTime = nil
		}},
		{"stale block time", func(r *BuildDepositRequest, now *time.Time) {
			old := now.Add(-61 * time.Second)
			r.Metrics.BlockTime = &old
		}},
		{"deadline now", func(r *BuildDepositRequest, now *time.Time) {
			r.Deadline = uint64(now.Unix())
		}},
		{"deadline beyond preview", func(r *BuildDepositRequest, now *time.Time) {
			r.Deadline = uint64(now.Add(60 * time.Second).Unix())
		}},
		{"zero assets", func(r *BuildDepositRequest, now *time.Time) {
			r.Metrics.DepositAssets.Raw = "0"
		}},
		{"negative assets", func(r *BuildDepositRequest, now *time.Time) {
			r.Metrics.DepositAssets.Raw = "-1"
		}},
		{"uint256 overflow", func(r *BuildDepositRequest, now *time.Time) {
			r.Metrics.DepositAssets.Raw = new(big.Int).Lsh(
				big.NewInt(1), 256,
			).String()
		}},
		{"zero rounded preview", func(r *BuildDepositRequest, now *time.Time) {
			r.Metrics.PreviewShares.Raw = "0"
		}},
		{"zero minimum", func(r *BuildDepositRequest, now *time.Time) {
			r.MinSharesRaw = "0"
		}},
		{"minimum above preview", func(r *BuildDepositRequest, now *time.Time) {
			r.MinSharesRaw = "900000000000000001"
		}},
		{"above max deposit", func(r *BuildDepositRequest, now *time.Time) {
			r.Metrics.MaxDeposit.Raw = "999999999999999999"
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request, now := depositBuilderFixture()
			test.mutate(&request, &now)
			if _, err := builder.BuildDepositTx(request, now); err == nil {
				t.Fatal("expected rejection")
			}
		})
	}
}

func TestBuildDepositTxAcceptsExactBoundaries(t *testing.T) {
	builder, err := NewDepositTxBuilder()
	if err != nil {
		t.Fatal(err)
	}
	request, now := depositBuilderFixture()
	request.Metrics.MaxDeposit.Raw = request.Metrics.DepositAssets.Raw
	request.MinSharesRaw = request.Metrics.PreviewShares.Raw
	request.Deadline = uint64(
		request.Metrics.Position.Provenance.ObservedAt.Add(
			60 * time.Second,
		).Unix(),
	)

	if _, err := builder.BuildDepositTx(request, now); err != nil {
		t.Fatal(err)
	}
}
