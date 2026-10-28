package avalanche

import (
	"fmt"
	"math/big"
	"strings"
)

// ParseRawAmount parses a non-negative base-10 integer within uint256.
// Zero is valid for balances and allowances.
func ParseRawAmount(value string) (*big.Int, error) {
	if value == "" {
		return nil, fmt.Errorf("raw amount must not be empty")
	}

	for i := 0; i < len(value); i++ {
		if value[i] < '0' || value[i] > '9' {
			return nil, fmt.Errorf(
				"raw amount must contain only decimal digits",
			)
		}
	}

	amount, ok := new(big.Int).SetString(value, 10)
	if !ok {
		return nil, fmt.Errorf("cannot parse raw amount")
	}

	if amount.BitLen() > 256 {
		return nil, fmt.Errorf("raw amount exceeds uint256")
	}

	return amount, nil
}

// ParseTokenAmount converts a decimal amount to the token's smallest units.
// It rejects fractional digits beyond the token's decimals.
func ParseTokenAmount(value string, decimals uint8) (*big.Int, error) {
	if value == "" {
		return nil, fmt.Errorf("token amount must not be empty")
	}

	parts := strings.Split(value, ".")
	if len(parts) > 2 {
		return nil, fmt.Errorf("token amount must contain at most one decimal point")
	}

	whole := parts[0]
	if whole == "" {
		return nil, fmt.Errorf("token amount must have an integer part")
	}

	fraction := ""
	if len(parts) == 2 {
		fraction = parts[1]
		if fraction == "" {
			return nil, fmt.Errorf("token amount must have digits after the decimal point")
		}
	}

	for _, part := range []string{whole, fraction} {
		for i := 0; i < len(part); i++ {
			if part[i] < '0' || part[i] > '9' {
				return nil, fmt.Errorf(
					"token amount must contain only decimal digits and an optional decimal point",
				)
			}
		}
	}

	if len(fraction) > int(decimals) {
		return nil, fmt.Errorf(
			"token amount exceeds %d decimal places",
			decimals,
		)
	}

	raw := whole + fraction +
		strings.Repeat("0", int(decimals)-len(fraction))

	amount, err := ParseRawAmount(raw)
	if err != nil {
		return nil, fmt.Errorf("parse token amount: %w", err)
	}

	return amount, nil
}
