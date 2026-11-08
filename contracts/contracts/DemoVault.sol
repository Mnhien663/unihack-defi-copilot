// SPDX-License-Identifier: MIT
pragma solidity ^0.8.28;

import {ERC20} from "@openzeppelin/contracts/token/ERC20/ERC20.sol";
import {IERC20} from "@openzeppelin/contracts/token/ERC20/IERC20.sol";
import {ERC4626} from "@openzeppelin/contracts/token/ERC20/extensions/ERC4626.sol";

/// @notice Demo vault for testing asset/share reads and deposit flows.
/// @dev Holds its underlying token directly. No yield strategy is implemented.
contract DemoVault is ERC4626 {
    error InvalidUnderlyingAsset();

    constructor(IERC20 underlying)
        ERC20("DeFi Copilot Demo Vault Share", "dvDDEMO")
        ERC4626(underlying)
    {
        if (
            address(underlying) == address(0) ||
            address(underlying).code.length == 0
        ) {
            revert InvalidUnderlyingAsset();
        }
    }
}