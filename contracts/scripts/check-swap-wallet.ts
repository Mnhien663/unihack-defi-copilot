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

const dev1FundingHash =
  "0x62e194b3af325a2cf5cd6485983a5a9320e561c36accd9e9f3eee9705cf66786";

const dev1FundingTx =
  await ethers.provider.getTransaction(dev1FundingHash);
const dev1FundingReceipt =
  await ethers.provider.getTransactionReceipt(dev1FundingHash);

assert.ok(dev1FundingTx, "Không tìm thấy funding transaction");
assert.ok(dev1FundingReceipt, "Chưa có receipt. Không gửi lại.");
assert.equal(dev1FundingReceipt.status, 1, "Funding đã revert");
assert.equal(dev1FundingTx.chainId, 43113n);
assert.equal(dev1FundingTx.from.toLowerCase(), wallet.toLowerCase());
assert.equal(
  dev1FundingTx.to?.toLowerCase(),
  "0xb4315e873dbcf96ffd0acd8ea43f689d8c20fb30"
);
assert.equal(dev1FundingTx.value, ethers.parseEther("0.08"));

const lfjUsdt = new ethers.Contract(
  "0xAb231A5744C8E6c45481754928cCfFFFD4aa0732",
  ["function balanceOf(address) view returns (uint256)"],
  ethers.provider
);
const fundedBalance = await lfjUsdt.getFunction("balanceOf")(
  wallet, { blockTag: dev1FundingReceipt.blockNumber }
);
assert.ok(fundedBalance >= 1000000n, "USDT vẫn chưa đủ 1 token");

console.log("\nDev1 funding transaction:", dev1FundingHash);
console.log("Funding receipt status: success");
console.log("Funding mined block:", dev1FundingReceipt.blockNumber);
console.log("Funded USDT balance raw:", fundedBalance.toString());
console.log("Funded USDT balance:", ethers.formatUnits(fundedBalance, 6));

const dev1ApprovalHash =
  "0xed824e22c2b4f700a4908c8c97957c44e8ebb7ea3b77b63469819f62f46ff83d";

const dev1ApprovalTx =
  await ethers.provider.getTransaction(dev1ApprovalHash);
const dev1ApprovalReceipt =
  await ethers.provider.getTransactionReceipt(dev1ApprovalHash);

assert.ok(dev1ApprovalTx, "Không tìm thấy approval");
assert.ok(dev1ApprovalReceipt, "Chưa có receipt. Không gửi lại.");
assert.equal(dev1ApprovalReceipt.status, 1, "Approval đã revert");
assert.equal(dev1ApprovalTx.chainId, 43113n);
assert.equal(dev1ApprovalTx.from.toLowerCase(), wallet.toLowerCase());
assert.equal(
  dev1ApprovalTx.to?.toLowerCase(),
  "0xab231a5744c8e6c45481754928ccffffd4aa0732"
);
assert.equal(dev1ApprovalTx.value, 0n);

const dev1ApprovalInterface = new ethers.Interface([
  "function approve(address spender,uint256 amount) returns (bool)",
]);
assert.equal(
  dev1ApprovalTx.data.toLowerCase(),
  dev1ApprovalInterface.encodeFunctionData(
    "approve", [manager, 1000000n]
  ).toLowerCase()
);

const dev1Usdt = new ethers.Contract(
  "0xAb231A5744C8E6c45481754928cCfFFFD4aa0732",
  ["function allowance(address,address) view returns (uint256)"],
  ethers.provider
);
const dev1Allowance = await dev1Usdt.getFunction("allowance")(
  wallet, manager,
  { blockTag: dev1ApprovalReceipt.blockNumber }
);
assert.equal(dev1Allowance, 1000000n);

console.log("\nDev1 approval transaction:", dev1ApprovalHash);
console.log("Approval receipt status: success");
console.log("Approval mined block:", dev1ApprovalReceipt.blockNumber);
console.log("Verified allowance raw:", dev1Allowance.toString());

// Verify the 1 USDT budget prepared for Dev1.
const dev1BudgetHash =
  "0x2968f59399cc9a2f77736965f958aa80dc759bcfdbaedba08ecd9714e0679d5a";

const dev1ReadyWallet =
  "0x37E146a4141C8fd82f0c8c5Bb8035930fD0ea588";
const dev1ReadyManager =
  "0xA7c9881eb43902Ca668bb864668eAA3795f2283B";
const dev1ReadyToken =
  "0xAb231A5744C8E6c45481754928cCfFFFD4aa0732";

const dev1RequiredAmount = 1_000_000n;
const dev1ExpectedExpiry = 1794499480n;

const dev1BudgetReceipt =
  await ethers.provider.getTransactionReceipt(dev1BudgetHash);
const dev1BudgetTx =
  await ethers.provider.getTransaction(dev1BudgetHash);

assert(dev1BudgetReceipt, "Dev1 budget receipt not found");
assert(dev1BudgetTx, "Dev1 budget transaction not found");
assert.equal(dev1BudgetReceipt.status, 1, "Budget transaction reverted");
assert.equal(dev1BudgetTx.chainId, 43113n);
assert.equal(
  dev1BudgetTx.from.toLowerCase(),
  dev1ReadyWallet.toLowerCase()
);
assert.equal(
  dev1BudgetTx.to?.toLowerCase(),
  dev1ReadyManager.toLowerCase()
);
assert.equal(dev1BudgetTx.value, 0n);

const dev1BudgetInterface = new ethers.Interface([
  "function setBudget(address token,uint256 amount,uint256 expiry)",
]);

assert.equal(
  dev1BudgetTx.data.toLowerCase(),
  dev1BudgetInterface.encodeFunctionData("setBudget", [
    dev1ReadyToken,
    dev1RequiredAmount,
    dev1ExpectedExpiry,
  ]).toLowerCase(),
  "Budget calldata differs from expected intent"
);

const dev1ReadyBlock = await ethers.provider.getBlock("latest");
assert(dev1ReadyBlock, "Latest block missing");

const dev1ReadToken = new ethers.Contract(
  dev1ReadyToken,
  [
    "function balanceOf(address) view returns (uint256)",
    "function allowance(address,address) view returns (uint256)",
  ],
  ethers.provider
);

const dev1ReadManager = new ethers.Contract(
  dev1ReadyManager,
  ["function remaining(address,address) view returns (uint256)"],
  ethers.provider
);

const dev1ReadOptions = { blockTag: dev1ReadyBlock.number };

const [
  dev1ReadyBalance,
  dev1ReadyAllowance,
  dev1ReadyBudget,
] = await Promise.all([
  dev1ReadToken.getFunction("balanceOf")(
    dev1ReadyWallet,
    dev1ReadOptions
  ),
  dev1ReadToken.getFunction("allowance")(
    dev1ReadyWallet,
    dev1ReadyManager,
    dev1ReadOptions
  ),
  dev1ReadManager.getFunction("remaining")(
    dev1ReadyWallet,
    dev1ReadyToken,
    dev1ReadOptions
  ),
]);

console.log("\nDev1 budget transaction:", dev1BudgetHash);
console.log("Budget receipt status: success");
console.log("Budget mined block:", dev1BudgetReceipt.blockNumber);
console.log("Budget transaction matches expected fields: true");
console.log("Readiness checked block:", dev1ReadyBlock.number);
console.log("USDT balance raw:", dev1ReadyBalance.toString());
console.log("Allowance raw:", dev1ReadyAllowance.toString());
console.log("Effective budget raw:", dev1ReadyBudget.toString());
console.log("Budget expiry:", dev1ExpectedExpiry.toString());
console.log(
  "Budget expiry UTC:",
  new Date(Number(dev1ExpectedExpiry) * 1000).toISOString()
);

const dev1OriginalBudgetUnexpired =
  BigInt(dev1ReadyBlock.timestamp) < dev1ExpectedExpiry;

const dev1WalletReady =
  dev1ReadyBalance >= dev1RequiredAmount &&
  dev1ReadyAllowance >= dev1RequiredAmount &&
  dev1ReadyBudget >= dev1RequiredAmount;

console.log(
  "Original verified budget still unexpired:",
  dev1OriginalBudgetUnexpired
);
console.log(
  "Wallet ready at checked block for amountInRaw=1000000:",
  dev1WalletReady
);

if (!dev1OriginalBudgetUnexpired) {
  console.log(
    "Historical budget has expired. Its successful receipt " +
    "does not prove current wallet readiness."
  );
}

if (!dev1WalletReady) {
  console.log(
    "Wallet is not currently ready. Review balance, allowance " +
    "and effective budget before requesting any new transaction."
  );
}

console.log("Read-only verification. No transaction sent.");