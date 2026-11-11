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
if (manifest.contracts?.VaultAdapter) {
  throw new Error("VaultAdapter entry exists; inspect before retrying");
}

const managerEntry = manifest.contracts.AllowanceManager;
const vaultEntry = manifest.contracts.DemoVault;

if (managerEntry?.status !== "deployed" ||
    vaultEntry?.status !== "deployed") {
  throw new Error("Manager and vault must be confirmed deployments");
}

const managerAddress = ethers.getAddress(managerEntry.address);
const vaultAddress = ethers.getAddress(vaultEntry.address);
const tokenAddress = ethers.getAddress(
  manifest.contracts.DemoToken.address
);

const [deployer] = await ethers.getSigners();
if (!deployer || ethers.getAddress(deployer.address) !==
    ethers.getAddress("0x4DFb0d91C07A7C706F65F974953cE8b111D27Ee5")) {
  throw new Error("Unexpected deployer");
}

for (const address of [managerAddress, vaultAddress, tokenAddress]) {
  if (await ethers.provider.getCode(address) === "0x") {
    throw new Error(`Contract has no code: ${address}`);
  }
}

const manager = await ethers.getContractAt(
  "AllowanceManager", managerAddress
);
if (ethers.getAddress(await manager.getFunction("policyAdmin")()) !==
    ethers.getAddress(deployer.address)) {
  throw new Error("Deployer is not manager policy admin");
}

const vault = await ethers.getContractAt("DemoVault", vaultAddress);
if (ethers.getAddress(await vault.getFunction("asset")()) !== tokenAddress) {
  throw new Error("Unexpected vault underlying");
}

const factory = await ethers.getContractFactory("VaultAdapter", deployer);
const deployment = await factory.getDeployTransaction(
  managerAddress, vaultAddress
);
const estimatedGas = await deployer.estimateGas(deployment);
const gasLimit = (estimatedGas * 120n + 99n) / 100n;

const fees = await ethers.provider.getFeeData();
const maxFeePerGas = fees.maxFeePerGas ?? fees.gasPrice;
if (maxFeePerGas === null) {
  throw new Error("Fee data unavailable");
}

const feeBound = gasLimit * maxFeePerGas;
const balance = await ethers.provider.getBalance(deployer.address);

console.log("Manager:", managerAddress);
console.log("Vault:", vaultAddress);
console.log("Underlying:", tokenAddress);
console.log("Estimated gas:", estimatedGas.toString());
console.log("Fee bound AVAX:", ethers.formatEther(feeBound));

if (balance < feeBound) {
  throw new Error("Insufficient AVAX for estimated deployment");
}

if (process.env.DEPLOY_VAULT_ADAPTER !== "1") {
  console.log("Preflight passed. No transaction sent.");
} else {
  const adapter = await factory.deploy(managerAddress, vaultAddress, {
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

  manifest.contracts.VaultAdapter = {
    address,
    deploymentTransaction: tx.hash,
    deployer: deployer.address,
    manager: managerAddress,
    vault: vaultAddress,
    underlying: tokenAddress,
    demo: true,
    status: "pending"
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
    ["vault", vaultAddress],
    ["underlying", tokenAddress]
  ]) {
    const actual = ethers.getAddress(await adapter.getFunction(method)());
    if (actual !== expected) {
      throw new Error(`Adapter ${method} mismatch`);
    }
  }

  manifest.contracts.VaultAdapter.status = "deployed";
  manifest.contracts.VaultAdapter.deploymentBlock = receipt.blockNumber;

  await writeFile(
    manifestURL, JSON.stringify(manifest, null, 2) + "\n", "utf8"
  );

  console.log("VaultAdapter deployed:", address);
  console.log("Block:", receipt.blockNumber);
  console.log("Bindings verified. Manager route is not configured yet.");
}