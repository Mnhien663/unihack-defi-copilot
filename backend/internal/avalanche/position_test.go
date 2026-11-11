package avalanche

import (
	"context"
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

const positionTestWallet = "0x0000000000000000000000000000000000000001"

type positionRPCStub struct {
	reader        *RPCAavePositionReader
	chainID       *big.Int
	contractCalls int
	headerCalls   int
	responses     map[string][]interface{}
	failMethod    string
	callErr       error
	shortMethod   string
	changeHeader  bool
}

func (s *positionRPCStub) ChainID(context.Context) (*big.Int, error) {
	return s.chainID, nil
}

func (s *positionRPCStub) BlockNumber(context.Context) (uint64, error) {
	return 12345, nil
}

func (s *positionRPCStub) HeaderByNumber(
	ctx context.Context,
	number *big.Int,
) (*types.Header, error) {
	s.headerCalls++
	if number == nil || number.Cmp(big.NewInt(12345)) != 0 {
		return nil, errors.New("expected pinned header block")
	}
	header := &types.Header{
		Number: big.NewInt(12345),
		Time:   100_000,
	}
	if s.changeHeader && s.headerCalls > 1 {
		header.Time++
	}
	return header, nil
}

func (s *positionRPCStub) BalanceAt(
	context.Context,
	common.Address,
	*big.Int,
) (*big.Int, error) {
	return nil, errors.New("position must not read native balance")
}

func (s *positionRPCStub) CallContract(
	ctx context.Context,
	msg ethereum.CallMsg,
	block *big.Int,
) ([]byte, error) {
	s.contractCalls++

	if block == nil || block.Cmp(big.NewInt(12345)) != 0 {
		return nil, errors.New("expected pinned contract block")
	}
	if msg.To == nil || len(msg.Data) < 4 {
		return nil, errors.New("invalid contract call")
	}

	r := s.reader
	method, err := r.abi.MethodById(msg.Data[:4])
	if err != nil {
		return nil, err
	}

	if method.Name == s.failMethod {
		return nil, s.callErr
	}
	if method.Name == s.shortMethod {
		return []byte{0x01}, nil
	}

	oracle := common.HexToAddress("0x0000000000000000000000000000000000000030")
	source := common.HexToAddress("0x0000000000000000000000000000000000000040")

	targets := map[string]common.Address{
		"getPool":                     r.addressesProvider,
		"getPriceOracle":              r.addressesProvider,
		"ADDRESSES_PROVIDER":          r.dataProvider,
		"getUserAccountData":          r.pool,
		"getReserveConfigurationData": r.dataProvider,
		"getUserReserveData":          r.dataProvider,
		"BASE_CURRENCY":               oracle,
		"BASE_CURRENCY_UNIT":          oracle,
		"getAssetPrice":               oracle,
		"getSourceOfAsset":            oracle,
		"decimals":                    source,
		"latestRoundData":             source,
	}
	if target, ok := targets[method.Name]; !ok || *msg.To != target {
		return nil, errors.New("method sent to unexpected contract")
	}

	args, err := method.Inputs.Unpack(msg.Data[4:])
	if err != nil {
		return nil, err
	}
	wallet := common.HexToAddress(positionTestWallet)

	switch method.Name {
	case "getUserAccountData":
		if args[0].(common.Address) != wallet {
			return nil, errors.New("wrong account owner")
		}
	case "getUserReserveData":
		if args[0].(common.Address) != r.asset ||
			args[1].(common.Address) != wallet {
			return nil, errors.New("wrong reserve asset or owner")
		}
	case "getReserveConfigurationData", "getAssetPrice", "getSourceOfAsset":
		if args[0].(common.Address) != r.asset {
			return nil, errors.New("wrong asset")
		}
	}

	values, ok := s.responses[method.Name]
	if !ok {
		return nil, errors.New("missing mock response")
	}
	return method.Outputs.Pack(values...)
}

func newPositionTestReader(
	t *testing.T,
	maxAge uint64,
) (*RPCAavePositionReader, *positionRPCStub) {
	t.Helper()

	maxUint256 := new(big.Int).Sub(
		new(big.Int).Lsh(big.NewInt(1), 256),
		big.NewInt(1),
	)

	client := &positionRPCStub{
		chainID: big.NewInt(FujiChainID),
		responses: map[string][]interface{}{
			"getPool": {
				common.HexToAddress(aaveFujiPool),
			},
			"getPriceOracle": {
				common.HexToAddress("0x0000000000000000000000000000000000000030"),
			},
			"ADDRESSES_PROVIDER": {
				common.HexToAddress(aaveFujiAddressesProvider),
			},
			"getUserAccountData": {
				big.NewInt(0), big.NewInt(0), big.NewInt(0),
				big.NewInt(0), big.NewInt(0), maxUint256,
			},
			"getReserveConfigurationData": {
				big.NewInt(6), big.NewInt(8250), big.NewInt(8600),
				big.NewInt(10500), big.NewInt(1000),
				true, true, false, true, false,
			},
			"getUserReserveData": {
				big.NewInt(0), big.NewInt(0), big.NewInt(0),
				big.NewInt(0), big.NewInt(0), big.NewInt(0),
				big.NewInt(0), big.NewInt(0), false,
			},
			"BASE_CURRENCY":      {common.Address{}},
			"BASE_CURRENCY_UNIT": {big.NewInt(100_000_000)},
			"getAssetPrice":      {big.NewInt(99_975_000)},
			"getSourceOfAsset": {
				common.HexToAddress("0x0000000000000000000000000000000000000040"),
			},
			"decimals": {uint8(8)},
			"latestRoundData": {
				big.NewInt(7), big.NewInt(99_975_000),
				big.NewInt(99_000), big.NewInt(99_000), big.NewInt(7),
			},
		},
	}

	reader, err := NewAavePositionReader(client, AavePositionConfig{
		MaxOracleAgeSeconds: maxAge,
	})
	if err != nil {
		t.Fatal(err)
	}
	client.reader = reader
	return reader, client
}

func TestAavePositionReadsEmptyAccount(t *testing.T) {
	reader, client := newPositionTestReader(t, 0)

	result, err := reader.ReadPosition(
		context.Background(), positionTestWallet,
	)
	if err != nil {
		t.Fatal(err)
	}

	if result.WalletAddress != positionTestWallet ||
		result.Account.HasDebt ||
		result.Account.TotalCollateralBaseRaw != "0" ||
		result.Account.TotalDebtBaseRaw != "0" {
		t.Fatalf("unexpected account: %+v", result.Account)
	}

	maxUint256 := new(big.Int).Sub(
		new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1),
	).String()
	if result.Account.HealthFactorRaw != maxUint256 {
		t.Fatal("no-debt health factor sentinel was changed")
	}

	reserve := result.Reserve
	if reserve.SuppliedRaw != "0" ||
		reserve.StableDebtRaw != "0" ||
		reserve.VariableDebtRaw != "0" ||
		reserve.UserCollateralEnabled ||
		!reserve.ReserveCollateralEnabled ||
		reserve.ReserveLiquidationThresholdBps != 8600 ||
		reserve.Asset.Decimals != 6 {
		t.Fatalf("unexpected reserve: %+v", reserve)
	}

	price := result.Oracle
	if price.PriceRaw != "99975000" ||
		price.BaseCurrencyUnitRaw != "100000000" ||
		price.SourceDecimals == nil || *price.SourceDecimals != 8 ||
		price.UpdatedAt == nil || *price.UpdatedAt != 99000 ||
		price.AgeSeconds == nil || *price.AgeSeconds != 1000 ||
		price.FreshnessStatus != OracleFreshnessUnknown {
		t.Fatalf("unexpected oracle: %+v", price)
	}

	p := result.Provenance
	if p.BlockNumber == nil || *p.BlockNumber != 12345 ||
		result.BlockTimestamp != 100000 ||
		p.SourceType != "rpc" || p.Mode != "live" ||
		p.IsDemo || p.ObservedAt.IsZero() {
		t.Fatalf("unexpected provenance: %+v", p)
	}

	if client.headerCalls != 2 || client.contractCalls != 12 {
		t.Fatalf("header calls=%d, contract calls=%d; want 2 and 12",
			client.headerCalls, client.contractCalls)
	}
}

func TestAavePositionReadsDebtWithoutCollateral(t *testing.T) {
	reader, client := newPositionTestReader(t, 0)

	client.responses["getUserAccountData"] = []interface{}{
		big.NewInt(0), big.NewInt(200_000_000), big.NewInt(0),
		big.NewInt(0), big.NewInt(0), big.NewInt(0),
	}
	client.responses["getUserReserveData"] = []interface{}{
		big.NewInt(0), big.NewInt(1_000_000), big.NewInt(1_000_000),
		big.NewInt(0), big.NewInt(0), big.NewInt(0),
		big.NewInt(0), big.NewInt(0), false,
	}

	result, err := reader.ReadPosition(
		context.Background(), positionTestWallet,
	)
	if err != nil {
		t.Fatal(err)
	}

	if !result.Account.HasDebt ||
		result.Account.TotalCollateralBaseRaw != "0" ||
		result.Account.TotalDebtBaseRaw != "200000000" ||
		result.Account.HealthFactorRaw != "0" {
		t.Fatalf("unexpected account: %+v", result.Account)
	}
	if result.Reserve.SuppliedRaw != "0" ||
		result.Reserve.StableDebtRaw != "1000000" ||
		result.Reserve.VariableDebtRaw != "1000000" {
		t.Fatalf("unexpected reserve: %+v", result.Reserve)
	}
}

func TestAavePositionOracleAgePolicy(t *testing.T) {
	tests := []struct {
		name      string
		maxAge    uint64
		wantState string
		wantErr   error
	}{
		{"unconfigured policy", 0, OracleFreshnessUnknown, nil},
		{"within policy", 1001, OracleFreshnessFresh, nil},
		{"exact age limit", 1000, OracleFreshnessFresh, nil},
		{"exceeds policy", 999, "", ErrOraclePriceStale},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader, _ := newPositionTestReader(t, tt.maxAge)
			result, err := reader.ReadPosition(
				context.Background(), positionTestWallet,
			)

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("want %v, got %v", tt.wantErr, err)
				}
				if result.WalletAddress != "" {
					t.Fatal("stale-price error must not return partial position")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if result.Oracle.FreshnessStatus != tt.wantState {
				t.Fatalf("freshness=%s; want %s",
					result.Oracle.FreshnessStatus, tt.wantState)
			}
		})
	}
}

func TestAavePositionRejectsInvalidOracleData(t *testing.T) {
	tests := []struct {
		name   string
		change func(*positionRPCStub)
	}{
		{"zero price", func(s *positionRPCStub) {
			s.responses["getAssetPrice"][0] = big.NewInt(0)
		}},
		{"missing source", func(s *positionRPCStub) {
			s.responses["getSourceOfAsset"][0] = common.Address{}
		}},
		{"scale mismatch", func(s *positionRPCStub) {
			s.responses["decimals"][0] = uint8(18)
		}},
		{"negative source answer", func(s *positionRPCStub) {
			s.responses["latestRoundData"][1] = big.NewInt(-1)
		}},
		{"source price mismatch", func(s *positionRPCStub) {
			s.responses["latestRoundData"][1] = big.NewInt(100_000_000)
		}},
		{"missing timestamp", func(s *positionRPCStub) {
			s.responses["latestRoundData"][3] = big.NewInt(0)
		}},
		{"future timestamp", func(s *positionRPCStub) {
			s.responses["latestRoundData"][3] = big.NewInt(100_001)
		}},
		{"round not answered", func(s *positionRPCStub) {
			s.responses["latestRoundData"][4] = big.NewInt(6)
		}},
		{"start after update", func(s *positionRPCStub) {
			s.responses["latestRoundData"][2] = big.NewInt(99_001)
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader, client := newPositionTestReader(t, 0)
			tt.change(client)

			result, err := reader.ReadPosition(
				context.Background(), positionTestWallet,
			)
			if !errors.Is(err, ErrOraclePriceUnavailable) {
				t.Fatalf("want invalid oracle error, got %v", err)
			}
			if result.WalletAddress != "" {
				t.Fatal("invalid oracle must not return partial position")
			}
		})
	}
}

func TestAavePositionRejectsInvalidWallet(t *testing.T) {
	for _, wallet := range []string{
		"bad",
		"",
		"0x0000000000000000000000000000000000000000",
	} {
		t.Run(wallet, func(t *testing.T) {
			reader, client := newPositionTestReader(t, 0)

			_, err := reader.ReadPosition(context.Background(), wallet)
			if !errors.Is(err, ErrInvalidAddress) {
				t.Fatalf("want ErrInvalidAddress, got %v", err)
			}
			if client.headerCalls != 0 || client.contractCalls != 0 {
				t.Fatal("invalid wallet reached block or contract reads")
			}
		})
	}
}

func TestAavePositionRejectsWrongChain(t *testing.T) {
	reader, client := newPositionTestReader(t, 0)
	client.chainID = big.NewInt(43114)

	_, err := reader.ReadPosition(
		context.Background(), positionTestWallet,
	)
	if !errors.Is(err, ErrWrongChain) {
		t.Fatalf("want ErrWrongChain, got %v", err)
	}
	if client.headerCalls != 0 || client.contractCalls != 0 {
		t.Fatal("wrong chain reached block or contract reads")
	}
}

func TestAavePositionRejectsWrongMarket(t *testing.T) {
	for _, method := range []string{
		"getPool",
		"ADDRESSES_PROVIDER",
	} {
		t.Run(method, func(t *testing.T) {
			reader, client := newPositionTestReader(t, 0)
			client.responses[method][0] = common.HexToAddress(
				"0x0000000000000000000000000000000000000099",
			)

			result, err := reader.ReadPosition(
				context.Background(), positionTestWallet,
			)
			if !errors.Is(err, ErrPositionSourceMismatch) {
				t.Fatalf("want source mismatch, got %v", err)
			}
			if result.WalletAddress != "" {
				t.Fatal("wrong market must not return a position")
			}
		})
	}
}

func TestAavePositionHonorsCancellation(t *testing.T) {
	reader, client := newPositionTestReader(t, 0)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := reader.ReadPosition(ctx, positionTestWallet)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
	if client.headerCalls != 0 || client.contractCalls != 0 {
		t.Fatal("cancelled request reached block or contract reads")
	}
}

func TestNewAavePositionReaderRejectsNilClient(t *testing.T) {
	_, err := NewAavePositionReader(nil, AavePositionConfig{})
	if err == nil {
		t.Fatal("expected nil client to be rejected")
	}
}

func TestAavePositionPreservesRPCErrors(t *testing.T) {
	tests := []struct {
		name   string
		method string
		err    error
	}{
		{
			"account timeout",
			"getUserAccountData",
			context.DeadlineExceeded,
		},
		{
			"oracle cancellation",
			"latestRoundData",
			context.Canceled,
		},
		{
			"oracle source unavailable",
			"latestRoundData",
			errors.New("source unavailable"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader, client := newPositionTestReader(t, 0)
			client.failMethod = tt.method
			client.callErr = tt.err

			result, err := reader.ReadPosition(
				context.Background(), positionTestWallet,
			)
			if !errors.Is(err, tt.err) {
				t.Fatalf("want preserved error %v, got %v", tt.err, err)
			}
			if result.WalletAddress != "" {
				t.Fatal("RPC failure must not produce a partial position")
			}
		})
	}
}

func TestAavePositionRejectsShortResponses(t *testing.T) {
	for _, method := range []string{
		"getUserAccountData",
		"getReserveConfigurationData",
		"getUserReserveData",
		"latestRoundData",
	} {
		t.Run(method, func(t *testing.T) {
			reader, client := newPositionTestReader(t, 0)
			client.shortMethod = method

			result, err := reader.ReadPosition(
				context.Background(), positionTestWallet,
			)
			if err == nil {
				t.Fatal("expected short ABI response to be rejected")
			}
			if result.WalletAddress != "" {
				t.Fatal("invalid response must not produce a position")
			}
		})
	}
}

func TestAavePositionRejectsChangedBlock(t *testing.T) {
	reader, client := newPositionTestReader(t, 0)
	client.changeHeader = true

	result, err := reader.ReadPosition(
		context.Background(), positionTestWallet,
	)
	if err == nil {
		t.Fatal("expected changed block to be rejected")
	}
	if client.headerCalls != 2 {
		t.Fatal("expected initial and final block checks")
	}
	if result.WalletAddress != "" {
		t.Fatal("changed block must not produce a position")
	}
}
