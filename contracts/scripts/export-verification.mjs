import { readdir, readFile, writeFile } from "node:fs/promises";

const buildDirectory = new URL("../artifacts/build-info/", import.meta.url);
const files = await readdir(buildDirectory);

let found = false;

for (const file of files) {
  if (!file.endsWith(".json") || file.endsWith(".output.json")) {
    continue;
  }

  const buildInfo = JSON.parse(
    await readFile(new URL(file, buildDirectory), "utf8")
  );

  const input = buildInfo.input;

  if (!input?.sources) {
    continue;
  }

  const containsDemoToken = Object.keys(input.sources).some(
    (name) => name.endsWith("/DemoToken.sol") || name === "DemoToken.sol"
  );

  if (!containsDemoToken) {
    continue;
  }

  await writeFile(
    new URL("../verification-input.json", import.meta.url),
    JSON.stringify(input, null, 2) + "\n",
    "utf8"
  );

  console.log("Created verification-input.json");
  console.log("Compiler:", buildInfo.solcLongVersion ?? buildInfo.solcVersion);
  console.log("Source files:", Object.keys(input.sources).length);

  found = true;
  break;
}

if (!found) {
  throw new Error("Could not find compiler input containing DemoToken.sol");
}