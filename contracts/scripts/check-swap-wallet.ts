import { network } from "hardhat";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

const { ethers } = await network.create();

const wallet = "0x37E146a4141C8fd82f0c8c5Bb8035930fD0ea588";
const manager = "0xA7c9881eb43902Ca668bb864668eAA3795f2283B";

const chain = await ethers.provider.getNetwork();
if (chain.chainId !== 43113n) {
  throw new Error("Expected Avalanche Fuji, chain ID 43113");
}

const block = await ethers.provider.getBlock("latest");
if (!block) {
  throw new Error("Latest block missing");
}

console.log("Chain ID:", chain.chainId.toString());
console.log("Block:", block.number);
console.log("Wallet:", wallet);
console.log(
  "AVAX:",
  ethers.formatEther(
    await ethers.provider.getBalance(wallet, block.number)
  )
);

const erc20ABI = [
  "function balanceOf(address) view returns (uint256)",
  "function allowance(address,address) view returns (uint256)",
];

const managerContract = new ethers.Contract(
  manager,
  ["function remaining(address,address) view returns (uint256)"],
  ethers.provider
);

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
  const contract = new ethers.Contract(
    token.address,
    erc20ABI,
    ethers.provider
  );
  const options = { blockTag: block.number };

  const balance = await contract.getFunction("balanceOf")(
    wallet, options
  );
  const allowance = await contract.getFunction("allowance")(
    wallet, manager, options
  );
  const remaining = await managerContract.getFunction("remaining")(
    wallet, token.address, options
  );

  console.log(`\n${token.symbol}: ${token.address}`);
  const ownable = new ethers.Contract(
  token.address,
  ["function owner() view returns (address)"],
  ethers.provider
);

console.log(
  "Token owner:",
  await ownable.getFunction("owner")({
    blockTag: block.number,
  })
);
  console.log("Balance raw:", balance.toString());
  console.log("Balance formatted:", ethers.formatUnits(balance, 6));
  console.log("Allowance to Manager raw:", allowance.toString());
  console.log("Effective budget raw:", remaining.toString());
}

console.log("\nRead-only check completed. No transaction sent.");

const fundingHash =
  "0x7b3733e501295963031be91dd37549fcac3d47db6d64b086b7647b19e8d31784";

const receipt = await ethers.provider.getTransactionReceipt(fundingHash);

console.log("\nFunding transaction:", fundingHash);
console.log(
  "Receipt status:",
  receipt === null
    ? "pending / chưa có receipt"
    : receipt.status === 1
      ? "success"
      : "failed"
);
console.log("Mined block:", receipt?.blockNumber ?? null);

const approvalHash =
  "0x307ccc3256984a1413f5ae21edb62d402ab4fdcb088cedd5ccb2996b0d74f318";

const approvalReceipt =
  await ethers.provider.getTransactionReceipt(approvalHash);

console.log("\nApproval transaction:", approvalHash);
console.log(
  "Approval receipt status:",
  approvalReceipt === null
    ? "pending / chưa có receipt"
    : approvalReceipt.status === 1
      ? "success"
      : "failed"
);
console.log(
  "Approval mined block:",
  approvalReceipt?.blockNumber ?? null
);

const budgetHash =
  "0x4b71380d080732ce60a57fcd52a283bd2cc5bc7b9de9bfb6a460d9d4ef6d5f45";

const budgetReceipt =
  await ethers.provider.getTransactionReceipt(budgetHash);

console.log("\nBudget transaction:", budgetHash);
console.log(
  "Budget receipt status:",
  budgetReceipt === null
    ? "pending / chưa có receipt"
    : budgetReceipt.status === 1
      ? "success"
      : "failed"
);
console.log("Budget mined block:", budgetReceipt?.blockNumber ?? null);

const evidence = JSON.parse(
  readFileSync(
    new URL(
      "../../backend/internal/avalanche/testdata/swap_fuji_expected_intent.json",
      import.meta.url
    ),
    "utf8"
  )
);

const intent = evidence.expected_intent;
const swapHash = evidence.transaction_hash;

const swapTx = await ethers.provider.getTransaction(swapHash);
const swapReceipt =
  await ethers.provider.getTransactionReceipt(swapHash);

assert.ok(swapTx, "Không tìm thấy giao dịch swap");
assert.ok(swapReceipt, "Swap chưa có receipt; không gửi lại");
assert.equal(swapReceipt.status, 1, "Swap đã revert");

const sameAddress = (actual: string, expected: string) => {
  assert.equal(actual.toLowerCase(), expected.toLowerCase());
};

assert.equal(swapTx.hash.toLowerCase(), swapHash.toLowerCase());
assert.equal(swapReceipt.hash.toLowerCase(), swapHash.toLowerCase());
assert.equal(swapTx.chainId, 43113n);
assert.equal(intent.tx.chain_id, 43113);
sameAddress(swapTx.from, wallet);
sameAddress(swapTx.from, intent.tx.from);
assert.ok(swapTx.to);
sameAddress(swapTx.to, manager);
sameAddress(swapTx.to, intent.tx.to);
assert.equal(swapTx.value, 0n);
assert.equal(swapTx.value, BigInt(intent.tx.value));
assert.equal(
  swapTx.data.toLowerCase(),
  intent.tx.data.toLowerCase()
);

const swapInterface = new ethers.Interface([
  "function executeSwap(address tokenIn,address tokenOut,uint256 amountIn,uint256 minAmountOut,uint256 deadline) returns(uint256 amountOut)",
  "event SwapExecuted(address indexed owner,address indexed tokenIn,address indexed tokenOut,address adapter,uint256 amountIn,uint256 amountOut)",
]);

const args = swapInterface.decodeFunctionData(
  "executeSwap",
  swapTx.data
);

sameAddress(args[0], intent.token_in_address);
sameAddress(args[1], intent.token_out_address);
assert.equal(args[2], BigInt(intent.amount_in_raw));
assert.equal(args[3], BigInt(intent.min_amount_out_raw));
assert.equal(args[4], BigInt(intent.deadline));

const swapEvent = swapInterface.getEvent("SwapExecuted");
assert.ok(swapEvent);

const matchingLogs = swapReceipt.logs.filter(
  (log) =>
    log.address.toLowerCase() === manager.toLowerCase() &&
    log.topics[0]?.toLowerCase() ===
      swapEvent.topicHash.toLowerCase()
);

assert.equal(
  matchingLogs.length,
  1,
  "Cần đúng một event SwapExecuted từ Manager"
);

const parsed = swapInterface.parseLog(matchingLogs[0]);
assert.ok(parsed);

sameAddress(parsed.args.owner, wallet);
sameAddress(parsed.args.tokenIn, intent.token_in_address);
sameAddress(parsed.args.tokenOut, intent.token_out_address);
sameAddress(parsed.args.adapter, intent.adapter_address);
assert.equal(
  parsed.args.amountIn,
  BigInt(intent.amount_in_raw)
);
assert.ok(
  parsed.args.amountOut >= BigInt(intent.min_amount_out_raw),
  "Output thấp hơn minimum"
);

console.log("\nManager swap transaction:", swapHash);
console.log("Swap receipt status: success");
console.log("Swap mined block:", swapReceipt.blockNumber);
console.log("Transaction matches expected intent: true");
console.log("Manager SwapExecuted event verified: true");
console.log("Amount in raw:", parsed.args.amountIn.toString());
console.log("Amount out raw:", parsed.args.amountOut.toString());
console.log(
  "Amount out formatted:",
  ethers.formatUnits(parsed.args.amountOut, 6)
);