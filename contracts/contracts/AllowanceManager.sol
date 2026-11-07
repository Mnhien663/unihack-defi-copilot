// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import {IERC20} from "@openzeppelin/contracts/token/ERC20/IERC20.sol";
import {SafeERC20} from "@openzeppelin/contracts/token/ERC20/utils/SafeERC20.sol";
import {ReentrancyGuard} from "@openzeppelin/contracts/utils/ReentrancyGuard.sol";
import {ISwapAdapter} from "./interfaces/ISwapAdapter.sol";
import {IVaultAdapter} from "./interfaces/IVaultAdapter.sol";

contract AllowanceManager is ReentrancyGuard {
    using SafeERC20 for IERC20;
    struct Budget {
        uint256 remaining;
        uint256 expiry;
    }

    mapping(address => mapping(address => Budget)) private budgets;

    address public immutable policyAdmin;

    mapping(address => bool) public supportedTokens;

    mapping(address => mapping(address => address)) public swapAdapters;

    struct VaultConfig {
        address adapter;
        address shareToken;
    }

    mapping(address => VaultConfig) public vaultConfigs;

    struct ExecutionState {
        address adapter;
        uint256 managerInputBefore;
        uint256 ownerInputBefore;
        uint256 ownerOutputBefore;
    }

    error InvalidToken();
    error InvalidAmount();
    error InvalidExpiry();
    error BudgetExpired();
    error BudgetExceeded(uint256 available, uint256 requested);
    error UnauthorizedPolicyAdmin();
    error InvalidContract(address account);
    error InvalidPair();
    error UnsupportedToken(address token);
    error RouteNotConfigured();
    error DeadlineExpired();
    error InvalidMinimumOutput();
    error UnexpectedInputBalance();
    error InsufficientOutput(uint256 received, uint256 minimum);
    error OutputAmountMismatch(uint256 reported, uint256 received);
    error AdapterAllowanceNotCleared();

    event BudgetSet(
        address indexed owner,
        address indexed token,
        uint256 amount,
        uint256 expiry
    );

    event BudgetRevoked(
        address indexed owner,
        address indexed token
    );

    event TokenSupportUpdated(
    address indexed token,
    bool supported
    );

    event SwapAdapterUpdated(
        address indexed tokenIn,
        address indexed tokenOut,
        address adapter
    );

    event VaultAdapterUpdated(
        address indexed token,
        address adapter,
        address shareToken
    );

    event SwapExecuted(
        address indexed owner,
        address indexed tokenIn,
        address indexed tokenOut,
        address adapter,
        uint256 amountIn,
        uint256 amountOut
    );

    event DepositExecuted(
        address indexed owner,
        address indexed token,
        address indexed shareToken,
        address adapter,
        uint256 amount,
        uint256 sharesOut
    );

    constructor() {
    policyAdmin = msg.sender;
    }

    modifier onlyPolicyAdmin() {
        if (msg.sender != policyAdmin) {
            revert UnauthorizedPolicyAdmin();
        }

        _;
    }

    function setTokenSupported(
        address token,
        bool supported
    ) external onlyPolicyAdmin nonReentrant {
        if (token == address(0)) {
            revert InvalidToken();
        }

        if (supported && token.code.length == 0) {
            revert InvalidContract(token);
        }

        supportedTokens[token] = supported;

        emit TokenSupportUpdated(token, supported);
    }

    function setSwapAdapter(
        address tokenIn,
        address tokenOut,
        address adapter
    ) external onlyPolicyAdmin nonReentrant {
        if (tokenIn == address(0) || tokenOut == address(0)) {
            revert InvalidToken();
        }

        if (tokenIn == tokenOut) {
            revert InvalidPair();
        }

        if (adapter != address(0) && adapter.code.length == 0) {
            revert InvalidContract(adapter);
        }

        swapAdapters[tokenIn][tokenOut] = adapter;

        emit SwapAdapterUpdated(tokenIn, tokenOut, adapter);
    }

    function setVaultAdapter(
        address token,
        address adapter,
        address shareToken
    ) external onlyPolicyAdmin nonReentrant {
        if (token == address(0)) {
            revert InvalidToken();
        }

        if (adapter == address(0)) {
            delete vaultConfigs[token];

            emit VaultAdapterUpdated(
                token,
                address(0),
                address(0)
            );

            return;
        }

        if (adapter.code.length == 0) {
            revert InvalidContract(adapter);
        }

        if (shareToken == address(0) || shareToken == token) {
            revert InvalidPair();
        }

        if (shareToken.code.length == 0) {
            revert InvalidContract(shareToken);
        }

        vaultConfigs[token] = VaultConfig({
            adapter: adapter,
            shareToken: shareToken
        });

        emit VaultAdapterUpdated(token, adapter, shareToken);
    }

    function setBudget(
        address token,
        uint256 amount,
        uint256 expiry
    ) external nonReentrant {
        if (token == address(0)) {
            revert InvalidToken();
        }

        if (amount == 0) {
            revert InvalidAmount();
        }

        if (expiry <= block.timestamp) {
            revert InvalidExpiry();
        }

        budgets[msg.sender][token] = Budget({
            remaining: amount,
            expiry: expiry
        });

        emit BudgetSet(msg.sender, token, amount, expiry);
    }

    function revoke(address token) external nonReentrant {
        if (token == address(0)) {
            revert InvalidToken();
        }

        delete budgets[msg.sender][token];

        emit BudgetRevoked(msg.sender, token);
    }

    function remaining(
        address owner,
        address token
    ) external view returns (uint256) {
        Budget storage budget = budgets[owner][token];

        if (block.timestamp >= budget.expiry) {
            return 0;
        }

        return budget.remaining;
    }

    function executeSwap(
        address tokenIn,
        address tokenOut,
        uint256 amountIn,
        uint256 minAmountOut,
        uint256 deadline
    ) external nonReentrant returns (uint256 amountOut) {
        if (tokenIn == tokenOut) {
            revert InvalidPair();
        }

        if (!supportedTokens[tokenIn]) {
            revert UnsupportedToken(tokenIn);
        }

        if (!supportedTokens[tokenOut]) {
            revert UnsupportedToken(tokenOut);
        }

        if (block.timestamp > deadline) {
            revert DeadlineExpired();
        }

        if (minAmountOut == 0) {
            revert InvalidMinimumOutput();
        }

        ExecutionState memory state;

        state.adapter = swapAdapters[tokenIn][tokenOut];

        if (state.adapter == address(0)) {
            revert RouteNotConfigured();
        }

        if (state.adapter.code.length == 0) {
            revert InvalidContract(state.adapter);
        }

        _consumeBudget(msg.sender, tokenIn, amountIn);

        state.managerInputBefore = IERC20(tokenIn).balanceOf(address(this));
        state.ownerInputBefore = IERC20(tokenIn).balanceOf(msg.sender);
        state.ownerOutputBefore = IERC20(tokenOut).balanceOf(msg.sender);

        IERC20(tokenIn).safeTransferFrom(
            msg.sender,
            address(this),
            amountIn
        );

        if (
            IERC20(tokenIn).balanceOf(address(this))
                != state.managerInputBefore + amountIn
        ) {
            revert UnexpectedInputBalance();
        }

        if (
            IERC20(tokenIn).balanceOf(msg.sender) + amountIn
                != state.ownerInputBefore
        ) {
            revert UnexpectedInputBalance();
        }

        IERC20(tokenIn).forceApprove(state.adapter, amountIn);

        uint256 reportedAmount = ISwapAdapter(state.adapter).swap(
            tokenIn,
            tokenOut,
            amountIn,
            minAmountOut,
            msg.sender,
            deadline
        );

        IERC20(tokenIn).forceApprove(state.adapter, 0);

        if (
            IERC20(tokenIn).allowance(address(this), state.adapter) != 0
        ) {
            revert AdapterAllowanceNotCleared();
        }

        if (
            IERC20(tokenIn).balanceOf(address(this))
                != state.managerInputBefore
        ) {
            revert UnexpectedInputBalance();
        }

        if (
            IERC20(tokenIn).balanceOf(msg.sender) + amountIn
                != state.ownerInputBefore
        ) {
            revert UnexpectedInputBalance();
        }

        uint256 outputAfter = IERC20(tokenOut).balanceOf(msg.sender);

        if (outputAfter < state.ownerOutputBefore) {
            revert InsufficientOutput(0, minAmountOut);
        }

        amountOut = outputAfter - state.ownerOutputBefore;

        if (amountOut < minAmountOut) {
            revert InsufficientOutput(amountOut, minAmountOut);
        }

        if (reportedAmount != amountOut) {
            revert OutputAmountMismatch(reportedAmount, amountOut);
        }

        emit SwapExecuted(
            msg.sender,
            tokenIn,
            tokenOut,
            state.adapter,
            amountIn,
            amountOut
        );
    }

    function executeDeposit(
        address token,
        uint256 amount,
        uint256 minSharesOut,
        uint256 deadline
    ) external nonReentrant returns (uint256 sharesOut) {
        if (!supportedTokens[token]) {
            revert UnsupportedToken(token);
        }

        if (block.timestamp > deadline) {
            revert DeadlineExpired();
        }

        if (minSharesOut == 0) {
            revert InvalidMinimumOutput();
        }

        VaultConfig memory config = vaultConfigs[token];

        if (config.adapter == address(0)) {
            revert RouteNotConfigured();
        }

        if (config.adapter.code.length == 0) {
            revert InvalidContract(config.adapter);
        }

        if (!supportedTokens[config.shareToken]) {
            revert UnsupportedToken(config.shareToken);
        }

        ExecutionState memory state;

        state.adapter = config.adapter;

        _consumeBudget(msg.sender, token, amount);

        state.managerInputBefore = IERC20(token).balanceOf(address(this));
        state.ownerInputBefore = IERC20(token).balanceOf(msg.sender);
        state.ownerOutputBefore = IERC20(config.shareToken).balanceOf(
            msg.sender
        );

        IERC20(token).safeTransferFrom(
            msg.sender,
            address(this),
            amount
        );

        if (
            IERC20(token).balanceOf(address(this))
                != state.managerInputBefore + amount
        ) {
            revert UnexpectedInputBalance();
        }

        if (
            IERC20(token).balanceOf(msg.sender) + amount
                != state.ownerInputBefore
        ) {
            revert UnexpectedInputBalance();
        }

        IERC20(token).forceApprove(state.adapter, amount);

        uint256 reportedShares = IVaultAdapter(state.adapter).deposit(
            token,
            amount,
            minSharesOut,
            msg.sender,
            deadline
        );

        IERC20(token).forceApprove(state.adapter, 0);

        if (
            IERC20(token).allowance(address(this), state.adapter) != 0
        ) {
            revert AdapterAllowanceNotCleared();
        }

        if (
            IERC20(token).balanceOf(address(this))
                != state.managerInputBefore
        ) {
            revert UnexpectedInputBalance();
        }

        if (
            IERC20(token).balanceOf(msg.sender) + amount
                != state.ownerInputBefore
        ) {
            revert UnexpectedInputBalance();
        }

        uint256 sharesAfter = IERC20(config.shareToken).balanceOf(
            msg.sender
        );

        if (sharesAfter < state.ownerOutputBefore) {
            revert InsufficientOutput(0, minSharesOut);
        }

        sharesOut = sharesAfter - state.ownerOutputBefore;

        if (sharesOut < minSharesOut) {
            revert InsufficientOutput(sharesOut, minSharesOut);
        }

        if (reportedShares != sharesOut) {
            revert OutputAmountMismatch(reportedShares, sharesOut);
        }

        emit DepositExecuted(
            msg.sender,
            token,
            config.shareToken,
            state.adapter,
            amount,
            sharesOut
        );
    }

    function _consumeBudget(
        address owner,
        address token,
        uint256 amount
    ) internal {
        if (token == address(0)) {
            revert InvalidToken();
        }

        if (amount == 0) {
            revert InvalidAmount();
        }

        Budget storage budget = budgets[owner][token];

        if (block.timestamp >= budget.expiry) {
            revert BudgetExpired();
        }

        if (amount > budget.remaining) {
            revert BudgetExceeded(budget.remaining, amount);
        }

        budget.remaining -= amount;
    }
}