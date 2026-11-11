import { network } from "hardhat";
import { readFile, writeFile } from "node:fs/promises";

const { ethers } = await network.create();

const chain = await ethers.provider.getNetwork();
if (chain.chainId !== 43113n) {
  throw new Error("Deployment requires Avalanche Fuji (43113)");
}

const manifestURL = new URL("../deployments/fuji.json", import.meta.url);
const manifest = JSON.parse(await readFile(manifestURL, "utf8"));

if (manifest.chainId !== 43113) {
  throw new Error("Manifest chain ID is not Fuji");
}
if (manifest.contracts?.DemoVault?.address) {
  throw new Error("DemoVault already exists in manifest; inspect before deploying");
}

const underlying = ethers.getAddress(
  manifest.contracts.DemoToken.address
);
const expectedToken = ethers.getAddress(
  "0xcCCb83Fbe349fAebDE74Ce633D25B282E6FDd21A"
);

if (underlying !== expectedToken) {
  throw new Error("Manifest DemoToken differs from selected underlying");
}
if (await ethers.provider.getCode(underlying) === "0x") {
  throw new Error("Underlying token has no contract code");
}

const token = new ethers.Contract(underlying, [
  "function decimals() view returns (uint8)",
  "function symbol() view returns (string)"
], ethers.provider);

const decimals = await token.decimals();
const symbol = await token.symbol();
if (decimals !== 18n || symbol !== "DDEMO") {
  throw new Error("Unexpected DemoToken metadata");
}

const [deployer] = await ethers.getSigners();
if (!deployer) {
  throw new Error("No Fuji deployer configured");
}

const expectedDeployer = ethers.getAddress(
  "0x4DFb0d91C07A7C706F65F974953cE8b111D27Ee5"
);
if (ethers.getAddress(deployer.address) !== expectedDeployer) {
  throw new Error("Configured signer differs from the selected deployer");
}

const factory = await ethers.getContractFactory("DemoVault", deployer);
const deployment = await factory.getDeployTransaction(underlying);
const gas = await deployer.estimateGas(deployment);
const fees = await ethers.provider.getFeeData();
const feePerGas = fees.maxFeePerGas ?? fees.gasPrice;

if (feePerGas === null) {
  throw new Error("RPC did not return fee data");
}

const estimatedFeeBound = gas * feePerGas;
const balance = await ethers.provider.getBalance(deployer.address);

console.log("Chain ID:", chain.chainId.toString());
console.log("Deployer:", deployer.address);
console.log("Underlying:", underlying);
console.log("Token:", symbol, "decimals:", decimals.toString());
console.log("Balance AVAX:", ethers.formatEther(balance));
console.log("Estimated deployment gas:", gas.toString());
console.log(
  "Estimated fee bound AVAX:",
  ethers.formatEther(estimatedFeeBound)
);

if (balance < estimatedFeeBound) {
  throw new Error("Insufficient AVAX for the estimated deployment fee");
}

if (process.env.DEPLOY_DEMO_VAULT !== "1") {
  console.log("Preflight passed. No transaction sent.");
} else {
  const gasLimit = (gas * 120n + 99n) / 100n;
  const feeBound = gasLimit * feePerGas;

  if (balance < feeBound) {
    throw new Error("Insufficient AVAX including gas buffer");
  }

  const vault = await factory.deploy(underlying, {
    gasLimit,
    maxFeePerGas: feePerGas,
    maxPriorityFeePerGas: fees.maxPriorityFeePerGas ?? 0n
  });

  const tx = vault.deploymentTransaction();
  if (!tx) {
    throw new Error("Deployment transaction is missing");
  }

  const vaultAddress = await vault.getAddress();
  console.log("Deployment transaction:", tx.hash);
  console.log("Expected vault address:", vaultAddress);

  // Record the broadcast before waiting, so a retry does not deploy twice.
  manifest.contracts.DemoVault = {
    address: vaultAddress,
    deploymentTransaction: tx.hash,
    deployer: deployer.address,
    underlying,
    name: "DeFi Copilot Demo Vault Share",
    symbol: "dvDDEMO",
    demo: true,
    status: "pending",
    yieldStrategy: null,
    apy: null
  };

  await writeFile(
    manifestURL,
    JSON.stringify(manifest, null, 2) + "\n",
    "utf8"
  );

  const receipt = await tx.wait();
  if (!receipt || receipt.status !== 1) {
    throw new Error("Deployment was not confirmed successfully");
  }

  await vault.waitForDeployment();

  const deployedAsset = ethers.getAddress(
    await vault.getFunction("asset")()
  );
  if (deployedAsset !== underlying) {
    throw new Error("Deployed vault underlying does not match");
  }

  manifest.contracts.DemoVault.status = "deployed";
  manifest.contracts.DemoVault.deploymentBlock = receipt.blockNumber;
  manifest.contracts.DemoVault.decimals = Number(
    await vault.getFunction("decimals")()
  );

  await writeFile(
    manifestURL,
    JSON.stringify(manifest, null, 2) + "\n",
    "utf8"
  );

  console.log("DemoVault deployed:", vaultAddress);
  console.log("Underlying verified:", deployedAsset);
  console.log("Deployment block:", receipt.blockNumber);
  console.log("Manifest updated.");
}