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
if (manifest.contracts?.AllowanceManager) {
  throw new Error("Manager entry already exists; inspect before retrying");
}

const [deployer] = await ethers.getSigners();
if (!deployer || ethers.getAddress(deployer.address) !==
    ethers.getAddress("0x4DFb0d91C07A7C706F65F974953cE8b111D27Ee5")) {
  throw new Error("Unexpected deployer");
}

const vaultAddress = ethers.getAddress(
  manifest.contracts.DemoVault.address
);
const tokenAddress = ethers.getAddress(
  manifest.contracts.DemoToken.address
);

if (manifest.contracts.DemoVault.status !== "deployed") {
  throw new Error("Vault deployment is not marked confirmed");
}
if (await ethers.provider.getCode(vaultAddress) === "0x") {
  throw new Error("Vault has no code");
}

const vault = await ethers.getContractAt("DemoVault", vaultAddress);
if (ethers.getAddress(await vault.getFunction("asset")()) !== tokenAddress) {
  throw new Error("Vault underlying does not match manifest");
}

const factory = await ethers.getContractFactory(
  "AllowanceManager", deployer
);
const deployment = await factory.getDeployTransaction();
const estimatedGas = await deployer.estimateGas(deployment);
const gasLimit = (estimatedGas * 120n + 99n) / 100n;

const fees = await ethers.provider.getFeeData();
const maxFeePerGas = fees.maxFeePerGas ?? fees.gasPrice;
if (maxFeePerGas === null) {
  throw new Error("Fee data unavailable");
}

const balance = await ethers.provider.getBalance(deployer.address);
const feeBound = gasLimit * maxFeePerGas;

console.log("Deployer:", deployer.address);
console.log("Existing vault:", vaultAddress);
console.log("Estimated gas:", estimatedGas.toString());
console.log("Fee bound with gas buffer AVAX:", ethers.formatEther(feeBound));

if (balance < feeBound) {
  throw new Error("Insufficient AVAX for estimated deployment");
}

if (process.env.DEPLOY_MANAGER !== "1") {
  console.log("Preflight passed. No transaction sent.");
} else {
  const manager = await factory.deploy({
    gasLimit,
    maxFeePerGas,
    maxPriorityFeePerGas: fees.maxPriorityFeePerGas ?? 0n
  });

  const tx = manager.deploymentTransaction();
  if (!tx) {
    throw new Error("Deployment transaction missing");
  }

  const address = await manager.getAddress();

  console.log("Manager transaction:", tx.hash);
  console.log("Expected manager:", address);

  manifest.contracts.AllowanceManager = {
    address,
    deploymentTransaction: tx.hash,
    deployer: deployer.address,
    policyAdmin: deployer.address,
    status: "pending",
    configurationStatus: "not_configured"
  };

  await writeFile(
    manifestURL, JSON.stringify(manifest, null, 2) + "\n", "utf8"
  );

  const receipt = await tx.wait();
  if (!receipt || receipt.status !== 1) {
    throw new Error("Manager deployment not confirmed successfully");
  }
  await manager.waitForDeployment();

  const admin = await manager.getFunction("policyAdmin")();
  if (ethers.getAddress(admin) !== ethers.getAddress(deployer.address)) {
    throw new Error("Unexpected manager policy admin");
  }

  manifest.contracts.AllowanceManager.status = "deployed";
  manifest.contracts.AllowanceManager.deploymentBlock = receipt.blockNumber;

  await writeFile(
    manifestURL, JSON.stringify(manifest, null, 2) + "\n", "utf8"
  );

  console.log("AllowanceManager deployed:", address);
  console.log("Policy admin:", admin);
  console.log("Block:", receipt.blockNumber);
  console.log("Manifest updated. Routes are not configured yet.");
}