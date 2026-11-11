package avalanche

import (
	"context"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

type aavePositionTestRPC struct {
	RPCReader
	call func(ethereum.CallMsg, *big.Int) ([]byte, error)
}

func (r *aavePositionTestRPC) ChainID(context.Context) (*big.Int, error) {
	return big.NewInt(FujiChainID), nil
}

func (r *aavePositionTestRPC) BlockNumber(context.Context) (uint64, error) {
	return 12345, nil
}

func (r *aavePositionTestRPC) CallContract(
	_ context.Context,
	msg ethereum.CallMsg,
	block *big.Int,
) ([]byte, error) {
	return r.call(msg, block)
}

func TestAavePositionReaderPositions(t *testing.T) {
	pool := common.HexToAddress("0x0000000000000000000000000000000000000010")
	provider := common.HexToAddress("0x0000000000000000000000000000000000000020")
	token := common.HexToAddress("0x0000000000000000000000000000000000000030")
	wallet := common.HexToAddress("0x0000000000000000000000000000000000000040")
	addressesProvider := common.HexToAddress("0x0000000000000000000000000000000000000050")
	oracle := common.HexToAddress("0x0000000000000000000000000000000000000060")

	selectorOf := func(signature string) string {
		return string(crypto.Keccak256([]byte(signature))[:4])
	}

	for _, tt := range []struct {
		name       string
		supplied   int64
		debt       int64
		collateral bool
	}{
		{"supply without collateral", 2_500_000, 0, false},
		{"position with variable debt", 2_500_000, 500_000, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rpc := &aavePositionTestRPC{}
			reader, err := NewAaveLendingReader(rpc, pool.Hex(), provider.Hex())
			if err != nil {
				t.Fatal(err)
			}

			calls := 0
			rpc.call = func(msg ethereum.CallMsg, block *big.Int) ([]byte, error) {
				t.Helper()
				calls++

				if block == nil || block.Cmp(big.NewInt(12345)) != 0 {
					t.Fatalf("contract call block = %v; want 12345", block)
				}
				if msg.To == nil || len(msg.Data) < 4 {
					t.Fatalf("invalid contract call: %+v", msg)
				}

				selector := string(msg.Data[:4])
				switch {
				case *msg.To == pool && selector == selectorOf("getUserEMode(address)"):
					if len(msg.Data) != 36 || common.BytesToAddress(msg.Data[4:]) != wallet {
						t.Fatal("unexpected wallet in getUserEMode")
					}
					return make([]byte, 32), nil // eMode = 0

				case *msg.To == pool && selector == selectorOf("ADDRESSES_PROVIDER()"):
					return common.LeftPadBytes(addressesProvider.Bytes(), 32), nil

				case *msg.To == addressesProvider && selector == selectorOf("getPriceOracle()"):
					return common.LeftPadBytes(oracle.Bytes(), 32), nil

				case *msg.To == oracle && selector == selectorOf("BASE_CURRENCY_UNIT()"):
					return big.NewInt(100_000_000).FillBytes(make([]byte, 32)), nil

				case *msg.To == provider && selector == selectorOf("getReserveConfigurationData(address)"):
					if len(msg.Data) != 36 || common.BytesToAddress(msg.Data[4:]) != token {
						t.Fatal("unexpected token in getReserveConfigurationData")
					}
					raw := make([]byte, 10*32)
					big.NewInt(6).FillBytes(raw[:32])
					big.NewInt(8600).FillBytes(raw[64:96])
					return raw, nil

				case *msg.To == oracle && selector == selectorOf("getAssetPrice(address)"):
					if len(msg.Data) != 36 || common.BytesToAddress(msg.Data[4:]) != token {
						t.Fatal("unexpected token in getAssetPrice")
					}
					return big.NewInt(100_000_000).FillBytes(make([]byte, 32)), nil
				}
				switch {
				case *msg.To == pool:
					method, err := reader.poolABI.MethodById(msg.Data[:4])
					if err != nil {
						return nil, err
					}

					switch method.Name {
					case "getUserAccountData":
						if len(msg.Data) != 36 ||
							common.BytesToAddress(msg.Data[4:36]) != wallet {
							t.Fatal("unexpected wallet in getUserAccountData")
						}

						collateralBase := big.NewInt(0)
						lt := big.NewInt(0)
						ltv := big.NewInt(0)
						hf := new(big.Int).Sub(
							new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1),
						)
						if tt.debt > 0 {
							collateralBase.SetInt64(250_000_000)
							lt.SetInt64(8600)
							ltv.SetInt64(8250)
							hf.SetInt64(4_300_000_000_000_000_000)
						}

						return method.Outputs.Pack(
							collateralBase,
							big.NewInt(tt.debt*100),
							big.NewInt(0),
							lt,
							ltv,
							hf,
						)
					case "getReservesList":
						return method.Outputs.Pack([]common.Address{token})
					}

				case *msg.To == provider &&
					selector == string(crypto.Keccak256([]byte("getUserReserveData(address,address)"))[:4]):
					if len(msg.Data) != 68 ||
						common.BytesToAddress(msg.Data[4:36]) != token ||
						common.BytesToAddress(msg.Data[36:68]) != wallet {
						t.Fatal("unexpected token or wallet in getUserReserveData")
					}

					raw := make([]byte, 9*32)
					big.NewInt(tt.supplied).FillBytes(raw[:32])
					big.NewInt(tt.debt).FillBytes(raw[64:96])
					if tt.collateral {
						raw[len(raw)-1] = 1
					}
					return raw, nil

				case *msg.To == token &&
					selector == string(common.FromHex("0x313ce567")):
					return big.NewInt(6).FillBytes(make([]byte, 32)), nil
				}

				t.Fatalf("unexpected call to %s: %x", msg.To.Hex(), msg.Data)
				return nil, nil
			}

			got, err := reader.ReadLendingPosition(context.Background(), wallet.Hex())
			if err != nil {
				t.Fatal(err)
			}
			if calls != 4 || len(got.Positions) != 1 {
				t.Fatalf("calls=%d positions=%d; want 4 and 1", calls, len(got.Positions))
			}

			position := got.Positions[0]
			if position.Supplied.Formatted != "2.5" ||
				position.Supplied.Asset.Address != token.Hex() ||
				position.Supplied.Asset.Decimals != 6 ||
				position.StableDebt.Raw != "0" ||
				position.VariableDebt.Raw != big.NewInt(tt.debt).String() ||
				position.CollateralEnabled != tt.collateral {
				t.Fatalf("unexpected position: %+v", position)
			}

			if tt.debt == 0 {
				if got.HealthFactorRaw != nil {
					t.Fatal("no-debt sentinel must not be exposed as a finite HF")
				}
			} else if got.HealthFactorRaw == nil ||
				*got.HealthFactorRaw != "4300000000000000000" {
				t.Fatalf("unexpected protocol HF: %v", got.HealthFactorRaw)
			}

			if got.Provenance.BlockNumber == nil ||
				*got.Provenance.BlockNumber != 12345 ||
				got.Provenance.ChainID != uint64(FujiChainID) {
				t.Fatalf("unexpected provenance: %+v", got.Provenance)
			}

			calls = 0
			risk, err := reader.ReadLendingRisk(context.Background(), wallet.Hex())
			if err != nil {
				t.Fatal(err)
			}

			if tt.debt == 0 {
				if calls != 4 || len(risk.Assets) != 0 ||
					risk.Oracle != "" || risk.BaseUnit != "" {
					t.Fatalf("no-debt read should skip oracle: calls=%d risk=%+v", calls, risk)
				}
				if len(risk.Position.Positions) != 1 ||
					risk.Position.Positions[0].Supplied.Raw != "2500000" {
					t.Fatal("no-debt risk snapshot must preserve the supplied position")
				}
				return
			}

			if calls != 10 || len(risk.Assets) != 1 ||
				risk.Oracle != oracle.Hex() || risk.BaseUnit != "100000000" {
				t.Fatalf("unexpected risk snapshot: calls=%d risk=%+v", calls, risk)
			}

			input := risk.Assets[0]
			if input.AssetID != token.Hex() ||
				input.Supplied.RatString() != "5/2" ||
				input.Debt.RatString() != "1/2" ||
				input.OraclePrice == nil || input.OraclePrice.RatString() != "1" ||
				!input.CollateralEnabled || input.LiquidationThresholdBPS != 8600 {
				t.Fatalf("unexpected risk inputs: %+v", input)
			}

			calculated, err := CalculateLendingRisk(risk.Assets, "")
			if err != nil {
				t.Fatal(err)
			}
			if calculated.HealthFactor == nil || calculated.HealthFactor.RatString() != "43/10" {
				t.Fatalf("expected calculated HF = 4.3, got %+v", calculated)
			}
		})
	}
}
