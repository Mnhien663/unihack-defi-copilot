import { network } from "hardhat";

const { ethers } = await network.create();

const expectedOwner = "0x4DFb0d91C07A7C706F65F974953cE8b111D27Ee5";
const recipient = "0x37E146a4141C8fd82f0c8c5Bb8035930fD0ea588";
const asset = "0xcCCb83Fbe349fAebDE74Ce633D25B282E6FDd21A";
const amount = ethers.parseUnits("0.1", 18);

const chain = await ethers.provider.getNetwork();
if (chain.chainId !== 43113n) {
  throw new Error("Expected Fuji chain ID 43113");
}

const [deployer] = await ethers.getSigners();
if (!deployer) throw new Error("Deployer signer unavailable");

const signerAddress = await deployer.getAddress();
if (signerAddress.toLowerCase() !== expectedOwner.toLowerCase()) {
  throw new Error("Unexpected deployer signer");
}

const token = new ethers.Contract(asset, [
  "function owner() view returns (address)",
  "function decimals() view returns (uint8)",
  "function balanceOf(address) view returns (uint256)",
  "function mint(address,uint256)",
], deployer);

const owner = await token.getFunction("owner")();
if (owner.toLowerCase() !== expectedOwner.toLowerCase()) {
  throw new Error("Token owner differs from expected deployer");
}
if (Number(await token.getFunction("decimals")()) !== 18) {
  throw new Error("Unexpected DDEMO decimals");
}

const before = await token.getFunction("balanceOf")(recipient);
console.log("Chain ID:", chain.chainId.toString());
console.log("Deployer:", signerAddress);
console.log("Token:", asset);
console.log("Recipient:", recipient);
console.log("Current DDEMO raw:", before.toString());

if (before >= amount) {
  console.log("Wallet already has enough DDEMO. No transaction sent.");
} else {
  // Mint only the shortfall to reach the 0.1 DDEMO test balance.
  const shortfall = amount - before;
  const mint = token.getFunction("mint");
  await mint.staticCall(recipient, shortfall);

  const gas = await mint.estimateGas(recipient, shortfall);
  const gasLimit = (gas * 120n + 99n) / 100n;
  const fees = await ethers.provider.getFeeData();
  const feePerGas = fees.maxFeePerGas ?? fees.gasPrice;
  if (feePerGas === null || feePerGas <= 0n) {
    throw new Error("Fee estimate unavailable");
  }
  const feeBound = gasLimit * feePerGas;
  const native = await ethers.provider.getBalance(signerAddress);
  if (native < feeBound) {
    throw new Error("Deployer has insufficient AVAX for estimated fee");
  }

  console.log("Mint DDEMO:", ethers.formatUnits(shortfall, 18));
  console.log("Estimated gas:", gas.toString());
  console.log("Fee bound AVAX:", ethers.formatEther(feeBound));

  if (process.env.SEND_DEMO_FUNDING !== "1") {
    console.log("Preflight passed. No transaction sent.");
  } else {
    // Check again immediately before sending.
    const latestBalance =
      await token.getFunction("balanceOf")(recipient);
    if (latestBalance !== before) {
      throw new Error("Recipient balance changed; rerun preflight");
    }

    const tx = await mint(recipient, shortfall, { gasLimit });
    console.log("Funding transaction:", tx.hash);

    const receipt = await tx.wait();
    if (!receipt || receipt.status !== 1) {
      throw new Error("Funding receipt not successful");
    }

    const after = await token.getFunction("balanceOf")(
      recipient, { blockTag: receipt.blockNumber }
    );
    if (after !== before + shortfall) {
      throw new Error("Unexpected recipient balance after mint");
    }

    console.log("Mined block:", receipt.blockNumber);
    console.log("DDEMO balance raw:", after.toString());
    console.log("DDEMO balance:", ethers.formatUnits(after, 18));
  }
}