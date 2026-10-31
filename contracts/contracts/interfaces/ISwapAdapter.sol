// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

interface ISwapAdapter {
    /// @notice Swaps input tokens already forwarded by the manager.
    /// @dev Implementation must restrict calls to the authorized manager.
    /// @return amountOut Actual output amount delivered to recipient.
    function swap(
        address tokenIn,
        address tokenOut,
        uint256 amountIn,
        uint256 minAmountOut,
        address recipient,
        uint256 deadline
    ) external returns (uint256 amountOut);
}