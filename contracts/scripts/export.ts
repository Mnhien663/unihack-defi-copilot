import { artifacts } from "hardhat";
import { mkdir, writeFile } from "node:fs/promises";

const contractNames = [
  "DemoToken",
  "AllowanceManager",
];

const outputDirectory = new URL("../abi/", import.meta.url);

await mkdir(outputDirectory, { recursive: true });

for (const contractName of contractNames) {
  const artifact = await artifacts.readArtifact(contractName);

  const outputFile = new URL(
    `${contractName}.json`,
    outputDirectory
  );

  await writeFile(
    outputFile,
    JSON.stringify(artifact.abi, null, 2) + "\n",
    "utf8"
  );

  console.log(
    `Exported ABI to contracts/abi/${contractName}.json`
  );
  console.log("ABI entries:", artifact.abi.length);
}