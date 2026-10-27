import { artifacts } from "hardhat";
import { mkdir, writeFile } from "node:fs/promises";

const artifact = await artifacts.readArtifact("DemoToken");

const outputDirectory = new URL("../abi/", import.meta.url);
const outputFile = new URL("DemoToken.json", outputDirectory);

await mkdir(outputDirectory, { recursive: true });

await writeFile(
  outputFile,
  JSON.stringify(artifact.abi, null, 2) + "\n",
  "utf8"
);

console.log("Exported ABI to contracts/abi/DemoToken.json");
console.log("ABI entries:", artifact.abi.length);