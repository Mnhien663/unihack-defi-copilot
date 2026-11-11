// SPDX-License-Identifier: MIT
pragma solidity ^0.8.28;

import {IERC20} from "@openzeppelin/contracts/token/ERC20/IERC20.sol";
import {SafeERC20} from "@openzeppelin/contracts/token/ERC20/utils/SafeERC20.sol";

import {ILFJRouter} from "../interfaces/ILFJRouter.sol";

/// @dev Local test double only. Not a price source or production router.
contract MockLFJRouter is ILFJRouter {
    using SafeERC20 for IERC20;

    enum Mode {
        Normal,
        RevertSwap,
        WrongReportedOutput,
        PartialInput
    }

    Mode public mode;
    uint256 public outputAmount;

    error SwapBlocked();
    error InvalidPath();
    error Expired();

    function configure(uint256 outputAmount_, Mode mode_) external {
        outputAmount = outputAmount_;
        mode = mode_;
    }

    function swapExactTokensForTokens(
        uint256 amountIn,
        uint256,
        Path memory path,
        address to,
        uint256 deadline
    ) external override returns (uint256 amountOut) {
        if (mode == Mode.RevertSwap) {
            revert SwapBlocked();
        }
        if (block.timestamp > deadline) {
            revert Expired();
        }

        // Assert the adapter constructs the expected direct V2.2 path.
        if (
            path.pairBinSteps.length != 1 ||
            path.pairBinSteps[0] != 1 ||
            path.versions.length != 1 ||
            path.versions[0] != Version.V2_2 ||
            path.tokenPath.length != 2 ||
            address(path.tokenPath[0]) == address(path.tokenPath[1])
        ) {
            revert InvalidPath();
        }

        uint256 consumed = amountIn;
        if (mode == Mode.PartialInput) {
            consumed = amountIn - 1;
        }

        path.tokenPath[0].safeTransferFrom(
            msg.sender,
            address(this),
            consumed
        );

        path.tokenPath[1].safeTransfer(to, outputAmount);

        amountOut = outputAmount;
        if (mode == Mode.WrongReportedOutput) {
            amountOut += 1;
        }
    }
}