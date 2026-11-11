import { network } from "hardhat";
import { readFile, writeFile } from "node:fs/promises";

const { ethers } = await network.create();
const chain = await ethers.provider.getNetwork();
if (chain.chainId !== 43113n) {
  throw new Error("Configuration requires Fuji");
}

const manifestURL = new URL("../deployments/fuji.json", import.meta.url);
const manifest = JSON.parse(await readFile(manifestURL, "utf8"));
if (manifest.chainId !== 43113) {
  throw new Error("Manifest is not Fuji");
}

const entries = manifest.contracts;
if (entries.AllowanceManager?.status !== "deployed" ||
    entries.LFJSwapAdapter?.status !== "deployed") {
  throw new Error("Confirmed manager and LFJ adapter required");
}

const managerAddress = ethers.getAddress(
  entries.AllowanceManager.address
);
const adapterAddress = ethers.getAddress(
  entries.LFJSwapAdapter.address
);

if (managerAddress !== ethers.getAddress(
  "0xA7c9881eb43902Ca668bb864668eAA3795f2283B"
) || adapterAddress !== ethers.getAddress(
  "0xD5cae0dF0f11a0Cf9818aCc47b11ce183b0E8AF5"
)) {
  throw new Error("Unexpected manager or adapter");
}

const routerAddress = "0x18556DA13313f3532c54711497A8FedAC273220E";
const tokenX = "0xAb231A5744C8E6c45481754928cCfFFFD4aa0732";
const tokenY = "0xB6076C93701D6a07266c31066B298AeC6dd65c2d";

const [admin] = await ethers.getSigners();
if (!admin) {
  throw new Error("No signer");
}

const manager = await ethers.getContractAt(
  "AllowanceManager", managerAddress, admin
);
const adapter = await ethers.getContractAt(
  "LFJSwapAdapter", adapterAddress
);

if (ethers.getAddress(await manager.getFunction("policyAdmin")()) !==
    ethers.getAddress(admin.address)) {
  throw new Error("Signer is not policy admin");
}

for (const [method, expected] of [
  ["manager", managerAddress],
  ["router", routerAddress],
  ["tokenX", tokenX],
  ["tokenY", tokenY]
]) {
  if (ethers.getAddress(await adapter.getFunction(method)()) !==
      ethers.getAddress(expected)) {
    throw new Error(`Adapter ${method} mismatch`);
  }
}
if (await adapter.getFunction("BIN_STEP")() !== 1n) {
  throw new Error("Adapter bin step mismatch");
}

const directions = [
  [tokenX, tokenY],
  [tokenY, tokenX]
];

for (const [input, output] of directions) {
  const current = ethers.getAddress(
    await manager.getFunction("swapAdapters")(input, output)
  );
  if (current !== ethers.ZeroAddress && current !== adapterAddress) {
    throw new Error("Different swap adapter configured; refusing overwrite");
  }
  console.log("Route:", input, "->", output, "adapter:", current);
}

for (const token of [tokenX, tokenY]) {
  console.log(
    "Token:", token,
    "enabled:", await manager.getFunction("supportedTokens")(token)
  );
}

if (process.env.CONFIGURE_LFJ !== "1") {
  console.log("Preflight passed. No transaction sent.");
} else {
  const history = entries.AllowanceManager.configurationTransactions ?? [];

  async function sendAndRecord(
    action: string,
    send: () => Promise<any>
  ) {
    const tx = await send();
    console.log(action, "transaction:", tx.hash);

    const entry: {
      action: string;
      hash: string;
      status: string;
      blockNumber: number | null;
    } = {
      action,
      hash: tx.hash,
      status: "pending",
      blockNumber: null
    };

    history.push(entry);
    entries.AllowanceManager.configurationTransactions = history;
    await writeFile(
      manifestURL, JSON.stringify(manifest, null, 2) + "\n", "utf8"
    );

    const receipt = await tx.wait();
    if (!receipt || receipt.status !== 1) {
      throw new Error(`${action} not confirmed successfully`);
    }

    entry.status = "confirmed";
    entry.blockNumber = receipt.blockNumber;
    await writeFile(
      manifestURL, JSON.stringify(manifest, null, 2) + "\n", "utf8"
    );
  }

  for (const [label, token] of [["USDT", tokenX], ["USDC", tokenY]]) {
    if (!await manager.getFunction("supportedTokens")(token)) {
      await sendAndRecord(`enable_lfj_${label}`, () =>
        manager.getFunction("setTokenSupported")(token, true)
      );
    }
  }

  for (const [input, output] of directions) {
    const current = ethers.getAddress(
      await manager.getFunction("swapAdapters")(input, output)
    );
    if (current === ethers.ZeroAddress) {
      await sendAndRecord(`set_swap_route_${input}_${output}`, () =>
        manager.getFunction("setSwapAdapter")(input, output, adapterAddress)
      );
    }
  }

  for (const token of [tokenX, tokenY]) {
    if (!await manager.getFunction("supportedTokens")(token)) {
      throw new Error("Final token support verification failed");
    }
  }
  for (const [input, output] of directions) {
    if (ethers.getAddress(
      await manager.getFunction("swapAdapters")(input, output)
    ) !== adapterAddress) {
      throw new Error("Final swap route verification failed");
    }
  }

  entries.LFJSwapAdapter.configurationStatus = "both_directions_configured";
  entries.AllowanceManager.swapRoutes = directions.map(([input, output]) => ({
    tokenIn: input,
    tokenOut: output,
    adapter: adapterAddress
  }));

  await writeFile(
    manifestURL, JSON.stringify(manifest, null, 2) + "\n", "utf8"
  );

  console.log("Both LFJ swap routes configured and verified.");
}