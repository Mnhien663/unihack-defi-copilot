import { network } from "hardhat";

const { ethers } = await network.create();
const provider = ethers.provider;

const chain = await provider.getNetwork();
if (chain.chainId !== 43113n) {
  throw new Error("Expected Fuji chain ID 43113");
}

const block = await provider.getBlock("latest");
if (!block) throw new Error("Latest block missing");

const options = { blockTag: block.number };

const router = new ethers.Contract(
  "0xb4315e873dBcf96Ffd0acd8EA43f689D8c20fB30",
  [
    "function getWNATIVE() view returns (address)",
    "function getFactory() view returns (address)",
    "function getSwapOut(address pair,uint128 amountIn,bool swapForY) view returns (uint128 amountInLeft,uint128 amountOut,uint128 fee)",
  ],
  provider
);

const wrapped = await router.getFunction("getWNATIVE")(options);
const factoryAddress = await router.getFunction("getFactory")(options);

const factory = new ethers.Contract(
  factoryAddress,
  [
    "function getAllLBPairs(address tokenA,address tokenB) view returns (tuple(uint16 binStep,address LBPair,bool createdByOwner,bool ignoredForRouting)[])",
  ],
  provider
);

console.log("Chain:", chain.chainId.toString());
console.log("Block:", block.number);
console.log("Wrapped AVAX:", wrapped);
console.log("Factory:", factoryAddress);

const amountIn = ethers.parseEther("0.001");

for (const token of [
  {
    symbol: "LFJ USDT",
    address: "0xAb231A5744C8E6c45481754928cCfFFFD4aa0732",
  },
  {
    symbol: "LFJ USDC",
    address: "0xB6076C93701D6a07266c31066B298AeC6dd65c2d",
  },
]) {
  console.log(`\n${token.symbol}`);

  const pairs = await factory.getFunction("getAllLBPairs")(
    wrapped,
    token.address,
    options
  );

  console.log("Pool count:", pairs.length);

  for (const info of pairs) {
    console.log("Pair:", info.LBPair);
    console.log("Bin step:", info.binStep.toString());
    console.log("Ignored:", info.ignoredForRouting);

    const pair = new ethers.Contract(
      info.LBPair,
      ["function getTokenY() view returns (address)"],
      provider
    );

    const tokenY = await pair.getFunction("getTokenY")(options);
    const swapForY =
      tokenY.toLowerCase() === token.address.toLowerCase();

    try {
      const quote = await router.getFunction("getSwapOut")(
        info.LBPair,
        amountIn,
        swapForY,
        options
      );

      console.log("AVAX input:", "0.001");
      console.log("Input left raw:", quote.amountInLeft.toString());
      console.log("Output raw:", quote.amountOut.toString());
      console.log(
        "Output formatted:",
        ethers.formatUnits(quote.amountOut, 6)
      );
    } catch (error) {
      console.log("Quote unavailable:", String(error));
    }
  }
}

console.log("\nRead-only check completed. No transaction sent.");