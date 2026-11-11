package avalanche

import (
	"context"
	"math/big"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

func TestAaveAccountProbe(t *testing.T) {
	if os.Getenv("AAVE_ACCOUNT_PROBE") != "1" {
		t.Skip("set AAVE_ACCOUNT_PROBE=1 to read the Fuji account snapshot")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	cfg, err := LoadRPCConfig()
	if err != nil {
		t.Fatal(err)
	}
	client, err := NewRPCClient(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	height, err := client.BlockNumber(ctx)
	if err != nil {
		t.Fatal(err)
	}

	pool := common.HexToAddress("0x8B9b2AF4afB389b4a70A474dfD4AdCD4a302bb40")
	wallet := common.HexToAddress("0x4DFb0d91C07A7C706F65F974953cE8b111D27Ee5")

	// ABI: 4-byte function selector + 32-byte address word.
	selector := crypto.Keccak256([]byte("getUserAccountData(address)"))[:4]
	calldata := append(selector, common.LeftPadBytes(wallet.Bytes(), 32)...)

	raw, err := client.CallContract(ctx, ethereum.CallMsg{
		To:   &pool,
		Data: calldata,
	}, new(big.Int).SetUint64(height))
	if err != nil {
		t.Fatalf("getUserAccountData at block %d: %v", height, err)
	}

	fields := []string{
		"total_collateral_base_raw",
		"total_debt_base_raw",
		"available_borrows_base_raw",
		"liquidation_threshold_bps",
		"ltv_bps",
		"health_factor_raw",
	}
	if len(raw) != 32*len(fields) {
		t.Fatalf("getUserAccountData returned %d bytes; want %d", len(raw), 32*len(fields))
	}

	t.Logf("block=%d pool=%s wallet=%s", height, pool.Hex(), wallet.Hex())
	for i, field := range fields {
		value := new(big.Int).SetBytes(raw[i*32 : (i+1)*32])
		t.Logf("%s=%s", field, value)
	}

	block := new(big.Int).SetUint64(height)

	callWords := func(to common.Address, signature string, words int, args ...byte) []byte {
		t.Helper()

		selector := crypto.Keccak256([]byte(signature))[:4]
		data := append(selector, args...)

		raw, err := client.CallContract(ctx, ethereum.CallMsg{
			To:   &to,
			Data: data,
		}, block)
		if err != nil {
			t.Fatalf("%s at block %d: %v", signature, height, err)
		}
		if len(raw) != 32*words {
			t.Fatalf("%s returned %d bytes; want %d", signature, len(raw), 32*words)
		}
		return raw
	}

	callWord := func(to common.Address, signature string, args ...byte) []byte {
		t.Helper()
		return callWords(to, signature, 1, args...)
	}

	provider := common.BytesToAddress(callWord(pool, "ADDRESSES_PROVIDER()"))
	if provider == (common.Address{}) {
		t.Fatal("Pool returned a zero Addresses Provider")
	}

	oracle := common.BytesToAddress(callWord(provider, "getPriceOracle()"))
	if oracle == (common.Address{}) {
		t.Fatal("Addresses Provider returned a zero oracle")
	}

	baseCurrency := common.BytesToAddress(callWord(oracle, "BASE_CURRENCY()"))
	baseUnit := new(big.Int).SetBytes(callWord(oracle, "BASE_CURRENCY_UNIT()"))

	usdc := common.HexToAddress("0x5425890298aed601595a70AB815c96711a31Bc65")
	usdcPrice := new(big.Int).SetBytes(callWord(
		oracle,
		"getAssetPrice(address)",
		common.LeftPadBytes(usdc.Bytes(), 32)...,
	))

	t.Logf("provider=%s oracle=%s", provider.Hex(), oracle.Hex())
	t.Logf("base_currency=%s base_currency_unit=%s", baseCurrency.Hex(), baseUnit)
	t.Logf("usdc=%s oracle_price_raw=%s", usdc.Hex(), usdcPrice)

	if baseUnit.Sign() <= 0 || usdcPrice.Sign() <= 0 {
		t.Fatal("oracle base unit and USDC price must be positive")
	}

	// Data Provider recorded in docs/architecture.md for this Fuji market.
	dataProvider := common.HexToAddress("0xC65cbd1e309Bf0e841Ee6f6E786480598e6a4014")
	usdcWord := common.LeftPadBytes(usdc.Bytes(), 32)
	walletWord := common.LeftPadBytes(wallet.Bytes(), 32)

	reserveCfg := callWords(
		dataProvider,
		"getReserveConfigurationData(address)",
		10,
		usdcWord...,
	)
	userReserve := callWords(
		dataProvider,
		"getUserReserveData(address,address)",
		9,
		append(usdcWord, walletWord...)...,
	)
	eMode := new(big.Int).SetBytes(callWord(pool, "getUserEMode(address)", walletWord...))

	uintWord := func(data []byte, i int) *big.Int {
		return new(big.Int).SetBytes(data[32*i : 32*(i+1)])
	}

	t.Logf("data_provider=%s user_emode=%s", dataProvider.Hex(), eMode)
	t.Logf(
		"reserve_decimals=%s reserve_ltv_bps=%s reserve_lt_bps=%s",
		uintWord(reserveCfg, 0),
		uintWord(reserveCfg, 1),
		uintWord(reserveCfg, 2),
	)
	t.Logf(
		"user_supplied_raw=%s user_stable_debt_raw=%s user_variable_debt_raw=%s user_collateral_flag=%s",
		uintWord(userReserve, 0),
		uintWord(userReserve, 1),
		uintWord(userReserve, 2),
		uintWord(userReserve, 8),
	)

	reserveListABI, err := abi.JSON(strings.NewReader(`[
	{"type":"function","name":"getReservesList","inputs":[],
	 "outputs":[{"type":"address[]"}]}
]`))
	if err != nil {
		t.Fatal(err)
	}

	listData, err := reserveListABI.Pack("getReservesList")
	if err != nil {
		t.Fatal(err)
	}

	listRaw, err := client.CallContract(ctx, ethereum.CallMsg{
		To:   &pool,
		Data: listData,
	}, block)
	if err != nil {
		t.Fatalf("getReservesList at block %d: %v", height, err)
	}

	listValues, err := reserveListABI.Unpack("getReservesList", listRaw)
	if err != nil {
		t.Fatalf("decode getReservesList: %v", err)
	}
	if len(listValues) != 1 {
		t.Fatalf("getReservesList returned %d outputs; want 1", len(listValues))
	}

	reserves, ok := listValues[0].([]common.Address)
	if !ok || len(reserves) == 0 {
		t.Fatal("getReservesList did not return a non-empty address array")
	}

	seen := make(map[common.Address]bool, len(reserves))
	for _, reserve := range reserves {
		if reserve == (common.Address{}) || seen[reserve] {
			t.Fatalf("zero or duplicate reserve address: %s", reserve.Hex())
		}
		seen[reserve] = true
	}
	if !seen[usdc] {
		t.Fatal("Aave USDC is missing from the Pool reserve list")
	}

	t.Logf("reserve_count=%d includes_aave_usdc=%t", len(reserves), seen[usdc])

	positions, err := readAaveUserReserves(
		ctx, client, dataProvider, wallet, block, reserves,
	)
	if err != nil {
		t.Fatal(err)
	}

	for _, position := range positions {
		t.Logf(
			"reserve=%s supplied_raw=%s stable_debt_raw=%s variable_debt_raw=%s collateral=%t",
			position.Asset.Hex(),
			position.SuppliedRaw,
			position.StableDebtRaw,
			position.VariableDebtRaw,
			position.CollateralEnabled,
		)
	}

	reader, err := NewAavePositionReader(client, pool.Hex(), dataProvider.Hex())
	if err != nil {
		t.Fatal(err)
	}

	snapshot, err := reader.ReadLendingPosition(ctx, wallet.Hex())
	if err != nil {
		t.Fatal(err)
	}

	t.Logf(
		"reader_block=%v positions=%d protocol_hf_present=%t source=%s",
		*snapshot.Provenance.BlockNumber,
		len(snapshot.Positions),
		snapshot.HealthFactorRaw != nil,
		snapshot.Provenance.Source,
	)
}
