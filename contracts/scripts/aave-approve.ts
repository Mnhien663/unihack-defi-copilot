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

const pool = "0x8B9b2AF4afB389b4a70A474dfD4AdCD4a302bb40";
const usdc = new ethers.Contract(
  "0x5425890298aed601595a70AB815c96711a31Bc65".toLowerCase(),
  [
    "function approve(address spender,uint256 amount) returns (bool)",
    "function allowance(address owner,address spender) view returns (uint256)",
  ],
  signer
);

const amount = ethers.parseUnits("0.1", 6);

console.log("Chain ID:", chain.chainId.toString());
console.log("Signer:", wallet);
console.log("Spender:", pool);
console.log("Approve amount: 0.1 USDC");

const simulated = await usdc.approve.staticCall(pool, amount);
if (!simulated) {
  throw new Error("Approve simulation returned false");
}

const tx = await usdc.approve(pool, amount);
console.log("Approval transaction:", tx.hash);

const receipt = await tx.wait();
if (!receipt || receipt.status !== 1) {
  throw new Error("Approval transaction failed");
}

const allowance = await usdc.allowance(wallet, pool);
console.log("Allowance raw:", allowance.toString());
console.log("Allowance USDC:", ethers.formatUnits(allowance, 6));