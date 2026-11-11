import { network } from "hardhat";

const { ethers } = await network.create();

const chain = await ethers.provider.getNetwork();
if (chain.chainId !== 43113n) {
  throw new Error(`Wrong chain: ${chain.chainId}`);
}

const [signer] = await ethers.getSigners();
const wallet = await signer.getAddress();

const expectedWallet = "0x4DFb0d91C07A7C706F65F974953cE8b111D27Ee5";
if (wallet.toLowerCase() !== expectedWallet.toLowerCase()) {
  throw new Error(`Unexpected signer: ${wallet}`);
}

const poolAddress = "0x8B9b2AF4afB389b4a70A474dfD4AdCD4a302bb40";
const asset = "0x5425890298aed601595a70AB815c96711a31Bc65".toLowerCase();
const aTokenAddress = "0x9CFcc1B289E59FBe1E769f020C77315DF8473760".toLowerCase();
const amount = ethers.parseUnits("0.1", 6);

const tokenABI = [
  "function balanceOf(address) view returns (uint256)",
  "function allowance(address owner,address spender) view returns (uint256)",
];

const usdc = new ethers.Contract(asset, tokenABI, signer);
const aToken = new ethers.Contract(aTokenAddress, tokenABI, signer);

const pool = new ethers.Contract(
  poolAddress,
  [
    "function supply(address asset,uint256 amount,address onBehalfOf,uint16 referralCode)",
  ],
  signer
);

const [balanceBefore, allowance, aTokenBefore] = await Promise.all([
  usdc.balanceOf(wallet),
  usdc.allowance(wallet, poolAddress),
  aToken.balanceOf(wallet),
]) as [bigint, bigint, bigint];

if (balanceBefore < amount) {
  throw new Error("Not enough USDC");
}
if (allowance < amount) {
  throw new Error("Not enough allowance");
}

console.log("Chain ID:", chain.chainId.toString());
console.log("Signer:", wallet);
console.log("Supply amount: 0.1 USDC");
console.log("USDC before:", ethers.formatUnits(balanceBefore, 6));
console.log("aToken before:", ethers.formatUnits(aTokenBefore, 6));

// Mô phỏng trước; nếu revert thì dừng, chưa gửi giao dịch.
await pool.supply.staticCall(asset, amount, wallet, 0);
console.log("Supply simulation: passed");

const tx = await pool.supply(asset, amount, wallet, 0);
console.log("Supply transaction:", tx.hash);

const receipt = await tx.wait();
if (!receipt || receipt.status !== 1) {
  throw new Error("Supply transaction failed");
}

// Đọc cả hai số dư tại block xác nhận giao dịch.
const options = { blockTag: receipt.blockNumber };
const [balanceAfter, aTokenAfter] = await Promise.all([
  usdc.balanceOf(wallet, options),
  aToken.balanceOf(wallet, options),
]) as [bigint, bigint];

console.log("Confirmed block:", receipt.blockNumber);
console.log("USDC after:", ethers.formatUnits(balanceAfter, 6));
console.log("aToken after:", ethers.formatUnits(aTokenAfter, 6));

if (balanceBefore - balanceAfter !== amount || aTokenAfter <= aTokenBefore) {
  throw new Error("Transaction confirmed, but balance checks need review");
}

console.log("Supply balance checks: passed");