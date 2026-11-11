package avalanche

import (
	"errors"
	"testing"
)

func TestFormatTokenAmount(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		raw      string
		decimals uint8
		expected string
	}{
		{
			name:     "AVAX with 18 decimals",
			raw:      "2181886544100000121",
			decimals: 18,
			expected: "2.181886544100000121",
		},
		{
			name:     "USDC with 6 decimals",
			raw:      "1234567",
			decimals: 6,
			expected: "1.234567",
		},
		{
			name:     "amount below one token",
			raw:      "42",
			decimals: 6,
			expected: "0.000042",
		},
		{
			name:     "trailing fractional zeros",
			raw:      "2500000000000000000",
			decimals: 18,
			expected: "2.5",
		},
		{
			name:     "whole token",
			raw:      "2000000",
			decimals: 6,
			expected: "2",
		},
		{
			name:     "zero",
			raw:      "0",
			decimals: 18,
			expected: "0",
		},
		{
			name:     "no decimals",
			raw:      "123456789",
			decimals: 0,
			expected: "123456789",
		},
		{
			name:     "leading zeros",
			raw:      "000123",
			decimals: 2,
			expected: "1.23",
		},
	}

	for _, testCase := range testCases {
		testCase := testCase

		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			actual, err := FormatTokenAmount(
				testCase.raw,
				testCase.decimals,
			)
			if err != nil {
				t.Fatalf("format token amount: %v", err)
			}

			if actual != testCase.expected {
				t.Fatalf(
					"unexpected formatted amount: got %q, want %q",
					actual,
					testCase.expected,
				)
			}
		})
	}
}

func TestFormatTokenAmountRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	testCases := []string{
		"",
		"abc",
		"-1",
		"1.5",
	}

	for _, raw := range testCases {
		raw := raw

		t.Run(raw, func(t *testing.T) {
			t.Parallel()

			_, err := FormatTokenAmount(raw, 18)

			if !errors.Is(err, ErrInvalidTokenAmount) {
				t.Fatalf(
					"expected ErrInvalidTokenAmount, got %v",
					err,
				)
			}
		})
	}
}
