import { expect } from "chai";
import { describe, it } from "mocha";
import { network } from "hardhat";

type Flow = "swap" | "deposit";

async function setupExecution(
  flow: Flow,
  inputContractName = "DemoToken"
) {
  const { ethers } = await network.create();
  const [admin, alice, bob] = await ethers.getSigners();

  const manager = await ethers.deployContract(
    "AllowanceManager",
    [],
    admin
  );
  await manager.waitForDeployment();

  const inputToken = await ethers.deployContract(
    inputContractName,
    [],
    admin
  );
  await inputToken.waitForDeployment();

  const outputToken = await ethers.deployContract(
    "DemoToken",
    [],
    admin
  );
  await outputToken.waitForDeployment();

  const managerAddress = await manager.getAddress();
  const inputAddress = await inputToken.getAddress();
  const outputAddress = await outputToken.getAddress();

  const adapter = flow === "swap"
    ? await ethers.deployContract(
        "MockSwapAdapter",
        [managerAddress],
        admin
      )
    : await ethers.deployContract(
        "MockVaultAdapter",
        [managerAddress, outputAddress],
        admin
      );

  await adapter.waitForDeployment();

  const adapterAddress = await adapter.getAddress();

  const amount = ethers.parseUnits("10", 18);
  const budget = amount * 2n;
  const initialBalance = amount * 10n;

  await (
    await inputToken.getFunction("mint")(
      alice.address,
      initialBalance
    )
  ).wait();

  await (
    await outputToken.getFunction("mint")(
      adapterAddress,
      initialBalance
    )
  ).wait();

  await (
    await manager.getFunction("setTokenSupported")(
      inputAddress,
      true
    )
  ).wait();

  await (
    await manager.getFunction("setTokenSupported")(
      outputAddress,
      true
    )
  ).wait();

  if (flow === "swap") {
    await (
      await manager.getFunction("setSwapAdapter")(
        inputAddress,
        outputAddress,
        adapterAddress
      )
    ).wait();
  } else {
    await (
      await manager.getFunction("setVaultAdapter")(
        inputAddress,
        adapterAddress,
        outputAddress
      )
    ).wait();
  }

  const block = await ethers.provider.getBlock("latest");

  if (block === null) {
    throw new Error("Latest block not found");
  }

  const expiry = block.timestamp + 7200;
  const deadline = block.timestamp + 3600;

  await (
    await manager.connect(alice).getFunction("setBudget")(
      inputAddress,
      budget,
      expiry
    )
  ).wait();

  await (
    await inputToken.connect(alice).getFunction("approve")(
      managerAddress,
      initialBalance
    )
  ).wait();

  const executeAsAlice = (
    requested = amount,
    minimum = requested,
    executionDeadline = deadline
  ) => {
    if (flow === "swap") {
      return manager.connect(alice).getFunction("executeSwap")(
        inputAddress,
        outputAddress,
        requested,
        minimum,
        executionDeadline
      );
    }

    return manager.connect(alice).getFunction("executeDeposit")(
      inputAddress,
      requested,
      minimum,
      executionDeadline
    );
  };

  return {
    ethers,
    admin,
    alice,
    bob,
    manager,
    inputToken,
    outputToken,
    adapter,
    managerAddress,
    inputAddress,
    outputAddress,
    adapterAddress,
    amount,
    budget,
    initialBalance,
    expiry,
    deadline,
    executeAsAlice,
  };
}

for (const flow of ["swap", "deposit"] as const) {
  describe(`AllowanceManager ${flow} execution`, function () {
    it("executes, pays the owner, consumes budget and clears allowance", async function () {
      const f = await setupExecution(flow);

      const eventName = flow === "swap"
        ? "SwapExecuted"
        : "DepositExecuted";

      await expect(f.executeAsAlice())
        .to.emit(f.manager, eventName)
        .withArgs(
          f.alice.address,
          f.inputAddress,
          f.outputAddress,
          f.adapterAddress,
          f.amount,
          f.amount
        );

      expect(
        await f.manager.remaining(
          f.alice.address,
          f.inputAddress
        )
      ).to.equal(f.budget - f.amount);

      expect(
        await f.inputToken.balanceOf(f.alice.address)
      ).to.equal(f.initialBalance - f.amount);

      expect(
        await f.outputToken.balanceOf(f.alice.address)
      ).to.equal(f.amount);

      expect(
        await f.inputToken.balanceOf(f.adapterAddress)
      ).to.equal(f.amount);

      expect(
        await f.outputToken.balanceOf(f.adapterAddress)
      ).to.equal(f.initialBalance - f.amount);

      expect(
        await f.inputToken.balanceOf(f.managerAddress)
      ).to.equal(0n);

      expect(
        await f.outputToken.balanceOf(f.managerAddress)
      ).to.equal(0n);

      expect(
        await f.inputToken.allowance(
          f.managerAddress,
          f.adapterAddress
        )
      ).to.equal(0n);
    });

    it("allows execution exactly at the budget cap", async function () {
      const f = await setupExecution(flow);

      await (
        await f.executeAsAlice(f.budget)
      ).wait();

      expect(
        await f.manager.remaining(
          f.alice.address,
          f.inputAddress
        )
      ).to.equal(0n);

      expect(
        await f.outputToken.balanceOf(f.alice.address)
      ).to.equal(f.budget);
    });

    it("rejects execution one raw unit above the budget", async function () {
      const f = await setupExecution(flow);

      await expect(
        f.executeAsAlice(f.budget + 1n)
      )
        .to.be.revertedWithCustomError(
          f.manager,
          "BudgetExceeded"
        )
        .withArgs(f.budget, f.budget + 1n);

      expect(
        await f.manager.remaining(
          f.alice.address,
          f.inputAddress
        )
      ).to.equal(f.budget);

      expect(
        await f.inputToken.balanceOf(f.alice.address)
      ).to.equal(f.initialBalance);

      expect(
        await f.inputToken.balanceOf(f.adapterAddress)
      ).to.equal(0n);
    });

    it("does not let Bob use Alice's budget or approval", async function () {
      const f = await setupExecution(flow);

      const execution = flow === "swap"
        ? f.manager.connect(f.bob).getFunction("executeSwap")(
            f.inputAddress,
            f.outputAddress,
            f.amount,
            f.amount,
            f.deadline
          )
        : f.manager.connect(f.bob).getFunction("executeDeposit")(
            f.inputAddress,
            f.amount,
            f.amount,
            f.deadline
          );

      await expect(execution)
        .to.be.revertedWithCustomError(
          f.manager,
          "BudgetExpired"
        );

      expect(
        await f.manager.remaining(
          f.alice.address,
          f.inputAddress
        )
      ).to.equal(f.budget);

      expect(
        await f.inputToken.balanceOf(f.alice.address)
      ).to.equal(f.initialBalance);
    });

    it("rejects a disabled input token", async function () {
      const f = await setupExecution(flow);

      await (
        await f.manager.getFunction("setTokenSupported")(
          f.inputAddress,
          false
        )
      ).wait();

      await expect(f.executeAsAlice())
        .to.be.revertedWithCustomError(
          f.manager,
          "UnsupportedToken"
        )
        .withArgs(f.inputAddress);
    });

    it("rejects a disabled output or share token", async function () {
      const f = await setupExecution(flow);

      await (
        await f.manager.getFunction("setTokenSupported")(
          f.outputAddress,
          false
        )
      ).wait();

      await expect(f.executeAsAlice())
        .to.be.revertedWithCustomError(
          f.manager,
          "UnsupportedToken"
        )
        .withArgs(f.outputAddress);
    });

    it("rejects a disabled route", async function () {
      const f = await setupExecution(flow);

      if (flow === "swap") {
        await (
          await f.manager.getFunction("setSwapAdapter")(
            f.inputAddress,
            f.outputAddress,
            f.ethers.ZeroAddress
          )
        ).wait();
      } else {
        await (
          await f.manager.getFunction("setVaultAdapter")(
            f.inputAddress,
            f.ethers.ZeroAddress,
            f.ethers.ZeroAddress
          )
        ).wait();
      }

      await expect(f.executeAsAlice())
        .to.be.revertedWithCustomError(
          f.manager,
          "RouteNotConfigured"
        );
    });

    it("rejects an expired execution deadline", async function () {
      const f = await setupExecution(flow);

      await f.ethers.provider.send(
        "evm_setNextBlockTimestamp",
        [f.deadline + 1]
      );

      await expect(f.executeAsAlice())
        .to.be.revertedWithCustomError(
          f.manager,
          "DeadlineExpired"
        );
    });

    it("rejects a zero minimum output", async function () {
      const f = await setupExecution(flow);

      await expect(
        f.executeAsAlice(f.amount, 0n)
      ).to.be.revertedWithCustomError(
        f.manager,
        "InvalidMinimumOutput"
      );
    });

    it("rejects direct adapter calls from Alice", async function () {
      const f = await setupExecution(flow);

      const execution = flow === "swap"
        ? f.adapter.connect(f.alice).getFunction("swap")(
            f.inputAddress,
            f.outputAddress,
            f.amount,
            f.amount,
            f.alice.address,
            f.deadline
          )
        : f.adapter.connect(f.alice).getFunction("deposit")(
            f.inputAddress,
            f.amount,
            f.amount,
            f.alice.address,
            f.deadline
          );

      await expect(execution)
        .to.be.revertedWithCustomError(
          f.adapter,
          "UnauthorizedManager"
        );
    });
  });
}

for (const flow of ["swap", "deposit"] as const) {
  describe(`AllowanceManager ${flow} adapter failures`, function () {
    async function assertUnchanged(
      f: Awaited<ReturnType<typeof setupExecution>>,
      ownerAllowanceBefore: bigint
    ) {
      expect(
        await f.manager.remaining(
          f.alice.address,
          f.inputAddress
        )
      ).to.equal(f.budget);

      expect(
        await f.inputToken.balanceOf(f.alice.address)
      ).to.equal(f.initialBalance);

      expect(
        await f.inputToken.balanceOf(f.managerAddress)
      ).to.equal(0n);

      expect(
        await f.inputToken.balanceOf(f.adapterAddress)
      ).to.equal(0n);

      expect(
        await f.outputToken.balanceOf(f.alice.address)
      ).to.equal(0n);

      expect(
        await f.outputToken.balanceOf(f.bob.address)
      ).to.equal(0n);

      expect(
        await f.outputToken.balanceOf(f.adapterAddress)
      ).to.equal(f.initialBalance);

      expect(
        await f.inputToken.allowance(
          f.alice.address,
          f.managerAddress
        )
      ).to.equal(ownerAllowanceBefore);

      expect(
        await f.inputToken.allowance(
          f.managerAddress,
          f.adapterAddress
        )
      ).to.equal(0n);
    }

    it("rolls back budget, tokens and approvals on downstream revert", async function () {
      const f = await setupExecution(flow);

      await (
        await f.adapter.getFunction("setMode")(
          1,
          f.ethers.ZeroAddress
        )
      ).wait();

      const allowanceBefore = await f.inputToken.allowance(
        f.alice.address,
        f.managerAddress
      );

      await expect(f.executeAsAlice())
        .to.be.revertedWithCustomError(
          f.adapter,
          "ForcedAdapterFailure"
        );

      await assertUnchanged(f, allowanceBefore);
    });

    it("rejects output sent to Bob and rolls back the transfer", async function () {
      const f = await setupExecution(flow);

      await (
        await f.adapter.getFunction("setMode")(
          2,
          f.bob.address
        )
      ).wait();

      const allowanceBefore = await f.inputToken.allowance(
        f.alice.address,
        f.managerAddress
      );

      await expect(f.executeAsAlice())
        .to.be.revertedWithCustomError(
          f.manager,
          "InsufficientOutput"
        )
        .withArgs(0n, f.amount);

      await assertUnchanged(f, allowanceBefore);
    });

    it("rejects a reported amount different from actual owner output", async function () {
      const f = await setupExecution(flow);

      await (
        await f.adapter.getFunction("setMode")(
          3,
          f.ethers.ZeroAddress
        )
      ).wait();

      const allowanceBefore = await f.inputToken.allowance(
        f.alice.address,
        f.managerAddress
      );

      await expect(f.executeAsAlice())
        .to.be.revertedWithCustomError(
          f.manager,
          "OutputAmountMismatch"
        )
        .withArgs(f.amount + 1n, f.amount);

      await assertUnchanged(f, allowanceBefore);
    });

    it("rejects partial input consumption even when output meets the minimum", async function () {
      const f = await setupExecution(flow);

      await (
        await f.adapter.getFunction("setMode")(
          4,
          f.ethers.ZeroAddress
        )
      ).wait();

      const allowanceBefore = await f.inputToken.allowance(
        f.alice.address,
        f.managerAddress
      );

      await expect(
        f.executeAsAlice(f.amount, f.amount / 2n)
      ).to.be.revertedWithCustomError(
        f.manager,
        "UnexpectedInputBalance"
      );

      await assertUnchanged(f, allowanceBefore);
    });

    it("preserves pre-existing manager input on successful execution", async function () {
      const f = await setupExecution(flow);

      const preExisting = 7n;

      await (
        await f.inputToken.getFunction("mint")(
          f.managerAddress,
          preExisting
        )
      ).wait();

      await (await f.executeAsAlice()).wait();

      expect(
        await f.inputToken.balanceOf(f.managerAddress)
      ).to.equal(preExisting);

      expect(
        await f.inputToken.balanceOf(f.adapterAddress)
      ).to.equal(f.amount);

      expect(
        await f.outputToken.balanceOf(f.alice.address)
      ).to.equal(f.amount);

      expect(
        await f.manager.remaining(
          f.alice.address,
          f.inputAddress
        )
      ).to.equal(f.budget - f.amount);

      expect(
        await f.inputToken.allowance(
          f.managerAddress,
          f.adapterAddress
        )
      ).to.equal(0n);
    });
  });
}

for (const flow of ["swap", "deposit"] as const) {
  describe(`AllowanceManager ${flow} false-return token`, function () {
    it("rejects transferFrom returning false and restores the budget", async function () {
      const f = await setupExecution(
        flow,
        "MockFalseReturnToken"
      );

      const allowanceBefore = await f.inputToken.allowance(
        f.alice.address,
        f.managerAddress
      );

      await expect(f.executeAsAlice())
        .to.be.revertedWithCustomError(
          f.manager,
          "SafeERC20FailedOperation"
        )
        .withArgs(f.inputAddress);

      expect(
        await f.manager.remaining(
          f.alice.address,
          f.inputAddress
        )
      ).to.equal(f.budget);

      expect(
        await f.inputToken.balanceOf(f.alice.address)
      ).to.equal(f.initialBalance);

      expect(
        await f.inputToken.balanceOf(f.managerAddress)
      ).to.equal(0n);

      expect(
        await f.inputToken.balanceOf(f.adapterAddress)
      ).to.equal(0n);

      expect(
        await f.outputToken.balanceOf(f.alice.address)
      ).to.equal(0n);

      expect(
        await f.outputToken.balanceOf(f.adapterAddress)
      ).to.equal(f.initialBalance);

      expect(
        await f.inputToken.allowance(
          f.alice.address,
          f.managerAddress
        )
      ).to.equal(allowanceBefore);

      expect(
        await f.inputToken.allowance(
          f.managerAddress,
          f.adapterAddress
        )
      ).to.equal(0n);
    });
  });
}

for (const flow of ["swap", "deposit"] as const) {
  describe(`AllowanceManager ${flow} reentrancy`, function () {
    for (const callback of [
      "executeSwap",
      "executeDeposit",
      "setBudget",
      "revoke",
    ] as const) {
      it(`blocks a callback into ${callback} while allowing the outer execution`, async function () {
        const f = await setupExecution(flow);

        let callbackData: string;

        switch (callback) {
          case "executeSwap":
            callbackData = f.manager.interface.encodeFunctionData(
              "executeSwap",
              [
                f.inputAddress,
                f.outputAddress,
                f.amount,
                f.amount,
                f.deadline,
              ]
            );
            break;

          case "executeDeposit":
            callbackData = f.manager.interface.encodeFunctionData(
              "executeDeposit",
              [
                f.inputAddress,
                f.amount,
                f.amount,
                f.deadline,
              ]
            );
            break;

          case "setBudget":
            callbackData = f.manager.interface.encodeFunctionData(
              "setBudget",
              [
                f.inputAddress,
                f.budget,
                f.expiry,
              ]
            );
            break;

          case "revoke":
            callbackData = f.manager.interface.encodeFunctionData(
              "revoke",
              [f.inputAddress]
            );
            break;
        }

        await (
          await f.adapter.getFunction("configureCallback")(
            callbackData
          )
        ).wait();

        await (await f.executeAsAlice()).wait();

        expect(
          await f.adapter.getFunction("callbackAttempted")()
        ).to.equal(true);

        const revertData = await f.adapter
          .getFunction("callbackRevertData")();

        const decodedError = f.manager.interface.parseError(
          revertData
        );

        expect(decodedError?.name).to.equal(
          "ReentrancyGuardReentrantCall"
        );

        expect(
          await f.manager.remaining(
            f.alice.address,
            f.inputAddress
          )
        ).to.equal(f.budget - f.amount);

        expect(
          await f.inputToken.balanceOf(f.alice.address)
        ).to.equal(f.initialBalance - f.amount);

        expect(
          await f.outputToken.balanceOf(f.alice.address)
        ).to.equal(f.amount);

        expect(
          await f.inputToken.balanceOf(f.adapterAddress)
        ).to.equal(f.amount);

        expect(
          await f.inputToken.balanceOf(f.managerAddress)
        ).to.equal(0n);

        expect(
          await f.inputToken.allowance(
            f.managerAddress,
            f.adapterAddress
          )
        ).to.equal(0n);
      });
    }
  });
}

describe("AllowanceManager policy permissions", function () {
  for (const operation of [
    "setTokenSupported",
    "setSwapAdapter",
    "setVaultAdapter",
  ] as const) {
    it(`rejects ${operation} from a non-admin`, async function () {
      const f = await setupExecution("swap");

      const managerAsBob = f.manager.connect(f.bob);

      const transaction = operation === "setTokenSupported"
        ? managerAsBob.getFunction(operation)(
            f.inputAddress,
            false
          )
        : operation === "setSwapAdapter"
          ? managerAsBob.getFunction(operation)(
              f.inputAddress,
              f.outputAddress,
              f.ethers.ZeroAddress
            )
          : managerAsBob.getFunction(operation)(
              f.inputAddress,
              f.adapterAddress,
              f.outputAddress
            );

      await expect(transaction)
        .to.be.revertedWithCustomError(
          f.manager,
          "UnauthorizedPolicyAdmin"
        );

      expect(
        await f.manager.supportedTokens(f.inputAddress)
      ).to.equal(true);

      expect(
        await f.manager.swapAdapters(
          f.inputAddress,
          f.outputAddress
        )
      ).to.equal(f.adapterAddress);

      const vaultConfig = await f.manager.vaultConfigs(
        f.inputAddress
      );

      expect(vaultConfig.adapter).to.equal(
        f.ethers.ZeroAddress
      );
    });
  }
});

for (const flow of ["swap", "deposit"] as const) {
  describe(`AllowanceManager ${flow} execution conditions`, function () {
    it("rejects execution exactly at budget expiry", async function () {
      const f = await setupExecution(flow);

      // Keep execution deadline valid so budget expiry is tested.
      const laterDeadline = f.expiry + 3600;

      await f.ethers.provider.send(
        "evm_setNextBlockTimestamp",
        [f.expiry]
      );

      await expect(
        f.executeAsAlice(
          f.amount,
          f.amount,
          laterDeadline
        )
      ).to.be.revertedWithCustomError(
        f.manager,
        "BudgetExpired"
      );

      expect(
        await f.inputToken.balanceOf(f.alice.address)
      ).to.equal(f.initialBalance);

      expect(
        await f.inputToken.balanceOf(f.adapterAddress)
      ).to.equal(0n);
    });

    it("rejects execution after owner revocation", async function () {
      const f = await setupExecution(flow);

      await (
        await f.manager.connect(f.alice).getFunction("revoke")(
          f.inputAddress
        )
      ).wait();

      await expect(f.executeAsAlice())
        .to.be.revertedWithCustomError(
          f.manager,
          "BudgetExpired"
        );

      expect(
        await f.manager.remaining(
          f.alice.address,
          f.inputAddress
        )
      ).to.equal(0n);

      expect(
        await f.inputToken.balanceOf(f.alice.address)
      ).to.equal(f.initialBalance);

      expect(
        await f.outputToken.balanceOf(f.alice.address)
      ).to.equal(0n);
    });

    it("restores budget when ERC-20 approval is insufficient", async function () {
      const f = await setupExecution(flow);

      const approved = f.amount - 1n;

      await (
        await f.inputToken.connect(f.alice).getFunction("approve")(
          f.managerAddress,
          approved
        )
      ).wait();

      await expect(f.executeAsAlice())
        .to.be.revertedWithCustomError(
          f.inputToken,
          "ERC20InsufficientAllowance"
        )
        .withArgs(
          f.managerAddress,
          approved,
          f.amount
        );

      expect(
        await f.manager.remaining(
          f.alice.address,
          f.inputAddress
        )
      ).to.equal(f.budget);

      expect(
        await f.inputToken.balanceOf(f.alice.address)
      ).to.equal(f.initialBalance);

      expect(
        await f.inputToken.allowance(
          f.alice.address,
          f.managerAddress
        )
      ).to.equal(approved);

      expect(
        await f.inputToken.balanceOf(f.managerAddress)
      ).to.equal(0n);
    });

    it("rolls back when the adapter cannot meet minimum output", async function () {
      const f = await setupExecution(flow);

      await expect(
        f.executeAsAlice(f.amount, f.amount + 1n)
      ).to.be.revertedWithCustomError(
        f.adapter,
        "AdapterMinimumNotMet"
      );

      expect(
        await f.manager.remaining(
          f.alice.address,
          f.inputAddress
        )
      ).to.equal(f.budget);

      expect(
        await f.inputToken.balanceOf(f.alice.address)
      ).to.equal(f.initialBalance);

      expect(
        await f.inputToken.balanceOf(f.adapterAddress)
      ).to.equal(0n);

      expect(
        await f.outputToken.balanceOf(f.alice.address)
      ).to.equal(0n);

      expect(
        await f.outputToken.balanceOf(f.adapterAddress)
      ).to.equal(f.initialBalance);

      expect(
        await f.inputToken.allowance(
          f.managerAddress,
          f.adapterAddress
        )
      ).to.equal(0n);
    });
  });
}

for (const flow of ["swap", "deposit"] as const) {
  describe(`AllowanceManager ${flow} fee-on-transfer input`, function () {
    it("rejects a short input receipt and rolls back balances, fee and budget", async function () {
      const f = await setupExecution(flow, "MockFeeToken");

      const supplyBefore = await f.inputToken.totalSupply();

      const allowanceBefore = await f.inputToken.allowance(
        f.alice.address,
        f.managerAddress
      );

      await expect(f.executeAsAlice())
        .to.be.revertedWithCustomError(
          f.manager,
          "UnexpectedInputBalance"
        );

      expect(
        await f.manager.remaining(
          f.alice.address,
          f.inputAddress
        )
      ).to.equal(f.budget);

      expect(
        await f.inputToken.balanceOf(f.alice.address)
      ).to.equal(f.initialBalance);

      expect(
        await f.inputToken.balanceOf(f.managerAddress)
      ).to.equal(0n);

      expect(
        await f.inputToken.balanceOf(f.adapterAddress)
      ).to.equal(0n);

      expect(
        await f.inputToken.totalSupply()
      ).to.equal(supplyBefore);

      expect(
        await f.inputToken.allowance(
          f.alice.address,
          f.managerAddress
        )
      ).to.equal(allowanceBefore);

      expect(
        await f.inputToken.allowance(
          f.managerAddress,
          f.adapterAddress
        )
      ).to.equal(0n);

      expect(
        await f.outputToken.balanceOf(f.alice.address)
      ).to.equal(0n);

      expect(
        await f.outputToken.balanceOf(f.adapterAddress)
      ).to.equal(f.initialBalance);
    });
  });
}

for (const flow of ["swap", "deposit"] as const) {
  describe(`AllowanceManager ${flow} adapter allowance`, function () {
    it("approves exactly the execution amount and clears it afterward", async function () {
      const f = await setupExecution(flow);

      const ownerAllowanceBefore = await f.inputToken.allowance(
        f.alice.address,
        f.managerAddress
      );

      expect(ownerAllowanceBefore).to.be.greaterThan(f.amount);

      await (await f.executeAsAlice()).wait();

      expect(
        await f.adapter.getFunction("observedAllowance")()
      ).to.equal(f.amount);

      expect(
        await f.inputToken.allowance(
          f.managerAddress,
          f.adapterAddress
        )
      ).to.equal(0n);

      expect(
        await f.inputToken.allowance(
          f.alice.address,
          f.managerAddress
        )
      ).to.equal(ownerAllowanceBefore - f.amount);

      expect(
        await f.manager.remaining(
          f.alice.address,
          f.inputAddress
        )
      ).to.equal(f.budget - f.amount);
    });

    it("rejects an adapter pulling one raw unit above its allowance", async function () {
      const f = await setupExecution(flow);

      // Existing mode numbers stay unchanged; OverPull is mode 6.
      await (
        await f.adapter.getFunction("setMode")(
          6,
          f.ethers.ZeroAddress
        )
      ).wait();

      const ownerAllowanceBefore = await f.inputToken.allowance(
        f.alice.address,
        f.managerAddress
      );

      // Ensure failure is caused by allowance, not manager balance.
      const preExisting = 7n;

      await (
        await f.inputToken.getFunction("mint")(
          f.managerAddress,
          preExisting
        )
      ).wait();

      await expect(f.executeAsAlice())
        .to.be.revertedWithCustomError(
          f.inputToken,
          "ERC20InsufficientAllowance"
        )
        .withArgs(
          f.adapterAddress,
          f.amount,
          f.amount + 1n
        );

      expect(
        await f.manager.remaining(
          f.alice.address,
          f.inputAddress
        )
      ).to.equal(f.budget);

      expect(
        await f.inputToken.balanceOf(f.alice.address)
      ).to.equal(f.initialBalance);

      expect(
        await f.inputToken.balanceOf(f.managerAddress)
      ).to.equal(preExisting);

      expect(
        await f.inputToken.balanceOf(f.adapterAddress)
      ).to.equal(0n);

      expect(
        await f.outputToken.balanceOf(f.alice.address)
      ).to.equal(0n);

      expect(
        await f.outputToken.balanceOf(f.adapterAddress)
      ).to.equal(f.initialBalance);

      expect(
        await f.inputToken.allowance(
          f.alice.address,
          f.managerAddress
        )
      ).to.equal(ownerAllowanceBefore);

      expect(
        await f.inputToken.allowance(
          f.managerAddress,
          f.adapterAddress
        )
      ).to.equal(0n);
    });
  });
}