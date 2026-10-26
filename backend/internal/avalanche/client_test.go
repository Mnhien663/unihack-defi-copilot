package avalanche

import "testing"

func TestInitClient(t *testing.T) {
	t.Setenv(
		"AVALANCHE_RPC_URL",
		"https://api.avax-test.network/ext/bc/C/rpc",
	)

	client, err := InitClient()
	if err != nil {
		t.Fatalf("InitClient() returned error: %v", err)
	}
	defer client.Close()

	if client == nil {
		t.Fatal("InitClient() returned a nil client")
	}

	t.Log("Connected to Avalanche Fuji successfully")
}
