package avalanche

import (
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"os"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

// RPCSnapshot contains historical RPC data loaded from a saved file.
type RPCSnapshot struct {
	ChainID        uint64     `json:"chain_id"`
	RPCURL         string     `json:"rpc_url"`
	BlockNumber    *uint64    `json:"block_number"`
	ObservedAt     time.Time  `json:"observed_at"`
	TokenAddress   string     `json:"token_address"`
	OwnerAddress   string     `json:"owner_address"`
	SpenderAddress string     `json:"spender_address"`
	Provenance     Provenance `json:"provenance"`

	AdapterResult struct {
		NativeBalanceRaw string `json:"native_balance_raw"`
		TokenBalanceRaw  string `json:"token_balance_raw"`
		AllowanceRaw     string `json:"allowance_raw"`
		Decimals         *int   `json:"decimals"`
	} `json:"adapter_result"`
}

// LoadRPCSnapshot reads saved data without making network requests.
func LoadRPCSnapshot(path string) (*RPCSnapshot, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open RPC snapshot: %w", err)
	}
	defer file.Close()

	const maxBytes = 1 << 20
	body, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read RPC snapshot: %w", err)
	}
	if len(body) > maxBytes {
		return nil, fmt.Errorf("RPC snapshot exceeds size limit")
	}

	var snapshot RPCSnapshot
	if err := json.Unmarshal(body, &snapshot); err != nil {
		return nil, fmt.Errorf("decode RPC snapshot: %w", err)
	}

	if snapshot.ChainID != uint64(FujiChainID) {
		return nil, fmt.Errorf("RPC snapshot has unexpected chain ID")
	}
	if snapshot.BlockNumber == nil {
		return nil, fmt.Errorf("RPC snapshot is missing block number")
	}
	if snapshot.ObservedAt.IsZero() {
		return nil, fmt.Errorf("RPC snapshot is missing observation time")
	}

	addresses := []struct {
		name  string
		value string
	}{
		{"token", snapshot.TokenAddress},
		{"owner", snapshot.OwnerAddress},
		{"spender", snapshot.SpenderAddress},
	}
	for _, address := range addresses {
		if !common.IsHexAddress(address.value) {
			return nil, fmt.Errorf(
				"RPC snapshot has invalid %s address", address.name,
			)
		}
	}

	decimals := snapshot.AdapterResult.Decimals
	if decimals == nil || *decimals < 0 || *decimals > 255 {
		return nil, fmt.Errorf("RPC snapshot has missing or invalid decimals")
	}

	amounts := []struct {
		name  string
		value string
	}{
		{"native balance", snapshot.AdapterResult.NativeBalanceRaw},
		{"token balance", snapshot.AdapterResult.TokenBalanceRaw},
		{"allowance", snapshot.AdapterResult.AllowanceRaw},
	}
	for _, amount := range amounts {
		if amount.value == "" ||
			strings.IndexFunc(amount.value, func(r rune) bool {
				return r < '0' || r > '9'
			}) >= 0 {
			return nil, fmt.Errorf(
				"RPC snapshot has invalid %s raw amount", amount.name,
			)
		}

		raw, ok := new(big.Int).SetString(amount.value, 10)
		if !ok || raw.BitLen() > 256 {
			return nil, fmt.Errorf(
				"RPC snapshot %s exceeds uint256", amount.name,
			)
		}
	}

	snapshot.Provenance = Provenance{
		Source:      "Avalanche Fuji RPC",
		SourceType:  "rpc",
		ChainID:     snapshot.ChainID,
		BlockNumber: snapshot.BlockNumber,
		ObservedAt:  snapshot.ObservedAt,
		Mode:        "snapshot",
		IsDemo:      true,
	}

	return &snapshot, nil
}
