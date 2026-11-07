// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import {ERC20} from "@openzeppelin/contracts/token/ERC20/ERC20.sol";

contract MockFeeToken is ERC20 {
    constructor() ERC20("Fee Token", "FEE") {}

    function mint(address recipient, uint256 amount) external {
        _mint(recipient, amount);
    }

    function _update(
        address from,
        address to,
        uint256 value
    ) internal override {
        // Mint and burn do not charge a fee.
        if (from == address(0) || to == address(0)) {
            super._update(from, to, value);
            return;
        }

        uint256 fee = value / 100;

        super._update(from, to, value - fee);

        if (fee > 0) {
            super._update(from, address(0), fee);
        }
    }
}