package database

import (
	"encoding/json"
	"testing"

	"unihack-backend/internal/ai"
)

func TestPreparedSwapIntentHashPreservesIntegerPrecision(t *testing.T) {
	action := ai.PendingAction{
		ToolName:             "prepare_swap",
		WalletAddress:        "0x0000000000000000000000000000000000000001",
		ChainID:              43113,
		RequiresConfirmation: true,
	}

	hash := func(payload string) string {
		t.Helper()
		action.Arguments = json.RawMessage(payload)

		_, digest, err := buildNormalizedIntent(&action)
		if err != nil {
			t.Fatal(err)
		}
		return digest
	}

	first := hash(`{"transaction":{"chain_id":43113},"summary":{"deadline":9007199254740992}}`)
	reordered := hash(`{ "summary": {"deadline": 9007199254740992}, "transaction": {"chain_id": 43113} }`)
	different := hash(`{"transaction":{"chain_id":43113},"summary":{"deadline":9007199254740993}}`)

	if first != reordered {
		t.Fatal("key order and whitespace must not change the normalized intent hash")
	}
	if first == different {
		t.Fatal("different integer values produced the same normalized intent hash")
	}
}
