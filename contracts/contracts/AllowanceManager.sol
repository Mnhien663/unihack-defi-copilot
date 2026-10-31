// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

contract AllowanceManager {
    struct Budget {
        uint256 remaining;
        uint256 expiry;
    }

    mapping(address => mapping(address => Budget)) private budgets;

    error InvalidToken();
    error InvalidAmount();
    error InvalidExpiry();
    error BudgetExpired();
    error BudgetExceeded(uint256 available, uint256 requested);

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

    function setBudget(
        address token,
        uint256 amount,
        uint256 expiry
    ) external {
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

    function revoke(address token) external {
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