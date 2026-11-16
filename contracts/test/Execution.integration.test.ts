import assert from "node:assert/strict";
import { describe, it } from "mocha";
import { deploySwapFixture } from "./LFJSwapAdapter.test.js";

describe("Execution after preparation", function () {
  for (const scenario of ["revoked", "above cap"] as const) {
    it(`rejects a prepared swap when ${scenario}`, async function () {
      const f = await deploySwapFixture();

      // Prepare transaction bytes while the original budget is sufficient.
      const data = f.manager.interface.encodeFunctionData(
        "executeSwap",
        [
          f.inputAddress,
          f.outputAddress,
          f.amountIn,
          f.amountOut,
          f.deadline,
        ]
      );

      const prepared = {
        to: f.managerAddress,
        data,
        value: 0n,
      };

      // Confirm the same prepared call initially succeeds in simulation.
      const simulated = await f.ethers.provider.call({
        ...prepared,
        from: f.alice.address,
      });

      const [quotedOutput] = f.manager.interface.decodeFunctionResult(
        "executeSwap",
        simulated
      );
      assert.equal(quotedOutput, f.amountOut);

      // Change policy state after preparation.
      let expectedRemaining: bigint;

      if (scenario === "revoked") {
        await (
          await f.manager.connect(f.alice).getFunction("revoke")(
            f.inputAddress
          )
        ).wait();

        expectedRemaining = 0n;
      } else {
        expectedRemaining = f.amountIn - 1n;

        await (
          await f.manager.connect(f.alice).getFunction("setBudget")(
            f.inputAddress,
            expectedRemaining,
            f.deadline
          )
        ).wait();
      }

      const balancesBefore: bigint[] = [];

      for (const token of [f.input, f.output]) {
        for (const address of [
          f.alice.address,
          f.managerAddress,
          f.adapterAddress,
          f.routerAddress,
        ]) {
          balancesBefore.push(
            await token.getFunction("balanceOf")(address)
          );
        }
      }

      const approvalBefore = await f.input.getFunction("allowance")(
        f.alice.address,
        f.managerAddress
      );

      const eventsBefore = await f.manager.queryFilter(
        f.manager.filters.SwapExecuted()
      );

      let rejected = false;

      try {
        const tx = await f.alice.sendTransaction(prepared);
        await tx.wait();
      } catch (error) {
        const revertData = (error as { data?: unknown }).data;
        assert.equal(
          typeof revertData,
          "string",
          "expected contract revert data"
        );

        const parsed = f.manager.interface.parseError(
          revertData as string
        );

        assert.equal(
          parsed?.name,
          scenario === "revoked" ? "BudgetExpired" : "BudgetExceeded"
        );

        if (scenario === "above cap") {
          assert.equal(parsed?.args[0], expectedRemaining);
          assert.equal(parsed?.args[1], f.amountIn);
        }

        rejected = true;
      }

      assert.ok(rejected, "prepared transaction must be rejected");

      const balancesAfter: bigint[] = [];

      for (const token of [f.input, f.output]) {
        for (const address of [
          f.alice.address,
          f.managerAddress,
          f.adapterAddress,
          f.routerAddress,
        ]) {
          balancesAfter.push(
            await token.getFunction("balanceOf")(address)
          );
        }
      }

      assert.deepEqual(balancesAfter, balancesBefore);

      assert.equal(
        await f.manager.getFunction("remaining")(
          f.alice.address,
          f.inputAddress
        ),
        expectedRemaining
      );

      assert.equal(
        await f.input.getFunction("allowance")(
          f.alice.address,
          f.managerAddress
        ),
        approvalBefore
      );

      assert.equal(
        await f.input.getFunction("allowance")(
          f.managerAddress,
          f.adapterAddress
        ),
        0n
      );

      assert.equal(
        await f.input.getFunction("allowance")(
          f.adapterAddress,
          f.routerAddress
        ),
        0n
      );

      const eventsAfter = await f.manager.queryFilter(
        f.manager.filters.SwapExecuted()
      );

      assert.equal(eventsAfter.length, eventsBefore.length);
    });
  }
});