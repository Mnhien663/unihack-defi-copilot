package ai

import (
	"context"
	"errors"
	"testing"
)

const (
	testWalletAddress = "0x0000000000000000000000000000000000000001"
	readBalanceTool   = "get_native_balance"
	prepareSwapTool   = "prepare_swap"
)

func newTestPolicyEngine() *StaticPolicyEngine {
	return NewStaticPolicyEngine(
		[]string{
			readBalanceTool,
		},
		[]string{
			prepareSwapTool,
		},
	)
}

func TestPolicyAllowsReadTool(t *testing.T) {
	engine := newTestPolicyEngine()

	decision, err := engine.Evaluate(
		context.Background(),
		PolicyRequest{
			ToolName:      readBalanceTool,
			ChainID:       FujiChainID,
			WalletAddress: testWalletAddress,
		},
	)
	if err != nil {
		t.Fatalf("Evaluate() returned error: %v", err)
	}

	if !decision.Allowed {
		t.Fatalf(
			"decision was denied: %s",
			decision.Reason,
		)
	}

	if decision.RequiresConfirmation {
		t.Fatal("read tool unexpectedly requires confirmation")
	}
}

func TestPolicyRequiresConfirmationForWriteTool(t *testing.T) {
	engine := newTestPolicyEngine()

	decision, err := engine.Evaluate(
		context.Background(),
		PolicyRequest{
			ToolName:      prepareSwapTool,
			ChainID:       FujiChainID,
			WalletAddress: testWalletAddress,
		},
	)
	if err != nil {
		t.Fatalf("Evaluate() returned error: %v", err)
	}

	if !decision.Allowed {
		t.Fatalf(
			"decision was denied: %s",
			decision.Reason,
		)
	}

	if !decision.RequiresConfirmation {
		t.Fatal("write tool did not require confirmation")
	}
}

func TestPolicyRejectsUnsupportedChain(t *testing.T) {
	engine := newTestPolicyEngine()

	decision, err := engine.Evaluate(
		context.Background(),
		PolicyRequest{
			ToolName:      readBalanceTool,
			ChainID:       43114,
			WalletAddress: testWalletAddress,
		},
	)
	if err != nil {
		t.Fatalf("Evaluate() returned error: %v", err)
	}

	if decision.Allowed {
		t.Fatal("unsupported chain was allowed")
	}

	if decision.RequiresConfirmation {
		t.Fatal("denied request unexpectedly requires confirmation")
	}
}

func TestPolicyRejectsInvalidWallet(t *testing.T) {
	engine := newTestPolicyEngine()

	decision, err := engine.Evaluate(
		context.Background(),
		PolicyRequest{
			ToolName:      readBalanceTool,
			ChainID:       FujiChainID,
			WalletAddress: "not-a-wallet",
		},
	)
	if err != nil {
		t.Fatalf("Evaluate() returned error: %v", err)
	}

	if decision.Allowed {
		t.Fatal("invalid wallet was allowed")
	}
}

func TestPolicyRejectsUnknownTool(t *testing.T) {
	engine := newTestPolicyEngine()

	decision, err := engine.Evaluate(
		context.Background(),
		PolicyRequest{
			ToolName:      "delete_everything",
			ChainID:       FujiChainID,
			WalletAddress: testWalletAddress,
		},
	)
	if err != nil {
		t.Fatalf("Evaluate() returned error: %v", err)
	}

	if decision.Allowed {
		t.Fatal("unknown tool was allowed")
	}
}

func TestPolicyHonorsCancelledContext(t *testing.T) {
	engine := newTestPolicyEngine()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := engine.Evaluate(
		ctx,
		PolicyRequest{
			ToolName:      readBalanceTool,
			ChainID:       FujiChainID,
			WalletAddress: testWalletAddress,
		},
	)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf(
			"Evaluate() error = %v, want context.Canceled",
			err,
		)
	}
}

func TestPolicyTreatsConflictingToolAsWrite(t *testing.T) {
	engine := NewStaticPolicyEngine(
		[]string{
			"conflicting_tool",
		},
		[]string{
			"conflicting_tool",
		},
	)

	decision, err := engine.Evaluate(
		context.Background(),
		PolicyRequest{
			ToolName:      "conflicting_tool",
			ChainID:       FujiChainID,
			WalletAddress: testWalletAddress,
		},
	)
	if err != nil {
		t.Fatalf("Evaluate() returned error: %v", err)
	}

	if !decision.Allowed {
		t.Fatalf(
			"decision was denied: %s",
			decision.Reason,
		)
	}

	if !decision.RequiresConfirmation {
		t.Fatal("conflicting tool was not treated as a write tool")
	}
}
