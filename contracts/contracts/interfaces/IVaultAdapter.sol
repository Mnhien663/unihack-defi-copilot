// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

interface IVaultAdapter {
    /// @notice Pulls approved input tokens from the manager and deposits them.
    /// @dev Implementation must restrict calls to the authorized manager.
    /// @return sharesOut Actual vault shares delivered to recipient.
    function deposit(
        address token,
        uint256 amount,
        uint256 minSharesOut,
        address recipient,
        uint256 deadline
    ) external returns (uint256 sharesOut);
}