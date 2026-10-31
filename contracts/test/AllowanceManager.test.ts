import { expect } from "chai";
import { describe, it } from "mocha";
import { network } from "hardhat";

async function setup() {
  const { ethers } = await network.create();

  const [deployer, alice, bob] = await ethers.getSigners();

  const manager = await ethers.deployContract(
    "AllowanceManager",
    [],
    deployer
  );
  await manager.waitForDeployment();

  const token = await ethers.deployContract("DemoToken", [], deployer);
  await token.waitForDeployment();

  const tokenAddress = await token.getAddress();
  const managerAddress = await manager.getAddress();

  const block = await ethers.provider.getBlock("latest");

  if (block === null) {
    throw new Error("Latest block not found");
  }

  const expiry = block.timestamp + 3600;
  const amount = ethers.parseUnits("20", 18);

  const setAsAlice = manager.connect(alice).getFunction("setBudget");
  const setAsBob = manager.connect(bob).getFunction("setBudget");
  const revokeAsAlice = manager.connect(alice).getFunction("revoke");

  return {
    ethers,
    deployer,
    alice,
    bob,
    manager,
    token,
    tokenAddress,
    managerAddress,
    expiry,
    amount,
    setAsAlice,
    setAsBob,
    revokeAsAlice,
  };
}

describe("AllowanceManager", function () {
  it("returns zero for an unset budget", async function () {
    const { manager, alice, tokenAddress } = await setup();

    expect(
      await manager.remaining(alice.address, tokenAddress)
    ).to.equal(0n);
  });

  it("sets the caller's budget and emits BudgetSet", async function () {
    const {
      manager,
      alice,
      tokenAddress,
      amount,
      expiry,
      setAsAlice,
    } = await setup();

    await expect(setAsAlice(tokenAddress, amount, expiry))
      .to.emit(manager, "BudgetSet")
      .withArgs(alice.address, tokenAddress, amount, expiry);

    expect(
      await manager.remaining(alice.address, tokenAddress)
    ).to.equal(amount);
  });

  it("replaces the previous budget instead of adding to it", async function () {
    const {
      manager,
      alice,
      tokenAddress,
      amount,
      expiry,
      setAsAlice,
    } = await setup();

    await (await setAsAlice(tokenAddress, amount, expiry)).wait();

    const replacement = amount / 2n;
    const replacementExpiry = expiry + 3600;

    await (
      await setAsAlice(
        tokenAddress,
        replacement,
        replacementExpiry
      )
    ).wait();

    expect(
      await manager.remaining(alice.address, tokenAddress)
    ).to.equal(replacement);
  });

  it("keeps Alice's and Bob's budgets independent", async function () {
    const {
      manager,
      alice,
      bob,
      tokenAddress,
      amount,
      expiry,
      setAsAlice,
      setAsBob,
    } = await setup();

    const bobAmount = amount * 3n;

    await (await setAsBob(tokenAddress, bobAmount, expiry)).wait();
    await (await setAsAlice(tokenAddress, amount, expiry)).wait();

    expect(
      await manager.remaining(alice.address, tokenAddress)
    ).to.equal(amount);

    expect(
      await manager.remaining(bob.address, tokenAddress)
    ).to.equal(bobAmount);
  });

  it("keeps budgets for different tokens independent", async function () {
    const {
      ethers,
      deployer,
      manager,
      alice,
      tokenAddress,
      amount,
      expiry,
      setAsAlice,
    } = await setup();

    const secondToken = await ethers.deployContract(
      "DemoToken",
      [],
      deployer
    );
    await secondToken.waitForDeployment();

    const secondAddress = await secondToken.getAddress();
    const secondAmount = amount * 2n;

    await (await setAsAlice(tokenAddress, amount, expiry)).wait();
    await (
      await setAsAlice(secondAddress, secondAmount, expiry)
    ).wait();

    expect(
      await manager.remaining(alice.address, tokenAddress)
    ).to.equal(amount);

    expect(
      await manager.remaining(alice.address, secondAddress)
    ).to.equal(secondAmount);
  });

  it("rejects the zero token address", async function () {
    const {
      ethers,
      manager,
      amount,
      expiry,
      setAsAlice,
    } = await setup();

    await expect(
      setAsAlice(ethers.ZeroAddress, amount, expiry)
    ).to.be.revertedWithCustomError(manager, "InvalidToken");
  });

  it("rejects a zero amount without overwriting an existing budget", async function () {
    const {
      manager,
      alice,
      tokenAddress,
      amount,
      expiry,
      setAsAlice,
    } = await setup();

    await (await setAsAlice(tokenAddress, amount, expiry)).wait();

    await expect(
      setAsAlice(tokenAddress, 0n, expiry)
    ).to.be.revertedWithCustomError(manager, "InvalidAmount");

    expect(
      await manager.remaining(alice.address, tokenAddress)
    ).to.equal(amount);
  });

  it("rejects expiry equal to the transaction timestamp", async function () {
    const {
      ethers,
      manager,
      tokenAddress,
      amount,
      expiry,
      setAsAlice,
    } = await setup();

    await ethers.provider.send(
      "evm_setNextBlockTimestamp",
      [expiry]
    );

    await expect(
      setAsAlice(tokenAddress, amount, expiry)
    ).to.be.revertedWithCustomError(manager, "InvalidExpiry");
  });

  it("rejects expiry earlier than the transaction timestamp", async function () {
    const {
      ethers,
      manager,
      tokenAddress,
      amount,
      expiry,
      setAsAlice,
    } = await setup();

    await ethers.provider.send(
      "evm_setNextBlockTimestamp",
      [expiry + 1]
    );

    await expect(
      setAsAlice(tokenAddress, amount, expiry)
    ).to.be.revertedWithCustomError(manager, "InvalidExpiry");
  });

  it("returns the budget before expiry and zero at or after expiry", async function () {
    const {
      ethers,
      manager,
      alice,
      tokenAddress,
      amount,
      expiry,
      setAsAlice,
    } = await setup();

    await (await setAsAlice(tokenAddress, amount, expiry)).wait();

    await ethers.provider.send(
      "evm_setNextBlockTimestamp",
      [expiry - 1]
    );
    await ethers.provider.send("evm_mine", []);

    expect(
      await manager.remaining(alice.address, tokenAddress)
    ).to.equal(amount);

    await ethers.provider.send(
      "evm_setNextBlockTimestamp",
      [expiry]
    );
    await ethers.provider.send("evm_mine", []);

    expect(
      await manager.remaining(alice.address, tokenAddress)
    ).to.equal(0n);

    await ethers.provider.send(
      "evm_setNextBlockTimestamp",
      [expiry + 1]
    );
    await ethers.provider.send("evm_mine", []);

    expect(
      await manager.remaining(alice.address, tokenAddress)
    ).to.equal(0n);
  });

  it("uses the replacement expiry instead of the old expiry", async function () {
    const {
      ethers,
      manager,
      alice,
      tokenAddress,
      amount,
      expiry,
      setAsAlice,
    } = await setup();

    await (await setAsAlice(tokenAddress, amount, expiry)).wait();

    const newExpiry = expiry + 3600;

    await (
      await setAsAlice(tokenAddress, amount, newExpiry)
    ).wait();

    await ethers.provider.send(
      "evm_setNextBlockTimestamp",
      [expiry]
    );
    await ethers.provider.send("evm_mine", []);

    expect(
      await manager.remaining(alice.address, tokenAddress)
    ).to.equal(amount);

    await ethers.provider.send(
      "evm_setNextBlockTimestamp",
      [newExpiry]
    );
    await ethers.provider.send("evm_mine", []);

    expect(
      await manager.remaining(alice.address, tokenAddress)
    ).to.equal(0n);
  });

  it("revokes Alice's budget without changing Bob's budget", async function () {
    const {
      manager,
      alice,
      bob,
      tokenAddress,
      amount,
      expiry,
      setAsAlice,
      setAsBob,
      revokeAsAlice,
    } = await setup();

    const bobAmount = amount * 3n;

    await (await setAsAlice(tokenAddress, amount, expiry)).wait();
    await (await setAsBob(tokenAddress, bobAmount, expiry)).wait();

    await expect(revokeAsAlice(tokenAddress))
      .to.emit(manager, "BudgetRevoked")
      .withArgs(alice.address, tokenAddress);

    expect(
      await manager.remaining(alice.address, tokenAddress)
    ).to.equal(0n);

    expect(
      await manager.remaining(bob.address, tokenAddress)
    ).to.equal(bobAmount);
  });

  it("allows repeated revocation and setting a new budget", async function () {
    const {
      manager,
      alice,
      tokenAddress,
      amount,
      expiry,
      setAsAlice,
      revokeAsAlice,
    } = await setup();

    await (await setAsAlice(tokenAddress, amount, expiry)).wait();
    await (await revokeAsAlice(tokenAddress)).wait();

    await expect(revokeAsAlice(tokenAddress))
      .to.emit(manager, "BudgetRevoked")
      .withArgs(alice.address, tokenAddress);

    expect(
      await manager.remaining(alice.address, tokenAddress)
    ).to.equal(0n);

    const newAmount = amount * 2n;

    await (
      await setAsAlice(tokenAddress, newAmount, expiry + 3600)
    ).wait();

    expect(
      await manager.remaining(alice.address, tokenAddress)
    ).to.equal(newAmount);
  });

  it("rejects revocation for the zero token address", async function () {
    const {
      ethers,
      manager,
      revokeAsAlice,
    } = await setup();

    await expect(
      revokeAsAlice(ethers.ZeroAddress)
    ).to.be.revertedWithCustomError(manager, "InvalidToken");
  });

  it("does not move tokens or change ERC-20 approval when setting or revoking a budget", async function () {
    const {
      token,
      manager,
      alice,
      tokenAddress,
      managerAddress,
      amount,
      expiry,
      setAsAlice,
      revokeAsAlice,
    } = await setup();

    const balance = amount * 5n;
    const approvedAmount = amount * 3n;

    await (
      await token.getFunction("mint")(alice.address, balance)
    ).wait();

    await (
      await token.connect(alice).getFunction("approve")(
        managerAddress,
        approvedAmount
      )
    ).wait();

    await (await setAsAlice(tokenAddress, amount, expiry)).wait();

    expect(
      await token.allowance(alice.address, managerAddress)
    ).to.equal(approvedAmount);

    expect(await token.balanceOf(alice.address)).to.equal(balance);
    expect(await token.balanceOf(managerAddress)).to.equal(0n);

    await (await revokeAsAlice(tokenAddress)).wait();

    expect(
      await manager.remaining(alice.address, tokenAddress)
    ).to.equal(0n);

    expect(
      await token.allowance(alice.address, managerAddress)
    ).to.equal(approvedAmount);

    expect(await token.balanceOf(alice.address)).to.equal(balance);
    expect(await token.balanceOf(managerAddress)).to.equal(0n);
  });
});