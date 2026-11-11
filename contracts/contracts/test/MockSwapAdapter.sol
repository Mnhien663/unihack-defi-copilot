// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import {IERC20} from "@openzeppelin/contracts/token/ERC20/IERC20.sol";
import {SafeERC20} from "@openzeppelin/contracts/token/ERC20/utils/SafeERC20.sol";

import {ISwapAdapter} from "../interfaces/ISwapAdapter.sol";

contract MockSwapAdapter is ISwapAdapter {
    using SafeERC20 for IERC20;

    address public immutable manager;
    address public immutable controller;

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

    constructor(address managerAddress) {
        manager = managerAddress;
        controller = msg.sender;
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

    function swap(
        address tokenIn,
        address tokenOut,
        uint256 amountIn,
        uint256 minAmountOut,
        address recipient,
        uint256 deadline
    ) external override returns (uint256 amountOut) {
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

        observedAllowance = IERC20(tokenIn).allowance(
            manager,
            address(this)
        );

        uint256 pulledAmount = amountIn;

        if (mode == Mode.PartialPull) {
            pulledAmount = amountIn / 2;
        } else if (mode == Mode.OverPull) {
            pulledAmount = amountIn + 1;
        }

        IERC20(tokenIn).safeTransferFrom(
            manager,
            address(this),
            pulledAmount
        );

        if (mode == Mode.RevertAfterPull) {
            revert ForcedAdapterFailure();
        }

        // Mock uses a 1:1 raw-unit exchange rate.
        amountOut = pulledAmount;

        if (amountOut < minAmountOut) {
            revert AdapterMinimumNotMet();
        }

        address actualRecipient = mode == Mode.WrongRecipient
            ? alternateRecipient
            : recipient;

        IERC20(tokenOut).safeTransfer(
            actualRecipient,
            amountOut
        );

        if (mode == Mode.WrongReportedAmount) {
            return amountOut + 1;
        }

        return amountOut;
    }
}