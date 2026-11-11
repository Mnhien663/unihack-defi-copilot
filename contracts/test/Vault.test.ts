import assert from "node:assert/strict";
import { describe, it } from "mocha";
import { network } from "hardhat";

async function deployVaultFixture(
  vaultContractName: string = "DemoVault"
) {
  const { ethers } = await network.create();
  const [owner, alice, bob] = await ethers.getSigners();

  const token = await ethers.deployContract("DemoToken", [], owner);
  await token.waitForDeployment();

  const manager = await ethers.deployContract("AllowanceManager", [], owner);
  await manager.waitForDeployment();

  const tokenAddress = await token.getAddress();
  const managerAddress = await manager.getAddress();

  const vault = await ethers.deployContract(
    vaultContractName,
    [tokenAddress],
    owner
  );
  await vault.waitForDeployment();

  const vaultAddress = await vault.getAddress();

  const adapter = await ethers.deployContract(
    "VaultAdapter",
    [managerAddress, vaultAddress],
    owner
  );
  await adapter.waitForDeployment();

  const adapterAddress = await adapter.getAddress();

  await (await manager.getFunction("setTokenSupported")(
    tokenAddress, true
  )).wait();

  await (await manager.getFunction("setTokenSupported")(
    vaultAddress, true
  )).wait();

  await (await manager.getFunction("setVaultAdapter")(
    tokenAddress, adapterAddress, vaultAddress
  )).wait();

  const amount = ethers.parseUnits("10", 18);

  await (await token.getFunction("mint")(
    alice.address, amount
  )).wait();

  await (await token.connect(alice).getFunction("approve")(
    managerAddress, amount
  )).wait();

  const block = await ethers.provider.getBlock("latest");
  if (!block) {
    throw new Error("Latest block missing");
  }
  const deadline = block.timestamp + 3600;

  await (await manager.connect(alice).getFunction("setBudget")(
    tokenAddress, amount, deadline
  )).wait();

  return {
    ethers,
    owner,
    alice,
    bob,
    token,
    manager,
    vault,
    adapter,
    tokenAddress,
    managerAddress,
    vaultAddress,
    adapterAddress,
    amount,
    deadline
  };
}

describe("DemoVault through VaultAdapter", function () {
  it("deposits Alice's assets and delivers shares directly to Alice", async function () {
    const f = await deployVaultFixture();

    const preview = await f.vault.getFunction("previewDeposit")(f.amount);

    assert.ok(preview > 0n);

    await (await f.manager.connect(f.alice).getFunction("executeDeposit")(
      f.tokenAddress,
      f.amount,
      preview,
      f.deadline
    )).wait();

    assert.equal(
      await f.vault.getFunction("balanceOf")(f.alice.address),
      preview
    );
    assert.equal(
      await f.vault.getFunction("balanceOf")(f.bob.address),
      0n
    );
    assert.equal(
      await f.vault.getFunction("balanceOf")(f.managerAddress),
      0n
    );
    assert.equal(
      await f.vault.getFunction("balanceOf")(f.adapterAddress),
      0n
    );

    assert.equal(
      await f.token.getFunction("balanceOf")(f.alice.address),
      0n
    );
    assert.equal(
      await f.token.getFunction("balanceOf")(f.vaultAddress),
      f.amount
    );
    assert.equal(
      await f.token.getFunction("balanceOf")(f.managerAddress),
      0n
    );
    assert.equal(
      await f.token.getFunction("balanceOf")(f.adapterAddress),
      0n
    );

    assert.equal(
      await f.manager.getFunction("remaining")(
        f.alice.address, f.tokenAddress
      ),
      0n
    );
    assert.equal(
      await f.token.getFunction("allowance")(
        f.managerAddress, f.adapterAddress
      ),
      0n
    );
    assert.equal(
      await f.token.getFunction("allowance")(
        f.adapterAddress, f.vaultAddress
      ),
      0n
    );
  });
  it("rolls back assets, shares, budget and approvals below minimum shares", async function () {
  const f = await deployVaultFixture();

  const preview = await f.vault.getFunction("previewDeposit")(f.amount);
  const minimum = preview + 1n;

  let reverted = false;

  try {
    const tx = await f.manager.connect(f.alice)
      .getFunction("executeDeposit")(
        f.tokenAddress,
        f.amount,
        minimum,
        f.deadline
      );
    await tx.wait();
  } catch (error) {
    const data = (error as { data?: unknown }).data;
    assert.equal(typeof data, "string", "expected contract revert data");

    const parsed = f.adapter.interface.parseError(data as string);
    assert.equal(parsed?.name, "InsufficientShares");
    assert.equal(parsed?.args[0], preview);
    assert.equal(parsed?.args[1], minimum);
    reverted = true;
  }

  assert.ok(reverted, "deposit must revert below minimum shares");

  assert.equal(
    await f.token.getFunction("balanceOf")(f.alice.address),
    f.amount
  );
  assert.equal(
    await f.token.getFunction("balanceOf")(f.vaultAddress),
    0n
  );
  assert.equal(
    await f.token.getFunction("balanceOf")(f.managerAddress),
    0n
  );
  assert.equal(
    await f.token.getFunction("balanceOf")(f.adapterAddress),
    0n
  );

  assert.equal(
    await f.vault.getFunction("totalSupply")(),
    0n
  );
  assert.equal(
    await f.vault.getFunction("balanceOf")(f.alice.address),
    0n
  );
  assert.equal(
    await f.manager.getFunction("remaining")(
      f.alice.address, f.tokenAddress
    ),
    f.amount
  );

  // The owner's original approval is restored by the revert.
  assert.equal(
    await f.token.getFunction("allowance")(
      f.alice.address, f.managerAddress
    ),
    f.amount
  );

  assert.equal(
    await f.token.getFunction("allowance")(
      f.managerAddress, f.adapterAddress
    ),
    0n
  );
  assert.equal(
    await f.token.getFunction("allowance")(
      f.adapterAddress, f.vaultAddress
    ),
    0n
  );
});

  it("uses actual shares after a donation and rejects the old minimum", async function () {
  const f = await deployVaultFixture();

  // Seed the empty vault with Bob's deposit.
  const seed = f.ethers.parseUnits("1", 18);

  await (await f.token.getFunction("mint")(
    f.bob.address, seed
  )).wait();

  await (await f.token.connect(f.bob).getFunction("approve")(
    f.vaultAddress, seed
  )).wait();

  await (await f.vault.connect(f.bob).getFunction("deposit")(
    seed, f.bob.address
  )).wait();

  const oldPreview = await f.vault.getFunction("previewDeposit")(f.amount);
  const supplyBefore = await f.vault.getFunction("totalSupply")();

  // A direct donation adds assets without minting shares.
  await (await f.token.getFunction("mint")(
    f.owner.address, seed
  )).wait();

  await (await f.token.getFunction("transfer")(
    f.vaultAddress, seed
  )).wait();

  assert.equal(
    await f.vault.getFunction("totalSupply")(),
    supplyBefore
  );
  assert.equal(
    await f.vault.getFunction("totalAssets")(),
    seed * 2n
  );

  const freshPreview = await f.vault.getFunction("previewDeposit")(f.amount);
  assert.ok(freshPreview > 0n);
  assert.ok(freshPreview < oldPreview);

  // OpenZeppelin's default offset is zero:
  // virtual shares = 1, virtual assets = 1.
  const expectedFloor =
    f.amount * (supplyBefore + 1n) / (seed * 2n + 1n);

  assert.equal(freshPreview, expectedFloor);

  let reverted = false;
  try {
    const tx = await f.manager.connect(f.alice)
      .getFunction("executeDeposit")(
        f.tokenAddress,
        f.amount,
        oldPreview,
        f.deadline
      );
    await tx.wait();
  } catch (error) {
    const data = (error as { data?: unknown }).data;
    assert.equal(typeof data, "string", "expected contract revert data");

    const parsed = f.adapter.interface.parseError(data as string);
    assert.equal(parsed?.name, "InsufficientShares");
    assert.equal(parsed?.args[0], freshPreview);
    assert.equal(parsed?.args[1], oldPreview);
    reverted = true;
  }
  assert.ok(reverted, "old minimum must reject the changed exchange rate");

  assert.equal(
    await f.manager.getFunction("remaining")(
      f.alice.address, f.tokenAddress
    ),
    f.amount
  );
  assert.equal(
    await f.token.getFunction("balanceOf")(f.alice.address),
    f.amount
  );

  // Retry using the newly observed minimum.
  await (await f.manager.connect(f.alice).getFunction("executeDeposit")(
    f.tokenAddress,
    f.amount,
    freshPreview,
    f.deadline
  )).wait();

  assert.equal(
    await f.vault.getFunction("balanceOf")(f.alice.address),
    freshPreview
  );
  assert.equal(
    await f.vault.getFunction("totalSupply")(),
    supplyBefore + freshPreview
  );
  assert.equal(
    await f.token.getFunction("allowance")(
      f.adapterAddress, f.vaultAddress
    ),
    0n
  );
});

  it("lets Alice redeem her shares and receive the underlying assets", async function () {
  const f = await deployVaultFixture();

  const shares = await f.vault.getFunction("previewDeposit")(f.amount);

  await (await f.manager.connect(f.alice).getFunction("executeDeposit")(
    f.tokenAddress,
    f.amount,
    shares,
    f.deadline
  )).wait();

  const assetsBefore = await f.token.getFunction("balanceOf")(
    f.alice.address
  );
  const expectedAssets = await f.vault.getFunction("previewRedeem")(shares);

  assert.equal(expectedAssets, f.amount);

  await (await f.vault.connect(f.alice).getFunction("redeem")(
    shares,
    f.alice.address,
    f.alice.address
  )).wait();

  assert.equal(
    await f.token.getFunction("balanceOf")(f.alice.address),
    assetsBefore + expectedAssets
  );
  assert.equal(
    await f.vault.getFunction("balanceOf")(f.alice.address),
    0n
  );
  assert.equal(
    await f.vault.getFunction("totalSupply")(),
    0n
  );
  assert.equal(
    await f.vault.getFunction("totalAssets")(),
    0n
  );

  // Redeeming vault shares does not restore the manager's spent budget.
  assert.equal(
    await f.manager.getFunction("remaining")(
      f.alice.address, f.tokenAddress
    ),
    0n
  );
});

  it("rejects Bob redeeming Alice's shares without share allowance", async function () {
  const f = await deployVaultFixture();

  const shares = await f.vault.getFunction("previewDeposit")(f.amount);

  await (await f.manager.connect(f.alice).getFunction("executeDeposit")(
    f.tokenAddress,
    f.amount,
    shares,
    f.deadline
  )).wait();

  let reverted = false;
  try {
    const tx = await f.vault.connect(f.bob).getFunction("redeem")(
      shares,
      f.bob.address,
      f.alice.address
    );
    await tx.wait();
  } catch (error) {
    const data = (error as { data?: unknown }).data;
    assert.equal(typeof data, "string", "expected contract revert data");

    const parsed = f.vault.interface.parseError(data as string);
    assert.equal(parsed?.name, "ERC20InsufficientAllowance");
    assert.equal(parsed?.args[0], f.bob.address);
    assert.equal(parsed?.args[1], 0n);
    assert.equal(parsed?.args[2], shares);
    reverted = true;
  }

  assert.ok(reverted, "Bob must not redeem Alice's shares without approval");

  assert.equal(
    await f.vault.getFunction("balanceOf")(f.alice.address),
    shares
  );
  assert.equal(
    await f.vault.getFunction("balanceOf")(f.bob.address),
    0n
  );
  assert.equal(
    await f.vault.getFunction("totalAssets")(),
    f.amount
  );
  assert.equal(
    await f.token.getFunction("balanceOf")(f.bob.address),
    0n
  );

  // Alice explicitly approves Bob to spend her vault shares.
  await (await f.vault.connect(f.alice).getFunction("approve")(
    f.bob.address, shares
  )).wait();

  await (await f.vault.connect(f.bob).getFunction("redeem")(
    shares,
    f.alice.address,
    f.alice.address
  )).wait();

  assert.equal(
    await f.token.getFunction("balanceOf")(f.alice.address),
    f.amount
  );
  assert.equal(
    await f.vault.getFunction("balanceOf")(f.alice.address),
    0n
  );
  assert.equal(
    await f.vault.getFunction("allowance")(
      f.alice.address, f.bob.address
    ),
    0n
  );
});

  it("rejects deposit with insufficient underlying allowance and restores budget", async function () {
  const f = await deployVaultFixture();

  await (await f.token.connect(f.alice).getFunction("approve")(
    f.managerAddress, f.amount - 1n
  )).wait();

  const shares = await f.vault.getFunction("previewDeposit")(f.amount);

  let reverted = false;
  try {
    const tx = await f.manager.connect(f.alice)
      .getFunction("executeDeposit")(
        f.tokenAddress,
        f.amount,
        shares,
        f.deadline
      );
    await tx.wait();
  } catch (error) {
    const data = (error as { data?: unknown }).data;
    assert.equal(typeof data, "string", "expected contract revert data");

    const parsed = f.token.interface.parseError(data as string);
    assert.equal(parsed?.name, "ERC20InsufficientAllowance");
    assert.equal(parsed?.args[0], f.managerAddress);
    assert.equal(parsed?.args[1], f.amount - 1n);
    assert.equal(parsed?.args[2], f.amount);
    reverted = true;
  }

  assert.ok(reverted, "insufficient underlying allowance must revert");

  assert.equal(
    await f.manager.getFunction("remaining")(
      f.alice.address, f.tokenAddress
    ),
    f.amount
  );
  assert.equal(
    await f.token.getFunction("balanceOf")(f.alice.address),
    f.amount
  );
  assert.equal(
    await f.vault.getFunction("totalSupply")(),
    0n
  );
  assert.equal(
    await f.vault.getFunction("totalAssets")(),
    0n
  );
});

it("rejects direct adapter calls from Alice", async function () {
  const f = await deployVaultFixture();

  let reverted = false;
  try {
    const tx = await f.adapter.connect(f.alice).getFunction("deposit")(
      f.tokenAddress,
      f.amount,
      1n,
      f.alice.address,
      f.deadline
    );
    await tx.wait();
  } catch (error) {
    const data = (error as { data?: unknown }).data;
    assert.equal(typeof data, "string", "expected contract revert data");

    const parsed = f.adapter.interface.parseError(data as string);
    assert.equal(parsed?.name, "UnauthorizedManager");
    reverted = true;
  }

  assert.ok(reverted, "only the configured manager may call adapter");

  assert.equal(
    await f.token.getFunction("balanceOf")(f.alice.address),
    f.amount
  );
  assert.equal(
    await f.vault.getFunction("totalSupply")(),
    0n
  );
});

  it("rolls back the entire deposit when the downstream vault reverts", async function () {
  const f = await deployVaultFixture("MockRevertingVault");

  let reverted = false;
  try {
    const tx = await f.manager.connect(f.alice)
      .getFunction("executeDeposit")(
        f.tokenAddress,
        f.amount,
        1n,
        f.deadline
      );
    await tx.wait();
  } catch (error) {
    const data = (error as { data?: unknown }).data;
    assert.equal(typeof data, "string", "expected contract revert data");

    const parsed = f.vault.interface.parseError(data as string);
    assert.equal(parsed?.name, "DepositBlocked");
    reverted = true;
  }

  assert.ok(reverted, "downstream vault revert must propagate");

  assert.equal(
    await f.manager.getFunction("remaining")(
      f.alice.address, f.tokenAddress
    ),
    f.amount
  );

  for (const address of [
    f.managerAddress,
    f.adapterAddress,
    f.vaultAddress
  ]) {
    assert.equal(
      await f.token.getFunction("balanceOf")(address),
      0n
    );
  }

  assert.equal(
    await f.token.getFunction("balanceOf")(f.alice.address),
    f.amount
  );
  assert.equal(
    await f.vault.getFunction("totalSupply")(),
    0n
  );
  assert.equal(
    await f.token.getFunction("allowance")(
      f.alice.address, f.managerAddress
    ),
    f.amount
  );
  assert.equal(
    await f.token.getFunction("allowance")(
      f.managerAddress, f.adapterAddress
    ),
    0n
  );
  assert.equal(
    await f.token.getFunction("allowance")(
      f.adapterAddress, f.vaultAddress
    ),
    0n
  );
});
});