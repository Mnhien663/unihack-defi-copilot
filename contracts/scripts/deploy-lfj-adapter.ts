import { network } from "hardhat";
import { readFile, writeFile } from "node:fs/promises";

const { ethers } = await network.create();
const chain = await ethers.provider.getNetwork();
if (chain.chainId !== 43113n) {
  throw new Error("Deployment requires Fuji");
}

const manifestURL = new URL("../deployments/fuji.json", import.meta.url);
const manifest = JSON.parse(await readFile(manifestURL, "utf8"));

if (manifest.chainId !== 43113) {
  throw new Error("Manifest is not Fuji");
}
if (manifest.contracts?.LFJSwapAdapter) {
  throw new Error("LFJSwapAdapter entry exists; inspect before retrying");
}
if (manifest.contracts?.AllowanceManager?.status !== "deployed") {
  throw new Error("Confirmed manager deployment required");
}

const managerAddress = ethers.getAddress(
  manifest.contracts.AllowanceManager.address
);
if (managerAddress !== ethers.getAddress(
  "0xA7c9881eb43902Ca668bb864668eAA3795f2283B"
)) {
  throw new Error("Unexpected manager");
}

const routerAddress = "0x18556DA13313f3532c54711497A8FedAC273220E";
const factoryAddress = "0xb43120c4745967fa9b93E79C149E66B0f2D6Fe0c";
const pairAddress = "0xc3b7307184381f65B398d385a71EF0490239a2b3";
const tokenX = "0xAb231A5744C8E6c45481754928cCfFFFD4aa0732";
const tokenY = "0xB6076C93701D6a07266c31066B298AeC6dd65c2d";

const [deployer] = await ethers.getSigners();
if (!deployer || ethers.getAddress(deployer.address) !== ethers.getAddress(
  "0x4DFb0d91C07A7C706F65F974953cE8b111D27Ee5"
)) {
  throw new Error("Unexpected deployer");
}

const manager = await ethers.getContractAt(
  "AllowanceManager", managerAddress
);
if (ethers.getAddress(await manager.getFunction("policyAdmin")()) !==
    ethers.getAddress(deployer.address)) {
  throw new Error("Deployer is not manager policy admin");
}

const block = await ethers.provider.getBlockNumber();
const at = { blockTag: block };

for (const address of [
  managerAddress, routerAddress, factoryAddress, pairAddress, tokenX, tokenY
]) {
  if (await ethers.provider.getCode(address, block) === "0x") {
    throw new Error(`Contract has no code: ${address}`);
  }
}

const router = new ethers.Contract(routerAddress, [
  "function getFactory() view returns (address)"
], ethers.provider);

if (ethers.getAddress(await router.getFactory(at)) !==
    ethers.getAddress(factoryAddress)) {
  throw new Error("Router factory mismatch");
}

const factoryContract = new ethers.Contract(factoryAddress, [
  "function getLBPairInformation(address,address,uint256) view returns (tuple(uint16 binStep,address LBPair,bool createdByOwner,bool ignoredForRouting))"
], ethers.provider);

const pairInfo = await factoryContract.getLBPairInformation(
  tokenX, tokenY, 1, at
);
if (
  ethers.getAddress(pairInfo.LBPair) !== ethers.getAddress(pairAddress) ||
  pairInfo.binStep !== 1n ||
  pairInfo.ignoredForRouting
) {
  throw new Error("Factory pair does not match quote route");
}

const factory = await ethers.getContractFactory("LFJSwapAdapter", deployer);
const args = [managerAddress, routerAddress, tokenX, tokenY] as const;
const deployment = await factory.getDeployTransaction(...args);
const gas = await deployer.estimateGas(deployment);
const gasLimit = (gas * 120n + 99n) / 100n;
const fees = await ethers.provider.getFeeData();
const maxFeePerGas = fees.maxFeePerGas ?? fees.gasPrice;

if (maxFeePerGas === null) {
  throw new Error("Fee data unavailable");
}

const feeBound = gasLimit * maxFeePerGas;
if (await ethers.provider.getBalance(deployer.address) < feeBound) {
  throw new Error("Insufficient AVAX for estimated deployment");
}

console.log("Manager:", managerAddress);
console.log("Router:", routerAddress);
console.log("Verified pair:", pairAddress);
console.log("Checked block:", block);
console.log("Estimated gas:", gas.toString());
console.log("Fee bound AVAX:", ethers.formatEther(feeBound));

if (process.env.DEPLOY_LFJ_ADAPTER !== "1") {
  console.log("Preflight passed. No transaction sent.");
} else {
  const adapter = await factory.deploy(...args, {
    gasLimit,
    maxFeePerGas,
    maxPriorityFeePerGas: fees.maxPriorityFeePerGas ?? 0n
  });

  const tx = adapter.deploymentTransaction();
  if (!tx) {
    throw new Error("Deployment transaction missing");
  }
  const address = await adapter.getAddress();

  console.log("Adapter transaction:", tx.hash);
  console.log("Expected adapter:", address);

  manifest.contracts.LFJSwapAdapter = {
    address,
    deploymentTransaction: tx.hash,
    deployer: deployer.address,
    manager: managerAddress,
    router: routerAddress,
    tokenX,
    tokenY,
    binStep: 1,
    version: "V2.2",
    verifiedPair: pairAddress,
    status: "pending",
    configurationStatus: "not_configured"
  };

  await writeFile(
    manifestURL, JSON.stringify(manifest, null, 2) + "\n", "utf8"
  );

  const receipt = await tx.wait();
  if (!receipt || receipt.status !== 1) {
    throw new Error("Adapter deployment not confirmed successfully");
  }
  await adapter.waitForDeployment();

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

  manifest.contracts.LFJSwapAdapter.status = "deployed";
  manifest.contracts.LFJSwapAdapter.deploymentBlock = receipt.blockNumber;

  await writeFile(
    manifestURL, JSON.stringify(manifest, null, 2) + "\n", "utf8"
  );

  console.log("LFJSwapAdapter deployed:", address);
  console.log("Block:", receipt.blockNumber);
  console.log("Bindings verified. Swap routes are not configured yet.");
}