// SPDX-License-Identifier: MIT
pragma solidity ^0.8.28;

import {AllowanceManager} from "./AllowanceManager.sol";
import {INativeBudget} from "./interfaces/INativeBudget.sol";

contract PrecompileAllowanceManager is AllowanceManager {
    address public constant NATIVE_BUDGET =
        0x0300000000000000000000000000000000000000;

    error ManageBudgetDirectlyOnPrecompile();
    error NativeBudgetConsumeFailed();

    function setBudget(address, uint256, uint256) external pure override {
        revert ManageBudgetDirectlyOnPrecompile();
    }

    function revoke(address) external pure override {
        revert ManageBudgetDirectlyOnPrecompile();
    }

    function remaining(address owner, address token)
        external
        view
        override
        returns (uint256)
    {
        return INativeBudget(NATIVE_BUDGET).remaining(owner, token, address(this));
    }

    function _consumeBudget(address owner, address token, uint256 amount)
        internal
        override
    {
        if (token == address(0)) revert InvalidToken();
        if (amount == 0) revert InvalidAmount();

        INativeBudget budget = INativeBudget(NATIVE_BUDGET);
        uint256 available = budget.remaining(owner, token, address(this));
        if (amount > available) revert BudgetExceeded(available, amount);

        // A native precompile may have no Solidity bytecode at its address.
        (bool ok, bytes memory reason) = NATIVE_BUDGET.call(
            abi.encodeCall(INativeBudget.consumeBudget, (owner, token, amount))
        );
        if (!ok) {
            if (reason.length == 0) revert NativeBudgetConsumeFailed();
            assembly ("memory-safe") {
                revert(add(reason, 0x20), mload(reason))
            }
        }

        if (budget.remaining(owner, token, address(this)) != available - amount) {
            revert NativeBudgetConsumeFailed();
        }
    }
}