package avalanche

import (
	"os"
	"testing"
)

func TestInitClient(t *testing.T) {
	os.Setenv("AVALANCHE_RPC_URL", "https://api.avax-test.network/ext/bc/C/rpc")

	client, err := InitClient()
	if err != nil {
		t.Fatalf("Ket noi that bai: %v", err)
	}

	if client == nil {
		t.Fatal("Loi: Client tra ve nil")
	}

	t.Log("Ket noi Avalanche Fuji thanh cong!")
}