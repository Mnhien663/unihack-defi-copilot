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

const asset = "0x5425890298aed601595a70AB815c96711a31Bc65".toLowerCase();
const amount = ethers.parseUnits("0.1", 6);

const balanceABI = [
  "function balanceOf(address) view returns (uint256)",
];

const usdc = new ethers.Contract(asset, balanceABI, signer);
const aToken = new ethers.Contract(
  "0x9CFcc1B289E59FBe1E769f020C77315DF8473760".toLowerCase(),
  balanceABI,
  signer
);

const pool = new ethers.Contract(
  "0x8B9b2AF4afB389b4a70A474dfD4AdCD4a302bb40",
  [
    "function withdraw(address asset,uint256 amount,address to) returns (uint256)",
  ],
  signer
);

const [balanceBefore, aTokenBefore] = await Promise.all([
  usdc.balanceOf(wallet),
  aToken.balanceOf(wallet),
]) as [bigint, bigint];

if (aTokenBefore < amount) {
  throw new Error("Not enough aToken for this withdrawal");
}

console.log("Chain ID:", chain.chainId.toString());
console.log("Signer:", wallet);
console.log("Withdraw amount: 0.1 USDC");
console.log("USDC before:", ethers.formatUnits(balanceBefore, 6));
console.log("aToken before:", ethers.formatUnits(aTokenBefore, 6));

const simulated: bigint = await pool.withdraw.staticCall(
  asset, amount, wallet
);
if (simulated !== amount) {
  throw new Error("Simulation returned an unexpected withdrawal amount");
}
console.log("Withdraw simulation: passed");

const tx = await pool.withdraw(asset, amount, wallet);
console.log("Withdraw transaction:", tx.hash);

const receipt = await tx.wait();
if (!receipt || receipt.status !== 1) {
  throw new Error("Withdraw transaction failed");
}

const options = { blockTag: receipt.blockNumber };
const [balanceAfter, aTokenAfter] = await Promise.all([
  usdc.balanceOf(wallet, options),
  aToken.balanceOf(wallet, options),
]) as [bigint, bigint];

console.log("Confirmed block:", receipt.blockNumber);
console.log("USDC after:", ethers.formatUnits(balanceAfter, 6));
console.log("aToken after:", ethers.formatUnits(aTokenAfter, 6));

if (balanceAfter - balanceBefore !== amount) {
  throw new Error("Transaction confirmed, but USDC balance needs review");
}
console.log("Withdraw USDC balance check: passed");