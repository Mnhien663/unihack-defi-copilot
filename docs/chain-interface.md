# Chain adapter interface

## Network and execution rules

- Target: Avalanche Fuji C-Chain, chain ID `43113`.
- Amounts are non-negative decimal integer strings in raw token units.
- Token identity is chain ID + address, not symbol alone.
- An omitted asset address identifies the native currency.
- Never invent decimals, block numbers, balances or debt amounts.
- `observed_at` is the UTC observation time, not block time.
- Omit `block_number` when the source does not provide it.
- `is_demo` identifies a custom demo integration, not whether a network
  is a testnet.
- Errors must be returned as Go errors, not replaced with zero values.
- Read operations must propagate the caller's context.
- Transaction preparation must not sign or broadcast.
- Product writes require frontend review and explicit wallet confirmation.

## Go interfaces

Definitions: `backend/internal/avalanche/types.go`.

| Interface | Method | Implementation status |
|---|---|---|
| ChainReader | ReadNativeBalance | DTO/interface defined; adapter pending |
| SwapQuoter | QuoteExactInput | DTO/interface defined; adapter pending |
| LendingPositionReader | ReadLendingPosition | DTO/interface defined; adapter pending |
| VaultPositionReader | ReadVaultPosition | DTO/interface defined; protocol and adapter pending |

Existing RPC/Data API readers remain available. They do not yet
implement these interfaces.

Dev 1 owns the AI-facing ChainClient/tool interfaces and response
envelope. Dev 2 owns chain adapters. Connect them through an adapter;
these definitions do not replace the chat API or pending_action schema.

## Verified capabilities

- RPC: Fuji chain ID, latest block and native balance.
  Go ERC-20 balanceOf, allowance and decimals reads were also
  checked against direct JSON-RPC calls at block 60155048.
  See the 2026-10-28 handoff below.
- Data API: native balance and paginated transaction history.
- LFJ: direct USDT/USDC quotes through Router and Quoter.
- Aave: USDC approve, supply and withdraw using the test deployer.
- Lending positions, oracle data, borrowing and liquidation:
  not yet verified by the Go adapter.
- Separate share-based vault: not selected.

Aave USDC:
`0x5425890298aed601595a70AB815c96711a31Bc65`

LFJ USDC:
`0xB6076C93701D6a07266c31066B298AeC6dd65c2d`

These are different assets. No conversion between them is implemented.

## Native balance response example

Illustrative JSON shape; the timestamp below is a placeholder.
The balance value was observed during the Data API spike.

```json
{
  "wallet_address": "0x4DFb0d91C07A7C706F65F974953cE8b111D27Ee5",
  "balance": {
    "asset": {
      "chain_id": 43113,
      "symbol": "AVAX",
      "decimals": 18
    },
    "raw": "99999999903593280"
  },
  "provenance": {
    "source": "avalanche_data_api",
    "chain_id": 43113,
    "observed_at": "2026-10-27T00:00:00Z",
    "is_demo": false
  }
}
```

## Exact-input quote request example

```json
{
  "token_path": [
    {
      "chain_id": 43113,
      "address": "0xAb231A5744C8E6c45481754928cCfFFFD4aa0732",
      "symbol": "USDT",
      "decimals": 6
    },
    {
      "chain_id": 43113,
      "address": "0xB6076C93701D6a07266c31066B298AeC6dd65c2d",
      "symbol": "USDC",
      "decimals": 6
    }
  ],
  "amount_in_raw": "1000000"
}
```

The observed LFJ quote at block `60110974` returned:

- Amount in raw: `1000000`.
- Amount out raw: `999900`.
- Pool: `0xc3b7307184381f65B398d385a71EF0490239a2b3`.
- Version: `V2.2`.
- Bin step: `1`.

The adapter must fetch a fresh quote for each request.
A historical quote is not an execution guarantee.
Intermediate token-path discovery is not implemented.

## Position and transaction semantics

- Lending supplied/debt amounts use underlying token units.
- Unknown debt must not be represented as zero.
- A lending result must fail if required position data is unavailable;
  partial-position responses are not defined yet.
- health_factor_raw uses a scale of 10^18; omit it when unavailable.
  Aave's no-debt maximum value requires explicit display handling.
- Vault shares and underlying assets may have different decimals.
- Vault assets corresponding to shares do not guarantee immediate
  withdrawal availability.
- UnsignedTx.value is a decimal Wei string.
- UnsignedTx.data is 0x-prefixed hexadecimal calldata.
- Gas settings and nonce are not fixed by this DTO.
- Separate approvals and protocol calls require separate transactions.
- PreparedTransaction is not the final pending_action schema.
- Allowlist, chain, wallet and calldata validation must occur before signing.

## RPC adapter handoff — 2026-10-28

### Implementation status

Low-level RPC functions are available in the avalanche package.
They do not yet implement the business-facing ChainReader,
SwapQuoter, LendingPositionReader or VaultPositionReader interfaces.

RPCReader is defined in types.go. It exposes ChainID, BlockNumber,
BalanceAt and CallContract for real clients and test mocks.

### Configuration and client lifecycle

Environment settings:

| Variable | Default | Requirement |
|---|---|---|
| AVALANCHE_RPC_URL | None | Required RPC URL |
| AVALANCHE_CHAIN_ID | 43113 | Positive expected chain ID |
| AVALANCHE_RPC_TIMEOUT | 5s | Positive Go duration |

Public functions:

```go
LoadRPCConfig() (RPCConfig, error)

InitClient() (*ethclient.Client, error)

NewRPCClient(
    ctx context.Context,
    cfg RPCConfig,
) (*ethclient.Client, error)
```

InitClient loads environment settings and calls NewRPCClient
with context.Background.

NewRPCClient validates config and checks the endpoint's chain ID.
Its timeout covers initialization and the startup chain check only.
It closes the client if the chain check fails.

The caller owns a successfully returned client and must close it.
Read operations require a caller-provided context with a deadline
or timeout; the startup timeout does not cover later reads.

### Available read functions

```go
GetLatestBlockNumber(
    ctx context.Context,
    client *ethclient.Client,
) (uint64, error)

GetNativeBalance(
    ctx context.Context,
    client *ethclient.Client,
    walletAddress string,
) (*big.Int, error)

GetERC20Balance(
    ctx context.Context,
    client RPCReader,
    tokenAddress string,
    ownerAddress string,
    blockNumber *big.Int,
) (*big.Int, error)

GetERC20Allowance(
    ctx context.Context,
    client RPCReader,
    tokenAddress string,
    ownerAddress string,
    spenderAddress string,
    blockNumber *big.Int,
) (*big.Int, error)

GetERC20Decimals(
    ctx context.Context,
    client RPCReader,
    tokenAddress string,
    blockNumber *big.Int,
) (uint8, error)
```

ERC-20 reads accept a block number. A nil block number reads
the latest state.

GetNativeBalance currently reads the latest state only.
For the fixed-block native-balance evidence below, the live test
called client.BalanceAt directly with an explicit block number.

These functions read state only. They do not approve, sign
or broadcast transactions.

### Amount semantics

```go
ParseRawAmount(value string) (*big.Int, error)

ParseTokenAmount(
    value string,
    decimals uint8,
) (*big.Int, error)
```

- Raw amounts are unsigned base-10 integers within uint256.
- ParseTokenAmount converts decimal text to raw units without float64.
- Excess fractional digits are rejected, without rounding.
- Signs, scientific notation and whitespace are rejected.
- Zero is valid for balances, allowances and the parsing helpers.
- Operations requiring a positive amount must separately reject zero.
- Convert successful big.Int results with String() for DTO raw fields.
- Do not guess decimals when metadata cannot be read.
- A successful decimals value of zero is valid.

### Error handling

Use errors.Is to identify:

| Error | Meaning |
|---|---|
| ErrWrongChain | RPC chain ID differs from the configured value |
| ErrInvalidAddress | Malformed wallet, token, owner or spender address |
| ErrTokenMetadataUnavailable | Token metadata could not be read or decoded |
| context.Canceled | Caller canceled the operation |
| context.DeadlineExceeded | Operation exceeded its deadline |

Decimals RPC failures preserve both the original error and
ErrTokenMetadataUnavailable.

Always check err before using returned values.
An error must not be converted into a successful zero balance,
zero allowance or zero decimals.

### Fuji verification evidence

Fixture:
backend/internal/avalanche/testdata/fuji_rpc_snapshot_2026-10-28.json

| Field | Value |
|---|---|
| Chain ID | 43113 |
| Block number | 60155048 |
| Block hex | 0x395e4a8 |
| Adapter observation time, UTC | 2026-10-28T15:11:21Z |
| Token | 0xcCCb83Fbe349fAebDE74Ce633D25B282E6FDd21A |
| Owner | 0x4DFb0d91C07A7C706F65F974953cE8b111D27Ee5 |
| Spender | Same address as owner |
| Native balance, raw wei | 99999999831919520 |
| Token balance, raw | 0 |
| Allowance, raw | 0 |
| Decimals | 18 |

All four values matched direct JSON-RPC calls through curl
at the same block and against the same RPC endpoint.

This verifies a separate call path, not an independent RPC provider.
The token is a demo asset. Using owner as spender verifies the
allowance read only; it does not demonstrate protocol approval.

The snapshot is historical evidence, not current wallet state.

### Validation

Completed checks:

- Configuration defaults and invalid configuration tests.
- Correct-chain, wrong-chain and startup-timeout tests.
- Raw and decimal amount parsing, including uint256 boundaries.
- ERC-20 calldata, response decoding and invalid-address tests.
- Metadata failure handling and context error propagation.
- go test ./... -count=1
- go test -race ./internal/avalanche/... -count=1
- go vet ./internal/avalanche/...

To repeat the live snapshot test from backend using Git Bash:

```bash
AVALANCHE_LIVE_TEST=1 go test ./internal/avalanche/... -run '^TestFujiLiveSnapshot$' -count=1 -v
```

A new run selects a new block and may produce different balances.
The live snapshot test is skipped unless explicitly enabled.
The existing TestInitClient test still requires live Fuji RPC access.