// SPDX-License-Identifier: MIT
pragma solidity ^0.8.28;

import {IERC20} from "@openzeppelin/contracts/token/ERC20/IERC20.sol";
import {DemoVault} from "../DemoVault.sol";

contract MockRevertingVault is DemoVault {
    error DepositBlocked();

    constructor(IERC20 underlying) DemoVault(underlying) {}

    function deposit(
        uint256,
        address
    ) public pure override returns (uint256) {
        revert DepositBlocked();
    }
}