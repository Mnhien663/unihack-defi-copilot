import { network } from "hardhat";

const { ethers } = await network.create();

const chain = await ethers.provider.getNetwork();

if (chain.chainId !== 43113n) {
  throw new Error("Wrong network: expected Fuji chain ID 43113");
}

const [deployer] = await ethers.getSigners();
const address = await deployer.getAddress();
const balance = await ethers.provider.getBalance(address);

console.log("Chain ID:", chain.chainId.toString());
console.log("Deployer address:", address);
console.log("Balance:", ethers.formatEther(balance), "AVAX");