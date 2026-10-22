package avalanche

import (
	"context"
	"fmt"
	"os"

	"github.com/ethereum/go-ethereum/ethclient"
)

// Thiet lap ket noi voi Avalanch RPC
func InitClient() (*ethclient.Client, error) {
	rpcURL := os.Getenv("AVALANCHE_RPC_URL")
	if rpcURL == "" {
		return nil, fmt.Errorf("chua cau hinh AVALANCHE_RPC_URL trong file.env")
	}

	// Dial mo ket noi den RPC Endpoint
	client, err := ethclient.Dial(rpcURL)
	if err != nil {
		return nil, fmt.Errorf("Loi ket noi den Avalanche RPC: %v", err)
	}

	// Ping thu den mang de lay ChainID (Fuji la 43113)
	chainID, err := client.ChainID(context.Background()) 
	if err != nil {
		return nil, fmt.Errorf("Ket noi that bai, khong the lay ChainID: %v", err)
	}

	fmt.Printf("Ket noi Avalanche thanh cong! ChainId: %s", chainID.String())
	return client, nil
}