import { ethers } from "ethers";
import fs from "node:fs";

const provider = new ethers.JsonRpcProvider(
  "https://api.avax-test.network/ext/bc/C/rpc"
);

try {
  const network = await provider.getNetwork();
  if (network.chainId !== 43113n) {
    throw new Error("RPC is not Fuji");
  }

  const block = await provider.getBlockNumber();
  const at = { blockTag: block };

  const addressesProviderAddress =
    "0x07D04EfAAA0Ac69D19d107795aF247C42Eb50F1C";
  const expectedPool =
    "0x8B9b2AF4afB389b4a70A474dfD4AdCD4a302bb40";
  const asset = "0x5425890298aed601595a70AB815c96711a31Bc65";

  const addressesProvider = new ethers.Contract(
    addressesProviderAddress,
    [
      "function getPool() view returns (address)",
      "function getPriceOracle() view returns (address)"
    ],
    provider
  );

  const poolAddress = await addressesProvider.getPool(at);
  const oracleAddress = await addressesProvider.getPriceOracle(at);

  if (poolAddress.toLowerCase() !== expectedPool.toLowerCase()) {
    throw new Error(`Pool changed: ${poolAddress}`);
  }
  if (oracleAddress === ethers.ZeroAddress) {
    throw new Error("Oracle is zero address");
  }

  const oracle = new ethers.Contract(
    oracleAddress,
    [
      "function BASE_CURRENCY() view returns (address)",
      "function BASE_CURRENCY_UNIT() view returns (uint256)",
      "function getAssetPrice(address asset) view returns (uint256)",
      "function getSourceOfAsset(address asset) view returns (address)"
    ],
    provider
  );

  const baseCurrency = await oracle.BASE_CURRENCY(at);
  const baseUnit = await oracle.BASE_CURRENCY_UNIT(at);
  const price = await oracle.getAssetPrice(asset, at);
  const source = await oracle.getSourceOfAsset(asset, at);

  if (baseUnit <= 0n || price <= 0n) {
    throw new Error("Invalid oracle unit or asset price");
  }

  const blockInfo = await provider.getBlock(block);
  if (!blockInfo) {
    throw new Error("Selected block was not found");
  }

  let sourceObservation = {
    address: source,
    round_data_available: false,
    updated_at: null,
    freshness_verified: false
  };

  if (source !== ethers.ZeroAddress) {
    const feed = new ethers.Contract(source, [
      "function decimals() view returns (uint8)",
      "function latestRoundData() view returns (uint80 roundId,int256 answer,uint256 startedAt,uint256 updatedAt,uint80 answeredInRound)"
    ], provider);

    try {
      const decimals = Number(await feed.decimals(at));
      const round = await feed.latestRoundData(at);

      sourceObservation = {
        address: source,
        round_data_available: true,
        decimals,
        round_id: round.roundId.toString(),
        answer_raw: round.answer.toString(),
        started_at: round.startedAt.toString(),
        updated_at: round.updatedAt.toString(),
        answered_in_round: round.answeredInRound.toString(),
        age_seconds:
          (BigInt(blockInfo.timestamp) - round.updatedAt).toString(),
        freshness_verified: false
      };
    } catch (error) {
        // Record failure without inventing a timestamp or fallback price.
      sourceObservation.read_error = {
        code: error.code ?? "UNKNOWN",
        message: error.shortMessage ?? "Source metadata read failed"
    };
  }
}
  
  const wallet = "0x4DFb0d91C07A7C706F65F974953cE8b111D27Ee5";

const pool = new ethers.Contract(poolAddress, [
  "function getUserAccountData(address user) view returns (uint256 totalCollateralBase,uint256 totalDebtBase,uint256 availableBorrowsBase,uint256 currentLiquidationThreshold,uint256 ltv,uint256 healthFactor)"
], provider);

const account = await pool.getUserAccountData(wallet, at);

const accountObservation = {
  wallet_address: wallet,
  total_collateral_base_raw: account.totalCollateralBase.toString(),
  total_debt_base_raw: account.totalDebtBase.toString(),
  available_borrows_base_raw: account.availableBorrowsBase.toString(),
  current_liquidation_threshold_bps:
    account.currentLiquidationThreshold.toString(),
  ltv_bps: account.ltv.toString(),
  health_factor_raw: account.healthFactor.toString(),
  health_factor_scale_raw: "1000000000000000000"
};

  const dataProviderAddress =
  "0xC65cbd1e309Bf0e841Ee6f6E786480598e6a4014";

const dataProvider = new ethers.Contract(dataProviderAddress, [
  "function ADDRESSES_PROVIDER() view returns (address)",
  "function getReserveConfigurationData(address asset) view returns (uint256 decimals,uint256 ltv,uint256 liquidationThreshold,uint256 liquidationBonus,uint256 reserveFactor,bool usageAsCollateralEnabled,bool borrowingEnabled,bool stableBorrowRateEnabled,bool isActive,bool isFrozen)",
  "function getUserReserveData(address asset,address user) view returns (uint256 currentATokenBalance,uint256 currentStableDebt,uint256 currentVariableDebt,uint256 principalStableDebt,uint256 scaledVariableDebt,uint256 stableBorrowRate,uint256 liquidityRate,uint40 stableRateLastUpdated,bool usageAsCollateralEnabled)"
], provider);

const linkedProvider = await dataProvider.ADDRESSES_PROVIDER(at);
if (linkedProvider.toLowerCase() !==
    addressesProviderAddress.toLowerCase()) {
  throw new Error("Data Provider belongs to another market");
}

const config = await dataProvider.getReserveConfigurationData(asset, at);
const userReserve = await dataProvider.getUserReserveData(
  asset, wallet, at
);

const reserveObservation = {
  asset,
  decimals: config.decimals.toString(),
  reserve_ltv_bps: config.ltv.toString(),
  reserve_liquidation_threshold_bps:
    config.liquidationThreshold.toString(),
  reserve_collateral_enabled: config.usageAsCollateralEnabled,
  borrowing_enabled: config.borrowingEnabled,
  is_active: config.isActive,
  is_frozen: config.isFrozen,
  supplied_raw: userReserve.currentATokenBalance.toString(),
  stable_debt_raw: userReserve.currentStableDebt.toString(),
  variable_debt_raw: userReserve.currentVariableDebt.toString(),
  user_collateral_enabled: userReserve.usageAsCollateralEnabled
};

  const evidence = {
    chain_id: Number(network.chainId),
    block_number: block,
    observed_at: new Date().toISOString(),
    addresses_provider: addressesProviderAddress,
    pool: poolAddress,
    oracle: oracleAddress,
    asset,
    base_currency: baseCurrency,
    base_currency_unit_raw: baseUnit.toString(),
    asset_price_raw: price.toString(),
    asset_price_source: source,
    block_timestamp: blockInfo.timestamp,
    source_observation: sourceObservation,
    oracle_updated_at: sourceObservation.updated_at,
    oracle_freshness_verified: false,
    account: accountObservation,
    data_provider: dataProviderAddress,
    reserve: reserveObservation
  };

  fs.writeFileSync(
    "../backend/internal/avalanche/testdata/position_fuji_oracle.json",
    JSON.stringify(evidence, null, 2) + "\n"
  );

  console.log(JSON.stringify(evidence, null, 2));
} catch (error) {
  console.error(error);
  process.exitCode = 1;
} finally {
  provider.destroy();
}