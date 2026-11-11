// SPDX-License-Identifier: MIT
pragma solidity ^0.8.28;

import {IERC20} from "@openzeppelin/contracts/token/ERC20/IERC20.sol";
import {IERC4626} from "@openzeppelin/contracts/interfaces/IERC4626.sol";
import {SafeERC20} from "@openzeppelin/contracts/token/ERC20/utils/SafeERC20.sol";
import {ReentrancyGuard} from "@openzeppelin/contracts/utils/ReentrancyGuard.sol";

import {IVaultAdapter} from "../interfaces/IVaultAdapter.sol";

/// @notice Deposits one configured underlying into one ERC-4626 vault.
/// @dev Only the configured manager may call deposit.
contract VaultAdapter is IVaultAdapter, ReentrancyGuard {
    using SafeERC20 for IERC20;

    address public immutable manager;
    IERC4626 public immutable vault;
    IERC20 public immutable underlying;

    error InvalidContract(address account);
    error UnauthorizedManager();
    error UnsupportedToken();
    error InvalidRecipient();
    error InvalidAmount();
    error InvalidMinimumShares();
    error DeadlineExpired();
    error UnexpectedInputBalance();
    error InsufficientShares(uint256 received, uint256 minimum);
    error ShareAmountMismatch(uint256 reported, uint256 received);
    error VaultAllowanceNotCleared();

    constructor(address manager_, address vault_) {
        if (manager_ == address(0) || manager_.code.length == 0) {
            revert InvalidContract(manager_);
        }
        if (vault_ == address(0) || vault_.code.length == 0) {
            revert InvalidContract(vault_);
        }

        IERC4626 selectedVault = IERC4626(vault_);
        address asset = selectedVault.asset();

        if (
            asset == address(0) ||
            asset == vault_ ||
            asset.code.length == 0
        ) {
            revert InvalidContract(asset);
        }

        manager = manager_;
        vault = selectedVault;
        underlying = IERC20(asset);
    }

    function deposit(
        address token,
        uint256 amount,
        uint256 minSharesOut,
        address recipient,
        uint256 deadline
    ) external override nonReentrant returns (uint256 sharesOut) {
        if (msg.sender != manager) {
            revert UnauthorizedManager();
        }
        if (token != address(underlying)) {
            revert UnsupportedToken();
        }
        if (
            recipient == address(0) ||
            recipient == address(this) ||
            recipient == manager ||
            recipient == address(vault)
        ) {
            revert InvalidRecipient();
        }
        if (amount == 0) {
            revert InvalidAmount();
        }
        if (minSharesOut == 0) {
            revert InvalidMinimumShares();
        }
        if (block.timestamp > deadline) {
            revert DeadlineExpired();
        }

        uint256 adapterInputBefore = underlying.balanceOf(address(this));
        uint256 managerInputBefore = underlying.balanceOf(manager);
        uint256 recipientSharesBefore = vault.balanceOf(recipient);

        underlying.safeTransferFrom(manager, address(this), amount);

        if (
            underlying.balanceOf(address(this))
                != adapterInputBefore + amount ||
            underlying.balanceOf(manager) + amount
                != managerInputBefore
        ) {
            revert UnexpectedInputBalance();
        }

        underlying.forceApprove(address(vault), amount);

        uint256 reportedShares = vault.deposit(amount, recipient);

        underlying.forceApprove(address(vault), 0);

        if (
            underlying.allowance(address(this), address(vault)) != 0
        ) {
            revert VaultAllowanceNotCleared();
        }
        if (
            underlying.balanceOf(address(this)) != adapterInputBefore ||
            underlying.balanceOf(manager) + amount != managerInputBefore
        ) {
            revert UnexpectedInputBalance();
        }

        uint256 recipientSharesAfter = vault.balanceOf(recipient);
        if (recipientSharesAfter < recipientSharesBefore) {
            revert ShareAmountMismatch(reportedShares, 0);
        }

        sharesOut = recipientSharesAfter - recipientSharesBefore;

        if (sharesOut < minSharesOut) {
            revert InsufficientShares(sharesOut, minSharesOut);
        }
        if (reportedShares != sharesOut) {
            revert ShareAmountMismatch(reportedShares, sharesOut);
        }
    }
}