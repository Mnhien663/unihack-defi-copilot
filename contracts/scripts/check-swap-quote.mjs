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
  const overrides = { blockTag: block };

  const pairAddress = "0xc3b7307184381f65B398d385a71EF0490239a2b3";
  const routerAddress = "0x18556DA13313f3532c54711497A8FedAC273220E";

  const pair = new ethers.Contract(pairAddress, [
    "function getTokenX() view returns (address)",
    "function getTokenY() view returns (address)",
    "function getReserves() view returns (uint128 reserveX, uint128 reserveY)"
  ], provider);

  const router = new ethers.Contract(routerAddress, [
    "function getSwapOut(address pair,uint128 amountIn,bool swapForY) view returns (uint128 amountInLeft,uint128 amountOut,uint128 fee)"
  ], provider);

  const tokenX = await pair.getTokenX(overrides);
  const tokenY = await pair.getTokenY(overrides);
  const reserves = await pair.getReserves(overrides);

  const tokenABI = ["function decimals() view returns (uint8)"];
  const decimalsX = Number(await new ethers.Contract(
    tokenX, tokenABI, provider
  ).decimals(overrides));
  const decimalsY = Number(await new ethers.Contract(
    tokenY, tokenABI, provider
  ).decimals(overrides));

  const quotes = [];
  for (const swapForY of [true, false]) {
    const inputDecimals = swapForY ? decimalsX : decimalsY;
    const outputDecimals = swapForY ? decimalsY : decimalsX;
    const amountIn = ethers.parseUnits("1", inputDecimals);

    const result = await router.getSwapOut(
      pairAddress, amountIn, swapForY, overrides
    );

    quotes.push({
      token_in: swapForY ? tokenX : tokenY,
      token_out: swapForY ? tokenY : tokenX,
      swap_for_y: swapForY,
      amount_in_raw: amountIn.toString(),
      amount_in_left_raw: result.amountInLeft.toString(),
      amount_out_raw: result.amountOut.toString(),
      amount_out_formatted: ethers.formatUnits(
        result.amountOut, outputDecimals
      ),
      fee_raw: result.fee.toString()
    });
  }

  const evidence = {
    chain_id: Number(network.chainId),
    block_number: block,
    observed_at: new Date().toISOString(),
    router: routerAddress,
    pair: pairAddress,
    token_x: tokenX,
    token_y: tokenY,
    decimals_x: decimalsX,
    decimals_y: decimalsY,
    reserve_x_raw: reserves.reserveX.toString(),
    reserve_y_raw: reserves.reserveY.toString(),
    quotes
  };

  fs.writeFileSync(
    "../backend/internal/avalanche/testdata/quote_fuji_router.json",
    JSON.stringify(evidence, null, 2) + "\n"
  );
  console.log(JSON.stringify(evidence, null, 2));
} catch (error) {
  console.error(error);
  process.exitCode = 1;
} finally {
  provider.destroy();
}