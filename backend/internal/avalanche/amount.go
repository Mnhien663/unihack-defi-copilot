package avalanche

import (
	"errors"
	"math/big"
	"strings"
)

var ErrInvalidTokenAmount = errors.New("invalid token amount")

// FormatTokenAmount converts an integer amount in the token's smallest
// units into a decimal string without using floating-point arithmetic.
func FormatTokenAmount(
	raw string,
	decimals uint8,
) (string, error) {
	raw = strings.TrimSpace(raw)

	amount, ok := new(big.Int).SetString(raw, 10)
	if !ok || amount.Sign() < 0 {
		return "", ErrInvalidTokenAmount
	}

	digits := amount.String()

	if decimals == 0 {
		return digits, nil
	}

	decimalCount := int(decimals)

	if len(digits) <= decimalCount {
		digits = strings.Repeat(
			"0",
			decimalCount-len(digits)+1,
		) + digits
	}

	splitIndex := len(digits) - decimalCount
	wholePart := digits[:splitIndex]
	fractionalPart := strings.TrimRight(
		digits[splitIndex:],
		"0",
	)

	if fractionalPart == "" {
		return wholePart, nil
	}

	return wholePart + "." + fractionalPart, nil
}
