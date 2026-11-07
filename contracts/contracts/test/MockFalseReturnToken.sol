// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import {ERC20} from "@openzeppelin/contracts/token/ERC20/ERC20.sol";

contract MockFalseReturnToken is ERC20 {
    constructor() ERC20("False Return Token", "FALSE") {}

    function mint(address recipient, uint256 amount) external {
        _mint(recipient, amount);
    }

    function transferFrom(
        address,
        address,
        uint256
    ) public pure override returns (bool) {
        return false;
    }
}