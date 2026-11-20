#!/usr/bin/env bash
set -euo pipefail

input_dir='D:/nativebudget-build'
MSYS_NO_PATHCONV=1 docker run --rm -i \
  --platform linux/amd64 --cpus=4 --memory=8g \
  -e GOTOOLCHAIN=local -e CGO_ENABLED=1 -e GOMAXPROCS=4 -e GOFLAGS=-p=2 \
  --mount type=volume,source=dev2-native-source,target=/baseline,readonly \
  --mount type=volume,source=dev2-native-gomod,target=/go/pkg/mod \
  --mount type=volume,source=dev2-native-gocache,target=/root/.cache/go-build \
  --mount "type=bind,source=$input_dir,target=/input,readonly" \
  golang:1.26.10@sha256:cfa27959837144bf4649802ba38e59daffd0dd2a7994aa83daaf113285b0f551 \
  bash -s <<'BASH'
set -euo pipefail

git -c safe.directory=/baseline/avalanchego clone -q --no-local \
  /baseline/avalanchego /tmp/avalanchego
cd /tmp/avalanchego
git checkout -q --detach 4bb5c1dc13e1b6ff4eaf49fe8674666fbb342a17

vm_id=srEXiWaHuhNyGwPUi444Tu47ZEDwxTWrbQiuD7FmgSAQ6X7Dy
mkdir -p /tmp/nativebudget-bin/plugins /tmp/nativebudget-networks
install -m 755 /input/avalanchego /tmp/nativebudget-bin/avalanchego
install -m 755 /input/subnet-evm-nativebudget-trusted-executor "/tmp/nativebudget-bin/plugins/$vm_id"
printf '%s  %s\n' \
  8b015978db793bb336dd6de3a2aeb43f5f61d894cf3e17b4ae370c85ea82a34e \
  "/tmp/nativebudget-bin/plugins/$vm_id" | sha256sum -c -

cd graft/subnet-evm
mkdir -p nativebudget-regression
cat > nativebudget-regression/main_test.go <<'GO'
package regression

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/genesis"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/ethclient"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/tests/utils"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
	"github.com/ava-labs/avalanchego/utils/logging"
	ethereum "github.com/ava-labs/libevm"
	"github.com/ava-labs/libevm/accounts/abi"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/crypto"
	"github.com/stretchr/testify/require"
)

type contract struct {
	address common.Address
	abi abi.ABI
}

func TestTrustedExecutorRegression(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()

	genesisBytes, err := os.ReadFile("/input/nativebudget-trusted-executor-genesis.json")
	require.NoError(t, err)
	var spec struct {
		Config struct {
			NativeBudgetConfig struct {
				TrustedExecutor common.Address
			}
		}
	}
	require.NoError(t, json.Unmarshal(genesisBytes, &spec))
	trusted := spec.Config.NativeBudgetConfig.TrustedExecutor

	bundle, err := os.ReadFile("/input/manager-artifacts.json")
	require.NoError(t, err)
	var artifacts map[string]struct {
		ABI json.RawMessage
		Bytecode string
	}
	require.NoError(t, json.Unmarshal(bundle, &artifacts))

	key, err := crypto.ToECDSA(genesis.EWOQKey.Bytes())
	require.NoError(t, err)
	owner := crypto.PubkeyToAddress(key.PublicKey)
	require.Equal(t, crypto.CreateAddress(owner, 0), trusted)

	nodes := tmpnet.NewNodesOrPanic(1)
	subnet := utils.NewTmpnetSubnet("nativebudget", genesisBytes, utils.DefaultChainConfig, nodes...)
	network := utils.NewTmpnetNetwork("nativebudget-regression", nodes, nil, subnet)
	network.DefaultRuntimeConfig.Process = &tmpnet.ProcessRuntimeConfig{
		AvalancheGoPath: "/tmp/nativebudget-bin/avalanchego",
		PluginDir: "/tmp/nativebudget-bin/plugins",
	}
	t.Log("Starting local AvalancheGo network")
	require.NoError(t, tmpnet.BootstrapNewNetwork(ctx, logging.NoLog{}, network, "/tmp/nativebudget-networks"))
	t.Cleanup(func() {
		stopCtx, stop := context.WithTimeout(context.Background(), 20*time.Second)
		defer stop()
		if err := network.Stop(stopCtx); err != nil {
			t.Logf("stop local network: %v", err)
		}
	})

	url := fmt.Sprintf("%s/ext/bc/%s/rpc", nodes[0].GetAccessibleURI(), subnet.Chains[0].ChainID)
	client, err := ethclient.DialContext(ctx, url)
	require.NoError(t, err)
	defer client.Close()

	rpcCtx, cancelRPC := context.WithTimeout(ctx, time.Minute)
	defer cancelRPC()
	var chainID *big.Int
	for {
		chainID, err = client.ChainID(rpcCtx)
		if err == nil {
			break
		}
		select {
		case <-rpcCtx.Done():
			t.Fatalf("RPC not ready at %s: %v", url, err)
		case <-time.After(time.Second):
		}
	}
	require.Zero(t, chainID.Cmp(big.NewInt(99999)))
	t.Log("chain_id:", chainID)

	const gas = 5_000_000
	send := func(label string, to *common.Address, data []byte, status uint64) *types.Receipt {
		t.Helper()
		nonce, err := client.NonceAt(ctx, owner, nil)
		require.NoError(t, err, label)
		price, err := client.SuggestGasPrice(ctx)
		require.NoError(t, err, label)
		tx, err := types.SignTx(types.NewTx(&types.LegacyTx{
			Nonce: nonce, To: to, Gas: gas, GasPrice: price, Data: data,
		}), types.LatestSignerForChainID(chainID), key)
		require.NoError(t, err, label)
		require.NoError(t, client.SendTransaction(ctx, tx), label)

		for {
			receipt, err := client.TransactionReceipt(ctx, tx.Hash())
			if err == nil {
				t.Logf("%s: tx=%s status=%d block=%s gas=%d",
					label, receipt.TxHash.Hex(), receipt.Status, receipt.BlockNumber, receipt.GasUsed)
				require.Equal(t, status, receipt.Status, label)
				return receipt
			}
			require.True(t, errors.Is(err, ethereum.NotFound), "%s: %v", label, err)
			select {
			case <-ctx.Done():
				t.Fatalf("%s: wait for receipt %s: %v", label, tx.Hash(), ctx.Err())
			case <-time.After(time.Second):
			}
		}
	}

	pack := func(target contract, method string, args ...any) []byte {
		t.Helper()
		data, err := target.abi.Pack(method, args...)
		require.NoError(t, err, method)
		return data
	}
	deploy := func(name string, args ...any) contract {
		t.Helper()
		a, ok := artifacts[name]
		require.True(t, ok, "missing artifact %s", name)
		parsed, err := abi.JSON(strings.NewReader(string(a.ABI)))
		require.NoError(t, err, name)
		code := common.FromHex(a.Bytecode)
		require.NotEmpty(t, code, name)
		receipt := send("deploy_"+name, nil, append(code, pack(contract{abi: parsed}, "", args...)...), 1)
		return contract{receipt.ContractAddress, parsed}
	}
	write := func(label string, target contract, method string, args ...any) *types.Receipt {
		return send(label, &target.address, pack(target, method, args...), 1)
	}

	nonce, err := client.NonceAt(ctx, owner, nil)
	require.NoError(t, err)
	require.Zero(t, nonce)
	manager := deploy("PrecompileAllowanceManager")
	require.Equal(t, trusted, manager.address)
	token := deploy("DemoToken")
	vault := deploy("DemoVault", token.address)
	adapter := deploy("VaultAdapter", manager.address, vault.address)

	write("support_token", manager, "setTokenSupported", token.address, true)
	write("support_shares", manager, "setTokenSupported", vault.address, true)
	write("set_vault_adapter", manager, "setVaultAdapter", token.address, adapter.address, vault.address)
	write("mint", token, "mint", owner, big.NewInt(200))
	write("approve", token, "approve", manager.address, big.NewInt(200))

	nativeABI, err := abi.JSON(strings.NewReader("[" +
		"{\"type\":\"function\",\"name\":\"setBudget\",\"inputs\":[{\"type\":\"address\"},{\"type\":\"address\"},{\"type\":\"uint256\"},{\"type\":\"uint256\"}],\"outputs\":[]}," +
		"{\"type\":\"function\",\"name\":\"revokeBudget\",\"inputs\":[{\"type\":\"address\"},{\"type\":\"address\"}],\"outputs\":[]}" +
		"]"))
	require.NoError(t, err)
	native := contract{common.HexToAddress("0x0300000000000000000000000000000000000000"), nativeABI}
	header, err := client.HeaderByNumber(ctx, nil)
	require.NoError(t, err)
	deadline := new(big.Int).SetUint64(max(header.Time, uint64(time.Now().Unix())) + 3600)

	checkState := func(label string, block *big.Int, budget, ownerAssets, shares, vaultAssets, allowance int64) {
		t.Helper()
		for _, item := range []struct {
			name string
			target contract
			method string
			args []any
			want int64
		}{
			{"remaining", manager, "remaining", []any{owner, token.address}, budget},
			{"owner_assets", token, "balanceOf", []any{owner}, ownerAssets},
			{"owner_shares", vault, "balanceOf", []any{owner}, shares},
			{"vault_assets", token, "balanceOf", []any{vault.address}, vaultAssets},
			{"owner_allowance", token, "allowance", []any{owner, manager.address}, allowance},
		} {
			raw, err := client.CallContract(ctx, ethereum.CallMsg{
				To: &item.target.address, Data: pack(item.target, item.method, item.args...),
			}, block)
			require.NoError(t, err, label+" "+item.name)
			require.Len(t, raw, 32, label+" "+item.name)
			got := new(big.Int).SetBytes(raw)
			require.Zero(t, got.Cmp(big.NewInt(item.want)), "%s %s: got %s, want %d", label, item.name, got, item.want)
		}
		t.Logf("%s: remaining=%d owner_assets=%d owner_shares=%d vault_assets=%d owner_allowance=%d",
			label, budget, ownerAssets, shares, vaultAssets, allowance)
	}

	reject := func(label string, block *big.Int, amount, minShares int64, errorSignature string, first, second int64) *types.Receipt {
		t.Helper()
		data := pack(manager, "executeDeposit", token.address, big.NewInt(amount), big.NewInt(minShares), deadline)
		_, err := client.CallContract(ctx, ethereum.CallMsg{
			From: owner, To: &manager.address, Gas: gas, Data: data,
		}, block)
		var rpcErr interface{ ErrorData() any }
		require.True(t, errors.As(err, &rpcErr), "%s: expected %s, got %v", label, errorSignature, err)
		selector := crypto.Keccak256([]byte(errorSignature))[:4]
		want := fmt.Sprintf("0x%x%064x%064x", selector, first, second)
		raw, ok := rpcErr.ErrorData().(string)
		require.True(t, ok && strings.EqualFold(raw, want), "%s: expected %s, got %v", label, want, rpcErr.ErrorData())
		t.Logf("%s_revert: %s args=(%d,%d)", label, errorSignature, first, second)
		return send(label, &manager.address, data, 0)
	}

	set := write("native_set_budget", native, "setBudget", token.address, manager.address, big.NewInt(100), deadline)
	checkState("after_set", set.BlockNumber, 100, 200, 0, 0, 200)

	deposit := write("deposit_under_cap", manager, "executeDeposit",
		token.address, big.NewInt(40), big.NewInt(40), deadline)
	checkState("after_deposit", deposit.BlockNumber, 60, 160, 40, 40, 160)

	rollback := reject("deposit_rollback", deposit.BlockNumber, 20, 21, "InsufficientShares(uint256,uint256)", 20, 21)
	checkState("after_rollback", rollback.BlockNumber, 60, 160, 40, 40, 160)

	overCap := reject("deposit_over_cap", rollback.BlockNumber, 61, 1, "BudgetExceeded(uint256,uint256)", 60, 61)
	checkState("after_over_cap", overCap.BlockNumber, 60, 160, 40, 40, 160)

	revoke := write("native_revoke_budget", native, "revokeBudget", token.address, manager.address)
	checkState("after_revoke", revoke.BlockNumber, 0, 160, 40, 40, 160)

	revoked := reject("deposit_after_revoke", revoke.BlockNumber, 1, 1, "BudgetExceeded(uint256,uint256)", 0, 1)
	checkState("after_revoked_deposit", revoked.BlockNumber, 0, 160, 40, 40, 160)
}
GO

go test -v -count=1 -timeout 10m ./nativebudget-regression
BASH
