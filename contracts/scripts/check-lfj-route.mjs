import { ethers } from "ethers";
import fs from "node:fs";

const provider = new ethers.JsonRpcProvider(
  "https://api.avax-test.network/ext/bc/C/rpc"
);

try {
  const chain = await provider.getNetwork();
  if (chain.chainId !== 43113n) {
    throw new Error("RPC is not Fuji");
  }

  const block = await provider.getBlockNumber();
  const at = { blockTag: block };

  const routerAddress = "0x18556DA13313f3532c54711497A8FedAC273220E";
  const expectedFactory = "0xb43120c4745967fa9b93E79C149E66B0f2D6Fe0c";
  const expectedPair = "0xc3b7307184381f65B398d385a71EF0490239a2b3";
  const tokenX = "0xAb231A5744C8E6c45481754928cCfFFFD4aa0732";
  const tokenY = "0xB6076C93701D6a07266c31066B298AeC6dd65c2d";

  const router = new ethers.Contract(routerAddress, [
    "function getFactory() view returns (address)",
    "function getSwapOut(address pair,uint128 amountIn,bool swapForY) view returns (uint128 amountInLeft,uint128 amountOut,uint128 fee)"
  ], provider);

  const factoryAddress = await router.getFactory(at);
  if (ethers.getAddress(factoryAddress) !==
      ethers.getAddress(expectedFactory)) {
    throw new Error(`Unexpected router factory: ${factoryAddress}`);
  }

  const factory = new ethers.Contract(factoryAddress, [
    "function getLBPairInformation(address tokenA,address tokenB,uint256 binStep) view returns (tuple(uint16 binStep,address LBPair,bool createdByOwner,bool ignoredForRouting))"
  ], provider);

  const info = await factory.getLBPairInformation(tokenX, tokenY, 1, at);
  if (ethers.getAddress(info.LBPair) !== ethers.getAddress(expectedPair)) {
    throw new Error(`Execution pair differs from quote pair: ${info.LBPair}`);
  }
  if (info.binStep !== 1n || info.ignoredForRouting) {
    throw new Error("Unexpected bin step or pair ignored for routing");
  }

  const pair = new ethers.Contract(expectedPair, [
    "function getTokenX() view returns (address)",
    "function getTokenY() view returns (address)"
  ], provider);

  if (ethers.getAddress(await pair.getTokenX(at)) !==
        ethers.getAddress(tokenX) ||
      ethers.getAddress(await pair.getTokenY(at)) !==
        ethers.getAddress(tokenY)) {
    throw new Error("Pair token identities differ");
  }

  const quotes = [];
  for (const direction of [true, false]) {
    const result = await router.getSwapOut(
      expectedPair, 1_000_000n, direction, at
    );

    if (result.amountInLeft !== 0n || result.amountOut <= 0n) {
      throw new Error("Route cannot quote full input");
    }

    quotes.push({
      swap_for_y: direction,
      amount_in_raw: "1000000",
      amount_in_left_raw: result.amountInLeft.toString(),
      amount_out_raw: result.amountOut.toString(),
      fee_raw: result.fee.toString()
    });
  }

  const evidence = {
    chain_id: 43113,
    block_number: block,
    observed_at: new Date().toISOString(),
    router: routerAddress,
    factory: factoryAddress,
    pair: info.LBPair,
    bin_step: Number(info.binStep),
    version: "V2.2",
    ignored_for_routing: info.ignoredForRouting,
    token_x: tokenX,
    token_y: tokenY,
    quotes
  };

  fs.writeFileSync(
    "../backend/internal/avalanche/testdata/swap_lfj_route.json",
    JSON.stringify(evidence, null, 2) + "\n"
  );
  console.log(JSON.stringify(evidence, null, 2));
} catch (error) {
  console.error(error);
  process.exitCode = 1;
} finally {
  provider.destroy();
}