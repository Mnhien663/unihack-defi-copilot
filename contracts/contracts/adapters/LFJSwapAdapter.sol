// SPDX-License-Identifier: MIT
pragma solidity ^0.8.28;

import {IERC20} from "@openzeppelin/contracts/token/ERC20/IERC20.sol";
import {SafeERC20} from "@openzeppelin/contracts/token/ERC20/utils/SafeERC20.sol";
import {ReentrancyGuard} from "@openzeppelin/contracts/utils/ReentrancyGuard.sol";

import {ISwapAdapter} from "../interfaces/ISwapAdapter.sol";
import {ILFJRouter} from "../interfaces/ILFJRouter.sol";

contract LFJSwapAdapter is ISwapAdapter, ReentrancyGuard {
    using SafeERC20 for IERC20;

    address public immutable manager;
    ILFJRouter public immutable router;
    address public immutable tokenX;
    address public immutable tokenY;

    uint256 public constant BIN_STEP = 1;

    struct SwapState {
        uint256 adapterInputBefore;
        uint256 managerInputBefore;
        uint256 recipientOutputBefore;
    }

    error InvalidContract(address account);
    error InvalidPair();
    error UnauthorizedManager();
    error UnsupportedPair();
    error InvalidRecipient();
    error InvalidAmount();
    error InvalidMinimumOutput();
    error DeadlineExpired();
    error UnexpectedInputBalance();
    error InsufficientOutput(uint256 received, uint256 minimum);
    error OutputAmountMismatch(uint256 reported, uint256 received);
    error RouterAllowanceNotCleared();

    constructor(
        address manager_,
        address router_,
        address tokenX_,
        address tokenY_
    ) {
        address[4] memory accounts = [
            manager_, router_, tokenX_, tokenY_
        ];

        for (uint256 i; i < accounts.length; ++i) {
            if (
                accounts[i] == address(0) ||
                accounts[i].code.length == 0
            ) {
                revert InvalidContract(accounts[i]);
            }
        }

        if (
            manager_ == router_ ||
            tokenX_ == tokenY_ ||
            tokenX_ == manager_ ||
            tokenY_ == manager_ ||
            tokenX_ == router_ ||
            tokenY_ == router_
        ) {
            revert InvalidPair();
        }

        manager = manager_;
        router = ILFJRouter(router_);
        tokenX = tokenX_;
        tokenY = tokenY_;
    }

    function swap(
        address tokenIn,
        address tokenOut,
        uint256 amountIn,
        uint256 minAmountOut,
        address recipient,
        uint256 deadline
    ) external override nonReentrant returns (uint256 amountOut) {
        if (msg.sender != manager) {
            revert UnauthorizedManager();
        }

        if (
            !(
                (tokenIn == tokenX && tokenOut == tokenY) ||
                (tokenIn == tokenY && tokenOut == tokenX)
            )
        ) {
            revert UnsupportedPair();
        }

        if (
            recipient == address(0) ||
            recipient == address(this) ||
            recipient == manager ||
            recipient == address(router) ||
            recipient == tokenX ||
            recipient == tokenY
        ) {
            revert InvalidRecipient();
        }

        if (amountIn == 0 || amountIn > type(uint128).max) {
            revert InvalidAmount();
        }
        if (minAmountOut == 0) {
            revert InvalidMinimumOutput();
        }
        if (block.timestamp > deadline) {
            revert DeadlineExpired();
        }

        IERC20 input = IERC20(tokenIn);
        IERC20 output = IERC20(tokenOut);

        SwapState memory state = SwapState({
            adapterInputBefore: input.balanceOf(address(this)),
            managerInputBefore: input.balanceOf(manager),
            recipientOutputBefore: output.balanceOf(recipient)
        });

        input.safeTransferFrom(manager, address(this), amountIn);

        if (
            input.balanceOf(address(this))
                != state.adapterInputBefore + amountIn ||
            input.balanceOf(manager) + amountIn
                != state.managerInputBefore
        ) {
            revert UnexpectedInputBalance();
        }

        ILFJRouter.Path memory path;
        path.pairBinSteps = new uint256[](1);
        path.pairBinSteps[0] = BIN_STEP;

        path.versions = new ILFJRouter.Version[](1);
        path.versions[0] = ILFJRouter.Version.V2_2;

        path.tokenPath = new IERC20[](2);
        path.tokenPath[0] = input;
        path.tokenPath[1] = output;

        input.forceApprove(address(router), amountIn);

        uint256 reported = router.swapExactTokensForTokens(
            amountIn,
            minAmountOut,
            path,
            recipient,
            deadline
        );

        input.forceApprove(address(router), 0);

        if (input.allowance(address(this), address(router)) != 0) {
            revert RouterAllowanceNotCleared();
        }
        if (
            input.balanceOf(address(this)) != state.adapterInputBefore ||
            input.balanceOf(manager) + amountIn
                != state.managerInputBefore
        ) {
            revert UnexpectedInputBalance();
        }

        uint256 outputAfter = output.balanceOf(recipient);
        if (outputAfter < state.recipientOutputBefore) {
            revert OutputAmountMismatch(reported, 0);
        }

        amountOut = outputAfter - state.recipientOutputBefore;

        if (amountOut < minAmountOut) {
            revert InsufficientOutput(amountOut, minAmountOut);
        }
        if (reported != amountOut) {
            revert OutputAmountMismatch(reported, amountOut);
        }
    }
}