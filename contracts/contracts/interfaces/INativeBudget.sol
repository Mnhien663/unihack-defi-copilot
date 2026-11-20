// SPDX-License-Identifier: MIT
pragma solidity ^0.8.28;

/// @notice Interface for a custom-L1 native precompile.
/// Deploying this interface does not install the precompile.
interface INativeBudget {
    /// @notice Replace the caller's budget for token and spender.
    /// @param expiry Expiry as a Unix timestamp in seconds.
    function setBudget(
        address token,
        address spender,
        uint256 amount,
        uint256 expiry
    ) external;

    /// @notice Clear the caller's budget for token and spender.
    function revokeBudget(
        address token,
        address spender
    ) external;

    /// @notice Effective remaining amount; zero when unset or expired.
    function remaining(
        address owner,
        address token,
        address spender
    ) external view returns (uint256 amount);

    /// @notice Consume the budget granted by owner to msg.sender.
    /// @dev Must revert if expired, insufficient, or amount is zero.
    function consumeBudget(
        address owner,
        address token,
        uint256 amount
    ) external;
}