package tools

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"unihack-backend/internal/avalanche"
)

const (
	toolAllowanceWallet  = "0x0000000000000000000000000000000000000001"
	toolAllowanceToken   = "0x0000000000000000000000000000000000000010"
	toolAllowanceSpender = "0x0000000000000000000000000000000000000002"
)

type allowanceReaderStub struct {
	calls   int
	token   string
	owner   string
	spender string
	result  avalanche.AllowanceResult
	err     error
}

func (s *allowanceReaderStub) ReadAllowance(
	ctx context.Context,
	token string,
	owner string,
	spender string,
) (avalanche.AllowanceResult, error) {
	s.calls++
	s.token = token
	s.owner = owner
	s.spender = spender
	return s.result, s.err
}

func allowanceToolArguments(t *testing.T, overrides map[string]any) json.RawMessage {
	t.Helper()

	args := map[string]any{
		"wallet_address":  toolAllowanceWallet,
		"chain_id":        43113,
		"token_address":   toolAllowanceToken,
		"spender_address": toolAllowanceSpender,
	}
	for key, value := range overrides {
		args[key] = value
	}

	encoded, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func TestGetAllowanceToolExecutes(t *testing.T) {
	block := uint64(12345)
	client := &allowanceReaderStub{
		result: avalanche.AllowanceResult{
			OwnerAddress:   toolAllowanceWallet,
			SpenderAddress: toolAllowanceSpender,
			Allowance: avalanche.TokenAmount{
				Asset: avalanche.Asset{
					ChainID:  43113,
					Address:  toolAllowanceToken,
					Decimals: 6,
				},
				Raw:       "1234567",
				Formatted: "1.234567",
			},
			Provenance: avalanche.Provenance{
				Source:      "mock_chain",
				ChainID:     43113,
				BlockNumber: &block,
				ObservedAt:  time.Now().UTC(),
				IsDemo:      true,
			},
		},
	}

	tool, err := NewGetAllowanceTool(client)
	if err != nil {
		t.Fatal(err)
	}

	result, err := tool.Execute(
		context.Background(),
		allowanceToolArguments(t, nil),
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.Error != nil {
		t.Fatalf("unexpected tool error: %+v", result.Error)
	}

	if client.calls != 1 ||
		client.token != toolAllowanceToken ||
		client.owner != toolAllowanceWallet ||
		client.spender != toolAllowanceSpender {
		t.Fatalf("unexpected reader arguments: %+v", client)
	}

	var payload map[string]json.RawMessage
	if err := json.Unmarshal(result.Data, &payload); err != nil {
		t.Fatal(err)
	}
	if _, exists := payload["balance"]; exists {
		t.Fatal("allowance result must not be labelled balance")
	}

	var decoded avalanche.AllowanceResult
	if err := json.Unmarshal(result.Data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Allowance.Raw != "1234567" ||
		decoded.Allowance.Formatted != "1.234567" {
		t.Fatalf("unexpected allowance: %+v", decoded.Allowance)
	}

	if result.Source != "mock_chain" ||
		result.ChainID != 43113 ||
		result.BlockNumber == nil ||
		*result.BlockNumber != block ||
		result.ObservedAt.IsZero() {
		t.Fatalf("missing provenance: %+v", result)
	}
}

func TestGetAllowanceToolRejectsInvalidArgumentsBeforeRead(t *testing.T) {
	tests := []struct {
		name      string
		overrides map[string]any
		code      string
	}{
		{"wallet", map[string]any{"wallet_address": "bad"}, "INVALID_WALLET_ADDRESS"},
		{"token", map[string]any{"token_address": "bad"}, "INVALID_TOKEN_ADDRESS"},
		{"spender", map[string]any{"spender_address": "bad"}, "INVALID_SPENDER_ADDRESS"},
		{"chain", map[string]any{"chain_id": 1}, "UNSUPPORTED_CHAIN_ID"},
		{"unknown field", map[string]any{"owner_address": toolAllowanceWallet}, "INVALID_TOOL_ARGUMENTS"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &allowanceReaderStub{}
			tool, err := NewGetAllowanceTool(client)
			if err != nil {
				t.Fatal(err)
			}

			result, err := tool.Execute(
				context.Background(),
				allowanceToolArguments(t, tt.overrides),
			)
			if err != nil {
				t.Fatal(err)
			}
			if result.Error == nil || result.Error.Code != tt.code {
				t.Fatalf("want %s, got %+v", tt.code, result.Error)
			}
			if client.calls != 0 {
				t.Fatalf("reader calls = %d; want 0", client.calls)
			}
		})
	}
}

func TestGetAllowanceToolMapsReaderErrors(t *testing.T) {
	tests := []struct {
		name string
		err  error
		code string
	}{
		{"unsupported token", avalanche.ErrAllowanceUnsupportedToken, "UNSUPPORTED_TOKEN"},
		{"wrong chain", avalanche.ErrWrongChain, "UNSUPPORTED_CHAIN_ID"},
		{"timeout", context.DeadlineExceeded, "UPSTREAM_TIMEOUT"},
		{"metadata", avalanche.ErrTokenMetadataUnavailable, "TOKEN_METADATA_UNAVAILABLE"},
		{"upstream", errors.New("upstream unavailable"), "UPSTREAM_ERROR"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &allowanceReaderStub{err: tt.err}
			tool, err := NewGetAllowanceTool(client)
			if err != nil {
				t.Fatal(err)
			}

			result, err := tool.Execute(
				context.Background(),
				allowanceToolArguments(t, nil),
			)
			if err != nil {
				t.Fatal(err)
			}
			if result.Error == nil || result.Error.Code != tt.code {
				t.Fatalf("want %s, got %+v", tt.code, result.Error)
			}
			if client.calls != 1 {
				t.Fatalf("reader calls = %d; want 1", client.calls)
			}
		})
	}
}

func TestGetAllowanceToolHonorsCancellation(t *testing.T) {
	client := &allowanceReaderStub{}
	tool, err := NewGetAllowanceTool(client)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = tool.Execute(ctx, allowanceToolArguments(t, nil))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
	if client.calls != 0 {
		t.Fatal("cancelled request reached reader")
	}
}

func TestNewGetAllowanceToolRejectsNilReader(t *testing.T) {
	if _, err := NewGetAllowanceTool(nil); err == nil {
		t.Fatal("expected error for nil reader")
	}
}
