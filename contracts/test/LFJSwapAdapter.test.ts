import assert from "node:assert/strict";
import { describe, it } from "mocha";
import { network } from "hardhat";

async function deploySwapFixture(reverse = false) {
  const { ethers } = await network.create();
  const [owner, alice, bob] = await ethers.getSigners();

  const tokenX = await ethers.deployContract("DemoToken", [], owner);
  const tokenY = await ethers.deployContract("DemoToken", [], owner);
  const manager = await ethers.deployContract("AllowanceManager", [], owner);
  const router = await ethers.deployContract("MockLFJRouter", [], owner);

  for (const contract of [tokenX, tokenY, manager, router]) {
    await contract.waitForDeployment();
  }

  const tokenXAddress = await tokenX.getAddress();
  const tokenYAddress = await tokenY.getAddress();
  const managerAddress = await manager.getAddress();
  const routerAddress = await router.getAddress();

  const adapter = await ethers.deployContract(
    "LFJSwapAdapter",
    [managerAddress, routerAddress, tokenXAddress, tokenYAddress],
    owner
  );
  await adapter.waitForDeployment();
  const adapterAddress = await adapter.getAddress();

  for (const token of [tokenXAddress, tokenYAddress]) {
    await (await manager.getFunction("setTokenSupported")(
      token, true
    )).wait();
  }

  const input = reverse ? tokenY : tokenX;
  const output = reverse ? tokenX : tokenY;
  const inputAddress = await input.getAddress();
  const outputAddress = await output.getAddress();

  await (await manager.getFunction("setSwapAdapter")(
    inputAddress, outputAddress, adapterAddress
  )).wait();

  const amountIn = ethers.parseUnits("10", 18);
  const amountOut = ethers.parseUnits("9", 18);

  await (await input.getFunction("mint")(
    alice.address, amountIn
  )).wait();

  await (await output.getFunction("mint")(
    routerAddress, amountOut
  )).wait();

  await (await router.getFunction("configure")(
    amountOut, 0
  )).wait();

  await (await input.connect(alice).getFunction("approve")(
    managerAddress, amountIn
  )).wait();

  const block = await ethers.provider.getBlock("latest");
  if (!block) {
    throw new Error("Latest block missing");
  }
  const deadline = block.timestamp + 3600;

  await (await manager.connect(alice).getFunction("setBudget")(
    inputAddress, amountIn, deadline
  )).wait();

  return {
    ethers, owner, alice, bob,
    input, output, manager, router, adapter,
    inputAddress, outputAddress,
    managerAddress, routerAddress, adapterAddress,
    amountIn, amountOut, deadline
  };
}

describe("LFJSwapAdapter through AllowanceManager", function () {
  for (const reverse of [false, true]) {
    it(`swaps ${reverse ? "Y to X" : "X to Y"} and pays Alice`, async function () {
      const f = await deploySwapFixture(reverse);

      await (await f.manager.connect(f.alice).getFunction("executeSwap")(
        f.inputAddress,
        f.outputAddress,
        f.amountIn,
        f.amountOut,
        f.deadline
      )).wait();

      assert.equal(
        await f.input.getFunction("balanceOf")(f.alice.address),
        0n
      );
      assert.equal(
        await f.input.getFunction("balanceOf")(f.routerAddress),
        f.amountIn
      );
      assert.equal(
        await f.output.getFunction("balanceOf")(f.alice.address),
        f.amountOut
      );
      assert.equal(
        await f.output.getFunction("balanceOf")(f.bob.address),
        0n
      );

      for (const address of [f.managerAddress, f.adapterAddress]) {
        assert.equal(
          await f.input.getFunction("balanceOf")(address),
          0n
        );
        assert.equal(
          await f.output.getFunction("balanceOf")(address),
          0n
        );
      }

      assert.equal(
        await f.manager.getFunction("remaining")(
          f.alice.address, f.inputAddress
        ),
        0n
      );
      assert.equal(
        await f.input.getFunction("allowance")(
          f.managerAddress, f.adapterAddress
        ),
        0n
      );
      assert.equal(
        await f.input.getFunction("allowance")(
          f.adapterAddress, f.routerAddress
        ),
        0n
      );
    });
  }

  for (const scenario of [
  {
    name: "downstream router revert",
    mode: 1,
    error: "SwapBlocked",
    fromRouter: true
  },
  {
    name: "wrong reported output",
    mode: 2,
    error: "OutputAmountMismatch",
    fromRouter: false
  },
  {
    name: "partial input consumption",
    mode: 3,
    error: "UnexpectedInputBalance",
    fromRouter: false
  },
  {
    name: "output below minimum",
    mode: 0,
    error: "InsufficientOutput",
    fromRouter: false
  }
]) {
  it(`rolls back on ${scenario.name}`, async function () {
    const f = await deploySwapFixture();

    await (await f.router.getFunction("configure")(
      f.amountOut, scenario.mode
    )).wait();

    const minimum = scenario.name === "output below minimum"
      ? f.amountOut + 1n
      : f.amountOut;

    let reverted = false;
    try {
      const tx = await f.manager.connect(f.alice)
        .getFunction("executeSwap")(
          f.inputAddress,
          f.outputAddress,
          f.amountIn,
          minimum,
          f.deadline
        );
      await tx.wait();
    } catch (error) {
      const data = (error as { data?: unknown }).data;
      assert.equal(typeof data, "string", "expected contract revert data");

      const contract = scenario.fromRouter ? f.router : f.adapter;
      const parsed = contract.interface.parseError(data as string);
      assert.equal(parsed?.name, scenario.error);
      reverted = true;
    }

    assert.ok(reverted, "expected swap to revert");

    assert.equal(
      await f.input.getFunction("balanceOf")(f.alice.address),
      f.amountIn
    );
    assert.equal(
      await f.input.getFunction("balanceOf")(f.routerAddress),
      0n
    );
    assert.equal(
      await f.output.getFunction("balanceOf")(f.alice.address),
      0n
    );
    assert.equal(
      await f.output.getFunction("balanceOf")(f.routerAddress),
      f.amountOut
    );
    assert.equal(
      await f.manager.getFunction("remaining")(
        f.alice.address, f.inputAddress
      ),
      f.amountIn
    );
    assert.equal(
      await f.input.getFunction("allowance")(
        f.alice.address, f.managerAddress
      ),
      f.amountIn
    );

    for (const address of [f.managerAddress, f.adapterAddress]) {
      assert.equal(
        await f.input.getFunction("balanceOf")(address),
        0n
      );
      assert.equal(
        await f.output.getFunction("balanceOf")(address),
        0n
      );
    }

    assert.equal(
      await f.input.getFunction("allowance")(
        f.managerAddress, f.adapterAddress
      ),
      0n
    );
    assert.equal(
      await f.input.getFunction("allowance")(
        f.adapterAddress, f.routerAddress
      ),
      0n
    );
  });
}

  it("rejects a direct swap call from Alice", async function () {
  const f = await deploySwapFixture();

  let reverted = false;
  try {
    const tx = await f.adapter.connect(f.alice).getFunction("swap")(
      f.inputAddress,
      f.outputAddress,
      f.amountIn,
      f.amountOut,
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

  assert.ok(reverted, "only the configured manager may call swap");
  assert.equal(
    await f.input.getFunction("balanceOf")(f.alice.address),
    f.amountIn
  );
  assert.equal(
    await f.output.getFunction("balanceOf")(f.alice.address),
    0n
  );
});

it("preserves pre-existing adapter input and output balances", async function () {
  const f = await deploySwapFixture();
  const inputBaseline = 7n;
  const outputBaseline = 11n;

  await (await f.input.getFunction("mint")(
    f.adapterAddress, inputBaseline
  )).wait();

  await (await f.output.getFunction("mint")(
    f.adapterAddress, outputBaseline
  )).wait();

  await (await f.manager.connect(f.alice).getFunction("executeSwap")(
    f.inputAddress,
    f.outputAddress,
    f.amountIn,
    f.amountOut,
    f.deadline
  )).wait();

  assert.equal(
    await f.input.getFunction("balanceOf")(f.adapterAddress),
    inputBaseline
  );
  assert.equal(
    await f.output.getFunction("balanceOf")(f.adapterAddress),
    outputBaseline
  );
  assert.equal(
    await f.output.getFunction("balanceOf")(f.alice.address),
    f.amountOut
  );
  assert.equal(
    await f.input.getFunction("balanceOf")(f.routerAddress),
    f.amountIn
  );
  assert.equal(
    await f.input.getFunction("allowance")(
      f.adapterAddress, f.routerAddress
    ),
    0n
  );
});
});