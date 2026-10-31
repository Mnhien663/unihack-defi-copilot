// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import {AllowanceManager} from "../AllowanceManager.sol";

contract AllowanceManagerHarness is AllowanceManager {
    error ForcedFailure();

    function consumeBudget(
        address token,
        uint256 amount
    ) external {
        _consumeBudget(msg.sender, token, amount);
    }

    function consumeThenRevert(
        address token,
        uint256 amount
    ) external {
        _consumeBudget(msg.sender, token, amount);

        revert ForcedFailure();
    }
}