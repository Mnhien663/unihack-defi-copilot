// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import {IERC20} from "@openzeppelin/contracts/token/ERC20/IERC20.sol";
import {SafeERC20} from "@openzeppelin/contracts/token/ERC20/utils/SafeERC20.sol";

import {IVaultAdapter} from "../interfaces/IVaultAdapter.sol";

contract MockVaultAdapter is IVaultAdapter {
    using SafeERC20 for IERC20;

    address public immutable manager;
    address public immutable controller;
    address public immutable shareToken;

    enum Mode {
        Normal,
        RevertAfterPull,
        WrongRecipient,
        WrongReportedAmount,
        PartialPull,
        Reenter,
        OverPull
    }

    Mode public mode;
    address public alternateRecipient;
    bytes public callbackData;
    bool public callbackAttempted;
    bytes public callbackRevertData;
    uint256 public observedAllowance;

    error UnexpectedCallbackSuccess();
    error UnauthorizedManager();
    error UnauthorizedController();
    error ForcedAdapterFailure();
    error InvalidRecipient();
    error AdapterDeadlineExpired();
    error AdapterMinimumNotMet();

    constructor(
        address managerAddress,
        address shareTokenAddress
    ) {
        manager = managerAddress;
        controller = msg.sender;
        shareToken = shareTokenAddress;
    }

    function setMode(
        Mode newMode,
        address newAlternateRecipient
    ) external {
        if (msg.sender != controller) {
            revert UnauthorizedController();
        }

        mode = newMode;
        alternateRecipient = newAlternateRecipient;
    }

    function configureCallback(bytes calldata data) external {
        if (msg.sender != controller) {
            revert UnauthorizedController();
        }

        callbackData = data;
        callbackAttempted = false;
        delete callbackRevertData;

        mode = Mode.Reenter;
    }

    function deposit(
        address token,
        uint256 amount,
        uint256 minSharesOut,
        address recipient,
        uint256 deadline
    ) external override returns (uint256 sharesOut) {
        if (msg.sender != manager) {
            revert UnauthorizedManager();
        }

        if (recipient == address(0)) {
            revert InvalidRecipient();
        }

        if (block.timestamp > deadline) {
            revert AdapterDeadlineExpired();
        }

        if (mode == Mode.Reenter) {
            callbackAttempted = true;

            (bool success, bytes memory result) = manager.call(
                callbackData
            );

            if (success) {
                revert UnexpectedCallbackSuccess();
            }

            callbackRevertData = result;
        }

        observedAllowance = IERC20(token).allowance(
            manager,
            address(this)
        );

        uint256 pulledAmount = amount;

        if (mode == Mode.PartialPull) {
            pulledAmount = amount / 2;
        } else if (mode == Mode.OverPull) {
            pulledAmount = amount + 1;
        }

        IERC20(token).safeTransferFrom(
            manager,
            address(this),
            pulledAmount
        );

        if (mode == Mode.RevertAfterPull) {
            revert ForcedAdapterFailure();
        }

        // Mock uses a 1:1 raw-unit conversion into shares.
        sharesOut = pulledAmount;

        if (sharesOut < minSharesOut) {
            revert AdapterMinimumNotMet();
        }

        address actualRecipient = mode == Mode.WrongRecipient
            ? alternateRecipient
            : recipient;

        IERC20(shareToken).safeTransfer(
            actualRecipient,
            sharesOut
        );

        if (mode == Mode.WrongReportedAmount) {
            return sharesOut + 1;
        }

        return sharesOut;
    }
}