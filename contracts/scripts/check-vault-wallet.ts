import { network } from "hardhat";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

const { ethers } = await network.create();

const wallet = "0x37E146a4141C8fd82f0c8c5Bb8035930fD0ea588";
const manager = "0xA7c9881eb43902Ca668bb864668eAA3795f2283B";
const asset = "0xcCCb83Fbe349fAebDE74Ce633D25B282E6FDd21A";
const vault = "0x896C05deae3d6e35021B12DD361401736Bb8aAda";

const chain = await ethers.provider.getNetwork();
if (chain.chainId !== 43113n) {
  throw new Error("Expected Fuji chain ID 43113");
}

const block = await ethers.provider.getBlock("latest");
if (!block) throw new Error("Latest block missing");
const options = { blockTag: block.number };

const token = new ethers.Contract(asset, [
  "function owner() view returns (address)",
  "function decimals() view returns (uint8)",
  "function balanceOf(address) view returns (uint256)",
  "function allowance(address,address) view returns (uint256)",
], ethers.provider);

const vaultContract = new ethers.Contract(vault, [
  "function asset() view returns (address)",
  "function decimals() view returns (uint8)",
  "function balanceOf(address) view returns (uint256)",
  "function previewDeposit(uint256) view returns (uint256)",
  "function maxDeposit(address) view returns (uint256)",
  "function maxRedeem(address) view returns (uint256)",
], ethers.provider);

const managerContract = new ethers.Contract(manager, [
  "function remaining(address,address) view returns (uint256)",
  "function supportedTokens(address) view returns (bool)",
  "function vaultConfigs(address) view returns (address adapter,address shareToken)",
], ethers.provider);

const underlying = await vaultContract.getFunction("asset")(options);
if (underlying.toLowerCase() !== asset.toLowerCase()) {
  throw new Error("Vault underlying mismatch");
}

const assetDecimals = Number(
  await token.getFunction("decimals")(options)
);
const shareDecimals = Number(
  await vaultContract.getFunction("decimals")(options)
);
if (assetDecimals !== 18 || shareDecimals !== 18) {
  throw new Error("Unexpected asset/share decimals");
}

const amount = ethers.parseUnits("0.1", assetDecimals);
const balance = await token.getFunction("balanceOf")(wallet, options);
const allowance = await token.getFunction("allowance")(
  wallet, manager, options
);
const budget = await managerContract.getFunction("remaining")(
  wallet, asset, options
);
const shares = await vaultContract.getFunction("balanceOf")(
  wallet, options
);
const preview = await vaultContract.getFunction("previewDeposit")(
  amount, options
);
const maximum = await vaultContract.getFunction("maxDeposit")(
  wallet, options
);
const route = await managerContract.getFunction("vaultConfigs")(
  asset, options
);

console.log("Chain ID:", chain.chainId.toString());
console.log("Block:", block.number);
console.log("Wallet:", wallet);
console.log("Token owner:", await token.getFunction("owner")(options));
console.log("DDEMO balance raw:", balance.toString());
console.log("DDEMO balance:", ethers.formatUnits(balance, assetDecimals));
console.log("Allowance to Manager raw:", allowance.toString());
console.log("Effective budget raw:", budget.toString());
console.log("Vault shares raw:", shares.toString());
console.log("Vault shares:", ethers.formatUnits(shares, shareDecimals));
console.log("Test deposit amount raw:", amount.toString());
console.log("Preview shares raw:", preview.toString());
console.log("Max deposit raw:", maximum.toString());
console.log(
  "Max redeem raw:",
  (await vaultContract.getFunction("maxRedeem")(wallet, options)).toString()
);
console.log("Configured adapter:", route.adapter);
console.log("Configured share token:", route.shareToken);
console.log(
  "Underlying enabled:",
  await managerContract.getFunction("supportedTokens")(asset, options)
);
console.log(
  "Shares enabled:",
  await managerContract.getFunction("supportedTokens")(vault, options)
);
console.log("Read-only check completed. No transaction sent.");

const approvalHash =
  "0x049d5322ffa2ed60cc4f039ef56acc5a99e43e79a6288c8582ea2bba2bf7693d";

const approvalTx =
  await ethers.provider.getTransaction(approvalHash);
const approvalReceipt =
  await ethers.provider.getTransactionReceipt(approvalHash);

if (!approvalTx || !approvalReceipt) {
  throw new Error("Approval chưa có transaction/receipt. Không gửi lại.");
}
if (approvalReceipt.status !== 1) {
  throw new Error("Approval đã revert.");
}
if (
  approvalTx.chainId !== 43113n ||
  approvalTx.from.toLowerCase() !== wallet.toLowerCase() ||
  approvalTx.to?.toLowerCase() !== asset.toLowerCase() ||
  approvalTx.value !== 0n
) {
  throw new Error("Approval transaction không khớp dự kiến.");
}

const approvalInterface = new ethers.Interface([
  "function approve(address spender,uint256 amount) returns (bool)",
]);
const approvalArgs = approvalInterface.decodeFunctionData(
  "approve", approvalTx.data
);
if (
  approvalArgs[0].toLowerCase() !== manager.toLowerCase() ||
  approvalArgs[1] !== amount
) {
  throw new Error("Approval sai spender hoặc amount.");
}

console.log("\nDDEMO approval transaction:", approvalHash);
console.log("Approval receipt status: success");
console.log("Approval mined block:", approvalReceipt.blockNumber);
console.log("Approval transaction matches expected fields: true");

const budgetHash =
  "0x8a83ab970a6995763432dbc8e30afa119e280e01c627452cf49cceb5b4c8d32d";

const budgetTx =
  await ethers.provider.getTransaction(budgetHash);
const budgetReceipt =
  await ethers.provider.getTransactionReceipt(budgetHash);

if (!budgetTx || !budgetReceipt) {
  throw new Error("Budget chưa có transaction/receipt. Không gửi lại.");
}
if (budgetReceipt.status !== 1) {
  throw new Error("setBudget đã revert.");
}
if (
  budgetTx.chainId !== 43113n ||
  budgetTx.from.toLowerCase() !== wallet.toLowerCase() ||
  budgetTx.to?.toLowerCase() !== manager.toLowerCase() ||
  budgetTx.value !== 0n
) {
  throw new Error("Budget transaction không khớp dự kiến.");
}

const budgetInterface = new ethers.Interface([
  "function setBudget(address token,uint256 amount,uint256 expiry)",
]);
const budgetArgs = budgetInterface.decodeFunctionData(
  "setBudget", budgetTx.data
);
if (
  budgetArgs[0].toLowerCase() !== asset.toLowerCase() ||
  budgetArgs[1] !== amount ||
  budgetArgs[2] !== 1794496527n
) {
  throw new Error("Budget sai token, amount hoặc expiry.");
}

console.log("\nDDEMO budget transaction:", budgetHash);
console.log("Budget receipt status: success");
console.log("Budget mined block:", budgetReceipt.blockNumber);
console.log("Budget expiry:", budgetArgs[2].toString());
console.log("Budget transaction matches expected fields: true");

const redeemEvidence = JSON.parse(readFileSync(
  new URL(
    "../../backend/internal/avalanche/testdata/redeem_fuji_expected_intent.json",
    import.meta.url
  ),
  "utf8"
));
const redeemIntent = redeemEvidence.expected_intent;
const redeemHash = redeemEvidence.transaction_hash;

const redeemTx = await ethers.provider.getTransaction(redeemHash);
const redeemReceipt =
  await ethers.provider.getTransactionReceipt(redeemHash);

assert.ok(redeemTx, "Không tìm thấy redeem transaction");
assert.ok(redeemReceipt, "Chưa có redeem receipt. Không gửi lại.");
assert.equal(redeemReceipt.status, 1, "Redeem đã revert");

const sameAddress = (a: string, b: string) =>
  assert.equal(a.toLowerCase(), b.toLowerCase());

assert.equal(redeemTx.hash.toLowerCase(), redeemHash.toLowerCase());
assert.equal(redeemReceipt.hash.toLowerCase(), redeemHash.toLowerCase());
assert.equal(redeemTx.chainId, 43113n);
assert.equal(redeemIntent.tx.chain_id, 43113);
sameAddress(redeemTx.from, wallet);
sameAddress(redeemTx.from, redeemIntent.tx.from);
assert.ok(redeemTx.to);
sameAddress(redeemTx.to, vault);
sameAddress(redeemTx.to, redeemIntent.tx.to);
sameAddress(redeemIntent.vault_address, vault);
sameAddress(redeemIntent.asset_address, asset);
sameAddress(redeemIntent.owner_address, wallet);
sameAddress(redeemIntent.receiver_address, wallet);
assert.equal(redeemTx.value, 0n);
assert.equal(BigInt(redeemIntent.tx.value), 0n);

const redeemInterface = new ethers.Interface([
  "function redeem(uint256 shares,address receiver,address owner) returns (uint256 assets)",
  "event Withdraw(address indexed sender,address indexed receiver,address indexed owner,uint256 assets,uint256 shares)",
  "event Transfer(address indexed from,address indexed to,uint256 value)",
]);

const expectedShares = BigInt(redeemIntent.shares_raw);
assert.equal(expectedShares, ethers.parseUnits("0.1", 18));

const expectedData = redeemInterface.encodeFunctionData(
  "redeem", [expectedShares, wallet, wallet]
);
assert.equal(redeemTx.data.toLowerCase(), expectedData.toLowerCase());
assert.equal(
  redeemIntent.tx.data.toLowerCase(),
  expectedData.toLowerCase()
);

const withdrawEvent = redeemInterface.getEvent("Withdraw");
const transferEvent = redeemInterface.getEvent("Transfer");
assert.ok(withdrawEvent);
assert.ok(transferEvent);

const withdrawals = redeemReceipt.logs.filter(log =>
  log.address.toLowerCase() === vault.toLowerCase() &&
  log.topics[0] === withdrawEvent.topicHash
);
assert.equal(withdrawals.length, 1, "Cần đúng một vault Withdraw");
const withdrawal = redeemInterface.parseLog(withdrawals[0]);
assert.ok(withdrawal);
sameAddress(withdrawal.args.sender, wallet);
sameAddress(withdrawal.args.receiver, wallet);
sameAddress(withdrawal.args.owner, wallet);
assert.equal(withdrawal.args.shares, expectedShares);
assert.ok(withdrawal.args.assets > 0n);

const burns = redeemReceipt.logs.filter(log =>
  log.address.toLowerCase() === vault.toLowerCase() &&
  log.topics[0] === transferEvent.topicHash
);
assert.equal(burns.length, 1, "Cần đúng một share Transfer");
const burn = redeemInterface.parseLog(burns[0]);
assert.ok(burn);
sameAddress(burn.args.from, wallet);
sameAddress(burn.args.to, ethers.ZeroAddress);
assert.equal(burn.args.value, expectedShares);

const transfers = redeemReceipt.logs.filter(log =>
  log.address.toLowerCase() === asset.toLowerCase() &&
  log.topics[0] === transferEvent.topicHash
);
assert.equal(transfers.length, 1, "Cần đúng một DDEMO Transfer");
const received = redeemInterface.parseLog(transfers[0]);
assert.ok(received);
sameAddress(received.args.from, vault);
sameAddress(received.args.to, wallet);
assert.equal(received.args.value, withdrawal.args.assets);

// Compare block-end state before and after the redeem block.
const beforeOptions = { blockTag: redeemReceipt.blockNumber - 1 };
const afterOptions = { blockTag: redeemReceipt.blockNumber };

const sharesBefore = await vaultContract.getFunction("balanceOf")(
  wallet, beforeOptions
);
const sharesAfter = await vaultContract.getFunction("balanceOf")(
  wallet, afterOptions
);
const assetsBefore = await token.getFunction("balanceOf")(
  wallet, beforeOptions
);
const assetsAfter = await token.getFunction("balanceOf")(
  wallet, afterOptions
);

assert.equal(sharesBefore - sharesAfter, expectedShares);
assert.equal(assetsAfter - assetsBefore, withdrawal.args.assets);

console.log("\nRedeem transaction:", redeemHash);
console.log("Redeem receipt status: success");
console.log("Redeem mined block:", redeemReceipt.blockNumber);
console.log("Redeem transaction and events verified: true");
console.log("Shares burned raw:", expectedShares.toString());
console.log("DDEMO received raw:", withdrawal.args.assets.toString());
console.log("DDEMO received:", ethers.formatUnits(withdrawal.args.assets, 18));
console.log("Shares after redeem raw:", sharesAfter.toString());
console.log("DDEMO after redeem raw:", assetsAfter.toString());