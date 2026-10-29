package tools

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"unihack-backend/internal/avalanche"
)

type mockChainClient struct {
	result         avalanche.NativeBalanceResult
	err            error
	receivedWallet string
	callCount      int
}

func (m *mockChainClient) ReadNativeBalance(
	ctx context.Context,
	walletAddress string,
) (avalanche.NativeBalanceResult, error) {
	m.callCount++
	m.receivedWallet = walletAddress

	return m.result, m.err
}

func newMockNativeBalanceResult() avalanche.NativeBalanceResult {
	blockNumber := uint64(60034644)

	return avalanche.NativeBalanceResult{
		WalletAddress: "0x0000000000000000000000000000000000000001",
		Balance: avalanche.TokenAmount{
			Asset: avalanche.Asset{
				ChainID:  uint64(avalanche.FujiChainID),
				Symbol:   "AVAX",
				Decimals: 18,
			},
			Raw: "2181886544100000121",
		},
		Provenance: avalanche.Provenance{
			Source:      "mock_chain",
			ChainID:     uint64(avalanche.FujiChainID),
			BlockNumber: &blockNumber,
			ObservedAt: time.Date(
				2026,
				time.October,
				28,
				0,
				0,
				0,
				0,
				time.UTC,
			),
			IsDemo: true,
		},
	}
}

func TestNativeBalanceToolDefinition(t *testing.T) {
	client := &mockChainClient{}
	tool, err := NewNativeBalanceTool(client)
	if err != nil {
		t.Fatalf("NewNativeBalanceTool() returned error: %v", err)
	}

	definition := tool.Definition()

	if definition.Name != NativeBalanceToolName {
		t.Fatalf(
			"tool name = %q, want %q",
			definition.Name,
			NativeBalanceToolName,
		)
	}

	if !json.Valid(definition.InputSchema) {
		t.Fatal("tool input schema is not valid JSON")
	}
}

func TestNativeBalanceToolExecute(t *testing.T) {
	expected := newMockNativeBalanceResult()

	client := &mockChainClient{
		result: expected,
	}

	tool, err := NewNativeBalanceTool(client)
	if err != nil {
		t.Fatalf("NewNativeBalanceTool() returned error: %v", err)
	}

	arguments := json.RawMessage(`{
		"wallet_address":
		"0x0000000000000000000000000000000000000001",
		"chain_id": 43113
	}`)

	result, err := tool.Execute(
		context.Background(),
		arguments,
	)
	if err != nil {
		t.Fatalf("Execute() returned error: %v", err)
	}

	if result.Error != nil {
		t.Fatalf(
			"Execute() returned tool error: %+v",
			result.Error,
		)
	}

	if client.callCount != 1 {
		t.Fatalf(
			"chain client call count = %d, want 1",
			client.callCount,
		)
	}

	if client.receivedWallet != expected.WalletAddress {
		t.Fatalf(
			"received wallet = %q, want %q",
			client.receivedWallet,
			expected.WalletAddress,
		)
	}

	if result.Source != "mock_chain" {
		t.Fatalf(
			"result source = %q, want mock_chain",
			result.Source,
		)
	}

	var decoded avalanche.NativeBalanceResult

	if err := json.Unmarshal(result.Data, &decoded); err != nil {
		t.Fatalf("could not decode ToolResult.Data: %v", err)
	}

	if decoded.Balance.Raw != expected.Balance.Raw {
		t.Fatalf(
			"balance raw = %q, want %q",
			decoded.Balance.Raw,
			expected.Balance.Raw,
		)
	}
}

func TestNativeBalanceToolRejectsUnsupportedChain(t *testing.T) {
	client := &mockChainClient{}
	tool, err := NewNativeBalanceTool(client)
	if err != nil {
		t.Fatalf("NewNativeBalanceTool() returned error: %v", err)
	}

	result, err := tool.Execute(
		context.Background(),
		json.RawMessage(`{
			"wallet_address":
			"0x0000000000000000000000000000000000000001",
			"chain_id": 43114
		}`),
	)
	if err != nil {
		t.Fatalf("Execute() returned Go error: %v", err)
	}

	if result.Error == nil {
		t.Fatal("Execute() returned no structured tool error")
	}

	if result.Error.Code != "UNSUPPORTED_CHAIN_ID" {
		t.Fatalf(
			"tool error code = %q, want UNSUPPORTED_CHAIN_ID",
			result.Error.Code,
		)
	}

	if client.callCount != 0 {
		t.Fatalf(
			"chain client was called %d time(s), want 0",
			client.callCount,
		)
	}
}

func TestNativeBalanceToolRejectsUnknownField(t *testing.T) {
	client := &mockChainClient{}
	tool, err := NewNativeBalanceTool(client)
	if err != nil {
		t.Fatalf("NewNativeBalanceTool() returned error: %v", err)
	}

	result, err := tool.Execute(
		context.Background(),
		json.RawMessage(`{
			"wallet_address":
			"0x0000000000000000000000000000000000000001",
			"chain_id": 43113,
			"private_key": "must-not-be-accepted"
		}`),
	)
	if err != nil {
		t.Fatalf("Execute() returned Go error: %v", err)
	}

	if result.Error == nil {
		t.Fatal("Execute() returned no structured tool error")
	}

	if result.Error.Code != "INVALID_TOOL_ARGUMENTS" {
		t.Fatalf(
			"tool error code = %q, want INVALID_TOOL_ARGUMENTS",
			result.Error.Code,
		)
	}

	if client.callCount != 0 {
		t.Fatalf(
			"chain client was called %d time(s), want 0",
			client.callCount,
		)
	}
}

func TestNativeBalanceToolReturnsChainError(t *testing.T) {
	expectedError := errors.New("mock RPC unavailable")

	client := &mockChainClient{
		err: expectedError,
	}

	tool, err := NewNativeBalanceTool(client)
	if err != nil {
		t.Fatalf("NewNativeBalanceTool() returned error: %v", err)
	}

	_, err = tool.Execute(
		context.Background(),
		json.RawMessage(`{
			"wallet_address":
			"0x0000000000000000000000000000000000000001",
			"chain_id": 43113
		}`),
	)

	if !errors.Is(err, expectedError) {
		t.Fatalf(
			"Execute() error = %v, want wrapped chain error",
			err,
		)
	}
}
