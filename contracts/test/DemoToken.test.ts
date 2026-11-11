import { expect } from "chai";
import { describe, it } from "mocha";
import { network } from "hardhat";

describe("DemoToken", function () {
  it("allows the owner to mint tokens to Alice", async function () {
    const { ethers } = await network.create();

    const [owner, alice] = await ethers.getSigners();

    const token = await ethers.deployContract("DemoToken", [], owner);
    await token.waitForDeployment();

    const amount = ethers.parseUnits("100", 18);

    const tx = await token.mint(alice.address, amount);
    await tx.wait();

    expect(await token.owner()).to.equal(owner.address);
    expect(await token.balanceOf(alice.address)).to.equal(amount);
    expect(await token.totalSupply()).to.equal(amount);
  });

    it("rejects minting by a non-owner", async function () {
    const { ethers } = await network.create();

    const [owner, alice, bob] = await ethers.getSigners();

    const token = await ethers.deployContract("DemoToken", [], owner);
    await token.waitForDeployment();

    const amount = ethers.parseUnits("100", 18);

    const tokenAsBob = token.connect(bob);
    const mintAsBob = tokenAsBob.getFunction("mint");

    await expect(
    mintAsBob(alice.address, amount)
    )
    .to.be.revertedWithCustomError(token, "OwnableUnauthorizedAccount")
    .withArgs(bob.address);
  });

    it("transfers tokens from Alice to Bob", async function () {
    const { ethers } = await network.create();

    const [owner, alice, bob] = await ethers.getSigners();

    const token = await ethers.deployContract("DemoToken", [], owner);
    await token.waitForDeployment();

    const initialAmount = ethers.parseUnits("100", 18);
    const transferAmount = ethers.parseUnits("25", 18);

    const mintTx = await token.mint(alice.address, initialAmount);
    await mintTx.wait();

    const tokenAsAlice = token.connect(alice);
    const transferAsAlice = tokenAsAlice.getFunction("transfer");

    await expect(
      transferAsAlice(bob.address, transferAmount)
    )
      .to.emit(token, "Transfer")
      .withArgs(alice.address, bob.address, transferAmount);

    expect(await token.balanceOf(alice.address)).to.equal(
      initialAmount - transferAmount
    );

    expect(await token.balanceOf(bob.address)).to.equal(
      transferAmount
    );

    expect(await token.totalSupply()).to.equal(initialAmount);
  });

    it("approves Bob without moving Alice's tokens", async function () {
    const { ethers } = await network.create();

    const [owner, alice, bob] = await ethers.getSigners();

    const token = await ethers.deployContract("DemoToken", [], owner);
    await token.waitForDeployment();

    const initialAmount = ethers.parseUnits("100", 18);
    const approvedAmount = ethers.parseUnits("30", 18);

    const mintTx = await token.mint(alice.address, initialAmount);
    await mintTx.wait();

    expect(
      await token.allowance(alice.address, bob.address)
    ).to.equal(0n);

    const tokenAsAlice = token.connect(alice);
    const approveAsAlice = tokenAsAlice.getFunction("approve");

    await expect(
      approveAsAlice(bob.address, approvedAmount)
    )
      .to.emit(token, "Approval")
      .withArgs(alice.address, bob.address, approvedAmount);

    expect(
      await token.allowance(alice.address, bob.address)
    ).to.equal(approvedAmount);

    expect(await token.balanceOf(alice.address)).to.equal(initialAmount);
    expect(await token.balanceOf(bob.address)).to.equal(0n);
    expect(await token.totalSupply()).to.equal(initialAmount);
  });

    it("lets Bob spend Alice's allowance using transferFrom", async function () {
    const { ethers } = await network.create();

    const [owner, alice, bob] = await ethers.getSigners();

    const token = await ethers.deployContract("DemoToken", [], owner);
    await token.waitForDeployment();

    const initialAmount = ethers.parseUnits("100", 18);
    const approvedAmount = ethers.parseUnits("30", 18);
    const spentAmount = ethers.parseUnits("12", 18);

    const mintTx = await token.mint(alice.address, initialAmount);
    await mintTx.wait();

    const approveAsAlice = token.connect(alice).getFunction("approve");

    const approveTx = await approveAsAlice(bob.address, approvedAmount);
    await approveTx.wait();

    const transferFromAsBob = token.connect(bob).getFunction("transferFrom");

    await expect(
      transferFromAsBob(alice.address, bob.address, spentAmount)
    )
      .to.emit(token, "Transfer")
      .withArgs(alice.address, bob.address, spentAmount);

    expect(await token.balanceOf(alice.address)).to.equal(
      initialAmount - spentAmount
    );

    expect(await token.balanceOf(bob.address)).to.equal(spentAmount);

    expect(
      await token.allowance(alice.address, bob.address)
    ).to.equal(approvedAmount - spentAmount);

    expect(await token.totalSupply()).to.equal(initialAmount);
  });

    it("rejects transferFrom above the allowance", async function () {
    const { ethers } = await network.create();

    const [owner, alice, bob] = await ethers.getSigners();

    const token = await ethers.deployContract("DemoToken", [], owner);
    await token.waitForDeployment();

    const initialAmount = ethers.parseUnits("100", 18);
    const approvedAmount = ethers.parseUnits("30", 18);
    const attemptedAmount = ethers.parseUnits("31", 18);

    const mintTx = await token.mint(alice.address, initialAmount);
    await mintTx.wait();

    const approveAsAlice = token.connect(alice).getFunction("approve");

    const approveTx = await approveAsAlice(bob.address, approvedAmount);
    await approveTx.wait();

    const transferFromAsBob = token.connect(bob).getFunction("transferFrom");

    await expect(
      transferFromAsBob(alice.address, bob.address, attemptedAmount)
    )
      .to.be.revertedWithCustomError(token, "ERC20InsufficientAllowance")
      .withArgs(bob.address, approvedAmount, attemptedAmount);

    expect(await token.balanceOf(alice.address)).to.equal(initialAmount);
    expect(await token.balanceOf(bob.address)).to.equal(0n);

    expect(
      await token.allowance(alice.address, bob.address)
    ).to.equal(approvedAmount);

    expect(await token.totalSupply()).to.equal(initialAmount);
  });

    it("rejects transferFrom when Alice has insufficient balance", async function () {
    const { ethers } = await network.create();

    const [owner, alice, bob] = await ethers.getSigners();

    const token = await ethers.deployContract("DemoToken", [], owner);
    await token.waitForDeployment();

    const initialAmount = ethers.parseUnits("10", 18);
    const approvedAmount = ethers.parseUnits("30", 18);
    const attemptedAmount = ethers.parseUnits("20", 18);

    const mintTx = await token.mint(alice.address, initialAmount);
    await mintTx.wait();

    const approveAsAlice = token.connect(alice).getFunction("approve");

    const approveTx = await approveAsAlice(bob.address, approvedAmount);
    await approveTx.wait();

    const transferFromAsBob = token.connect(bob).getFunction("transferFrom");

    await expect(
      transferFromAsBob(alice.address, bob.address, attemptedAmount)
    )
      .to.be.revertedWithCustomError(token, "ERC20InsufficientBalance")
      .withArgs(alice.address, initialAmount, attemptedAmount);

    expect(await token.balanceOf(alice.address)).to.equal(initialAmount);
    expect(await token.balanceOf(bob.address)).to.equal(0n);

    expect(
      await token.allowance(alice.address, bob.address)
    ).to.equal(approvedAmount);

    expect(await token.totalSupply()).to.equal(initialAmount);
  });
});