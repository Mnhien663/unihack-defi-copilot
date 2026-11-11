package avalanche

import "testing"

func TestParseRawAmount(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{
			name:  "normal amount",
			input: "1250000",
			want:  "1250000",
		},
		{
			name:  "zero",
			input: "0",
			want:  "0",
		},
		{
			name:  "leading zeros",
			input: "00012",
			want:  "12",
		},
		{
			name:  "large amount remains exact",
			input: "9007199254740993",
			want:  "9007199254740993",
		},
		{
			name:  "maximum uint256",
			input: "115792089237316195423570985008687907853269984665640564039457584007913129639935",
			want:  "115792089237316195423570985008687907853269984665640564039457584007913129639935",
		},
		{
			name:    "uint256 overflow",
			input:   "115792089237316195423570985008687907853269984665640564039457584007913129639936",
			wantErr: true,
		},
		{
			name:    "empty",
			input:   "",
			wantErr: true,
		},
		{
			name:    "negative",
			input:   "-1",
			wantErr: true,
		},
		{
			name:    "explicit plus sign",
			input:   "+1",
			wantErr: true,
		},
		{
			name:    "fraction",
			input:   "1.25",
			wantErr: true,
		},
		{
			name:    "scientific notation",
			input:   "1e18",
			wantErr: true,
		},
		{
			name:    "hexadecimal",
			input:   "0xff",
			wantErr: true,
		},
		{
			name:    "whitespace",
			input:   " 12 ",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseRawAmount(tt.input)

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected an error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if got == nil {
				t.Fatal("expected an amount, got nil")
			}

			if got.String() != tt.want {
				t.Errorf(
					"amount = %s, want %s",
					got.String(),
					tt.want,
				)
			}
		})
	}
}

func TestParseTokenAmount(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		decimals uint8
		want     string
		wantErr  bool
	}{
		{
			name:     "six decimals",
			input:    "1.25",
			decimals: 6,
			want:     "1250000",
		},
		{
			name:     "smallest unit with six decimals",
			input:    "0.000001",
			decimals: 6,
			want:     "1",
		},
		{
			name:     "eighteen decimals",
			input:    "1",
			decimals: 18,
			want:     "1000000000000000000",
		},
		{
			name:     "smallest unit with eighteen decimals",
			input:    "0.000000000000000001",
			decimals: 18,
			want:     "1",
		},
		{
			name:     "zero",
			input:    "0",
			decimals: 18,
			want:     "0",
		},
		{
			name:     "token without fractional units",
			input:    "12",
			decimals: 0,
			want:     "12",
		},
		{
			name:     "large amount remains exact",
			input:    "9007199254740993.123456",
			decimals: 6,
			want:     "9007199254740993123456",
		},
		{
			name:     "too many fractional digits",
			input:    "0.0000001",
			decimals: 6,
			wantErr:  true,
		},
		{
			name:     "excess trailing zero",
			input:    "1.0000000",
			decimals: 6,
			wantErr:  true,
		},
		{
			name:     "fraction for zero decimals",
			input:    "1.0",
			decimals: 0,
			wantErr:  true,
		},
		{
			name:     "negative",
			input:    "-1",
			decimals: 18,
			wantErr:  true,
		},
		{
			name:     "empty",
			input:    "",
			decimals: 18,
			wantErr:  true,
		},
		{
			name:     "missing integer part",
			input:    ".5",
			decimals: 18,
			wantErr:  true,
		},
		{
			name:     "missing fractional part",
			input:    "1.",
			decimals: 18,
			wantErr:  true,
		},
		{
			name:     "multiple decimal points",
			input:    "1.2.3",
			decimals: 18,
			wantErr:  true,
		},
		{
			name:     "scientific notation",
			input:    "1e18",
			decimals: 18,
			wantErr:  true,
		},
		{
			name:     "whitespace",
			input:    " 1 ",
			decimals: 18,
			wantErr:  true,
		},
		{
			name:     "maximum uint256",
			input:    "115792089237316195423570985008687907853269984665640564039457584007913129639935",
			decimals: 0,
			want:     "115792089237316195423570985008687907853269984665640564039457584007913129639935",
		},
		{
			name:     "overflow after scaling",
			input:    "115792089237316195423570985008687907853269984665640564039457584007913129639935",
			decimals: 1,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseTokenAmount(tt.input, tt.decimals)

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected an error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if got == nil {
				t.Fatal("expected an amount, got nil")
			}

			if got.String() != tt.want {
				t.Errorf(
					"amount = %s, want %s",
					got.String(),
					tt.want,
				)
			}
		})
	}
}
