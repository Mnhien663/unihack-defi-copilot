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
for (const name of ["AllowanceManager", "DemoVault", "VaultAdapter"]) {
  if (entries[name]?.status !== "deployed") {
    throw new Error(`${name} is not marked deployed`);
  }
}

const managerAddress = ethers.getAddress(entries.AllowanceManager.address);
const vaultAddress = ethers.getAddress(entries.DemoVault.address);
const adapterAddress = ethers.getAddress(entries.VaultAdapter.address);
const tokenAddress = ethers.getAddress(entries.DemoToken.address);

const [admin] = await ethers.getSigners();
if (!admin) {
  throw new Error("No signer");
}

const manager = await ethers.getContractAt(
  "AllowanceManager", managerAddress, admin
);
const adapter = await ethers.getContractAt("VaultAdapter", adapterAddress);
const vault = await ethers.getContractAt("DemoVault", vaultAddress);

if (ethers.getAddress(await manager.getFunction("policyAdmin")()) !==
    ethers.getAddress(admin.address)) {
  throw new Error("Signer is not policy admin");
}

for (const [method, expected] of [
  ["manager", managerAddress],
  ["vault", vaultAddress],
  ["underlying", tokenAddress]
]) {
  if (ethers.getAddress(await adapter.getFunction(method)()) !== expected) {
    throw new Error(`Adapter ${method} mismatch`);
  }
}
if (ethers.getAddress(await vault.getFunction("asset")()) !== tokenAddress) {
  throw new Error("Vault asset mismatch");
}

const route = await manager.getFunction("vaultConfigs")(tokenAddress);
const currentAdapter = ethers.getAddress(route.adapter);
const currentShare = ethers.getAddress(route.shareToken);

if (currentAdapter !== ethers.ZeroAddress &&
    (currentAdapter !== adapterAddress || currentShare !== vaultAddress)) {
  throw new Error("A different deposit route exists; refusing to overwrite");
}

const tokenEnabled = await manager.getFunction("supportedTokens")(tokenAddress);
const sharesEnabled = await manager.getFunction("supportedTokens")(vaultAddress);

console.log("Manager:", managerAddress);
console.log("Admin:", admin.address);
console.log("DDEMO enabled:", tokenEnabled);
console.log("Shares enabled:", sharesEnabled);
console.log("Current adapter:", currentAdapter);

if (process.env.CONFIGURE_VAULT !== "1") {
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
      throw new Error(`${action} was not confirmed successfully`);
    }

    entry.status = "confirmed";
    entry.blockNumber = receipt.blockNumber;

    await writeFile(
      manifestURL, JSON.stringify(manifest, null, 2) + "\n", "utf8"
    );
  }

  if (!tokenEnabled) {
    await sendAndRecord("enable_underlying", () =>
      manager.getFunction("setTokenSupported")(tokenAddress, true)
    );
  }

  if (!sharesEnabled) {
    await sendAndRecord("enable_shares", () =>
      manager.getFunction("setTokenSupported")(vaultAddress, true)
    );
  }

  if (currentAdapter === ethers.ZeroAddress) {
    await sendAndRecord("set_vault_route", () =>
      manager.getFunction("setVaultAdapter")(
        tokenAddress, adapterAddress, vaultAddress
      )
    );
  }

  const verifiedRoute = await manager.getFunction("vaultConfigs")(tokenAddress);
  if (
    !await manager.getFunction("supportedTokens")(tokenAddress) ||
    !await manager.getFunction("supportedTokens")(vaultAddress) ||
    ethers.getAddress(verifiedRoute.adapter) !== adapterAddress ||
    ethers.getAddress(verifiedRoute.shareToken) !== vaultAddress
  ) {
    throw new Error("Final configuration verification failed");
  }

  entries.AllowanceManager.configurationStatus = "vault_configured";
  entries.AllowanceManager.vaultRoute = {
    token: tokenAddress,
    adapter: adapterAddress,
    shareToken: vaultAddress
  };

  await writeFile(
    manifestURL, JSON.stringify(manifest, null, 2) + "\n", "utf8"
  );

  console.log("Vault route configured and verified.");
}