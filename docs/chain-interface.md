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

Allowance definitions: backend/internal/avalanche/allowance_reader.go.
Receipt definitions: backend/internal/avalanche/receipts.go.
History response types: backend/internal/avalanche/glacier.go.

| Interface | Method | Implementation status |
|---|---|---|
| ChainReader | ReadNativeBalance | Implemented by DataAPIClient; wired to get_native_balance |
| TokenBalanceReader | ListTokenBalances | Implemented by DataAPIClient; wired to list_token_balances; live ERC-20 request returned HTTP 500 during verification |
| SwapQuoter | QuoteExactInput | DTO/interface defined; adapter pending |
| LendingPositionReader | ReadLendingPosition | DTO/interface defined; adapter pending |
| VaultPositionReader | ReadVaultPosition | DTO/interface defined; protocol and adapter pending |
| AllowanceReader | ReadAllowance | Implemented by RPCAllowanceReader; connected through get_allowance |
| ReceiptReader | ReadReceipt | Implemented by RPCReceiptReader; unit-tested; not wired into a tool or main.go |

DataAPIClient implements ChainReader and TokenBalanceReader.
Low-level RPC balance and metadata functions remain available.
They are not an automatic fallback for the AI-facing token-list tool.

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

Domain DTO based on the Data API observation displayed at
2026-11-08T04:32:31Z. This is historical evidence, not current balance.

```json
{
  "wallet_address": "0x4DFb0d91C07A7C706F65F974953cE8b111D27Ee5",
  "balance": {
    "asset": {
      "chain_id": 43113,
      "symbol": "AVAX",
      "decimals": 18
    },
    "raw": "99999999831919520",
    "formatted": "0.09999999983191952"
  },
  "provenance": {
    "source": "Avalanche Data API",
    "source_type": "data_api",
    "chain_id": 43113,
    "observed_at": "2026-11-08T04:32:31Z",
    "mode": "live",
    "is_demo": false
  }
}
```

The saved upstream response contains nativeTokenBalance, not this DTO.
The timestamp was recorded after displaying the response.
No block number was supplied for this balance.

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
TestInitClient uses a local JSON-RPC test server.
The separate live snapshot test requires explicit opt-in.

## Balance tools handoff — roadmap 2026-11-02

Verification was performed on 2026-11-08.

### Adapter boundaries

- ChainReader.ReadNativeBalance(ctx, walletAddress) returns
  NativeBalanceResult.
- TokenBalanceReader.ListTokenBalances(ctx, walletAddress, pageSize,
  pageToken) returns TokenBalancePage.
- DataAPIClient implements both interfaces.
- Dev 1 owns tool schemas, response envelopes and AI wording.
- Dev 2 owns adapter data validation, units and provenance.
- Existing tool wiring is in read_tools.go and token_balance_tool.go.

### Amount and metadata rules

- raw is a base-10 integer string within uint256 bounds.
- formatted is computed without float64.
- Token identity is chain ID plus contract address.
- Missing/null decimals is an error; zero decimals is valid.
- Unknown token reputation does not itself invalidate a balance.
- Unknown reputation must not be presented as a safety endorsement.
- Do not convert upstream errors into successful zero balances.

### Token-list scope

- ListTokenBalances returns one page, not complete wallet holdings.
- Page size is 1–500; the tool defaults to 100.
- The request enables filterSpamTokens=true.
- NextPageToken indicates that another page is available.
- Missing a token from one page does not establish its balance.
- Successful empty pages are distinct from HTTP or decoding errors.
- No automatic RPC token-discovery fallback is implemented.
- Direct RPC balanceOf reads require known token addresses.

### Provenance rules

- Live Data API results use source="Avalanche Data API",
  source_type="data_api", mode="live" and is_demo=false.
- observed_at is observation time, not block time.
- Omit block_number when unavailable.
- Saved observations are historical; do not relabel them as fresh reads.

### Live evidence

- Wallet: 0x4DFb0d91C07A7C706F65F974953cE8b111D27Ee5.
- Native RPC and Data API raw balances matched:
  99999999831919520 wei.
- Reads occurred at different times; same-block equality was not verified.
- DemoToken RPC balance was 0 and decimals was 18.
- DemoToken: 0xcCCb83Fbe349fAebDE74Ce633D25B282E6FDd21A.
- The ERC-20 Data API endpoint returned HTTP 500 twice.
- The failure cause remains undetermined.
- Live ERC-20 cross-source verification remains incomplete.
- Evidence index:
  backend/internal/avalanche/testdata/balances_verification.json.

### Local verification

- RPC ABI decoding, zero/max balances and invalid response lengths.
- Metadata validation and context error propagation.
- Data API zero balance, unknown reputation and missing/null decimals.
- Exact large integers and uint256 overflow rejection.
- Wrong chain, invalid addresses and invalid raw values.
- Empty/missing lists, oversized pages and malformed JSON.
- HTTP 429/500 and context cancellation/deadline handling.
- These unit tests use local servers or mocks, not live APIs.

## History, allowance and receipt handoff — roadmap 2026-11-03

Verification was performed on 2026-11-08.

### Transaction history

AI-facing tool: list_transactions in transaction_tool.go.

Arguments:
- wallet_address: required wallet address.
- chain_id: required, must be 43113.
- page_size: optional, defaults to 10; range 1–20.
- page_token: optional opaque cursor, at most 2048 bytes.
  Pass it unchanged; do not trim or decode it.

The tool returns one page. Its data contains:
wallet_address, transactions, optional next_page_token,
source, chain_id and observed_at.

Transactions currently use the Data API shape:
transactions[].nativeTransaction, including chainId,
blockNumber, blockTimestamp, txHash, txStatus, from, to and value.
These upstream field names remain camelCase.

The current Go DTO does not preserve erc20Transfers or decoded
method information. Raw fixtures retain those upstream fields.
Do not claim that the tool returns decoded ERC-20 transfer details.

A transaction can involve the wallet through an ERC-20 transfer
even when nativeTransaction.from/to do not match the wallet.
Do not filter such transactions out using native from/to alone.

DataAPIClient.ListTransactions accepts page sizes 1–100.
It rejects oversized pages, oversized input/output cursors,
missing/null transaction arrays and immediately repeated cursors.

ReadTransactionHistory is a separate bounded helper:
- At most two pages, requesting 50 records per page.
- At most 100 records.
- Rejects repeated continuation cursors.
- Returns Truncated=true when another cursor remains at the limit.
- It is not the helper called by list_transactions.

Neither a page nor the bounded helper proves complete wallet history.
An empty page does not prove the wallet has never transacted.

The tool has a five-second timeout.
Parent request cancellation is propagated as a Go error.

### History error mapping

| Condition | Tool code |
|---|---|
| Invalid JSON or unknown argument | INVALID_TOOL_ARGUMENTS |
| Invalid wallet address | INVALID_WALLET_ADDRESS |
| Chain other than 43113 | UNSUPPORTED_CHAIN_ID |
| Page size outside 1–20 | INVALID_PAGE_SIZE |
| Input cursor exceeds 2048 bytes | INVALID_PAGE_TOKEN |
| Read timeout | UPSTREAM_TIMEOUT |
| Client returns an error, including HTTP or decoding errors | UPSTREAM_ERROR |
| Client returns nil page or too many records | UPSTREAM_INVALID_RESPONSE |
| Successful empty page | EMPTY_HISTORY |

EMPTY_HISTORY currently accompanies data containing transactions=[].
The next cursor, if present, must still be preserved.
Provider rejection of a cursor currently maps to UPSTREAM_ERROR;
there is no dedicated invalid-provider-cursor code.

### ERC-20 allowance

AI-facing tool: get_allowance in allowance_tool.go.

Required arguments:
wallet_address, chain_id, token_address and spender_address.

wallet_address is the ERC-20 owner.
The reader queries allowance(owner, spender) on token_address.

- Only application-allowlisted tokens are accepted.
- A valid unfamiliar spender is accepted for this read.
- No spender allowlist is implemented in this reader.
- Never invent a missing token or spender address.
- The reader verifies Fuji chain ID before contract calls.
- Allowance and decimals are read at the same pinned block.
- The read has a five-second timeout.
- Amounts retain raw integer precision and token decimals.
- The token symbol is not guessed.

AllowanceResult contains:
owner_address, spender_address, allowance and provenance.
allowance contains asset, raw and formatted.

Allowance is distinct from:
- Wallet token balance.
- Application budget stored by AllowanceManager.
- Approval from AllowanceManager to a protocol adapter.

Zero allowance is valid.
Allowance may exceed the current wallet balance.
Reading allowance must not cap it to balance.
A positive allowance does not guarantee transfer success.
This tool does not approve, sign or broadcast.

### Allowance error mapping

| Condition | Tool code |
|---|---|
| Invalid JSON or unknown argument | INVALID_TOOL_ARGUMENTS |
| Invalid wallet address | INVALID_WALLET_ADDRESS |
| Invalid token address | INVALID_TOKEN_ADDRESS |
| Invalid spender address | INVALID_SPENDER_ADDRESS |
| Unsupported input chain or wrong RPC chain | UNSUPPORTED_CHAIN_ID |
| Token outside application allowlist | UNSUPPORTED_TOKEN |
| Read timeout | UPSTREAM_TIMEOUT |
| Unavailable token decimals | TOKEN_METADATA_UNAVAILABLE |
| Other reader failure | UPSTREAM_ERROR |

Parent request cancellation is propagated as a Go error.
Never convert failures into a successful zero allowance.

### Receipt reader

Definitions and implementation: receipts.go.
Tests: receipts_test.go.

NewRPCReceiptReader accepts ReceiptRPCClient, which exposes:
ChainID, TransactionByHash and TransactionReceipt.

ReceiptRPCClient is separate from RPCReader.
Existing balance interfaces do not need additional methods.
The caller owns the RPC client and must close it.

ReadReceipt(ctx, transactionHash) returns ReceiptResult or a Go error.
The reader validates a 32-byte 0x-prefixed hash and Fuji chain ID.
It checks the returned transaction identity and recovers its sender.

ReceiptResult fields:

| Field | Meaning |
|---|---|
| transaction_hash | Transaction hash |
| from_address | Recovered transaction sender |
| to_address | Destination; null for contract creation |
| input | 0x-prefixed calldata |
| value_raw | Decimal native value in wei, excluding fees |
| chain_id | 43113 |
| block_number | Block number; null while pending |
| block_hash | Block hash; null while pending |
| status | pending, succeeded or failed |
| logs | Raw receipt logs; empty array when absent |
| provenance | Source, chain and observation metadata |

ReceiptLog preserves address, topics, data, block_number,
transaction_hash, transaction_index, log_index and removed.

- pending requires a pending transaction and a not-found receipt.
- succeeded requires receipt status 1.
- failed requires receipt status 0.
- Timeout, cancellation and upstream failures remain Go errors.
- A transaction not found is an error, not automatically pending.
- A mined transaction with a missing receipt returns an error.
- Inconsistent receipt identity, block or logs are rejected.
- Removed logs are rejected.
- Success describes execution in the reported block, not finality
  or application-level correctness.

The reader uses a five-second timeout.
It is not wired into main.go or exposed as an AI tool yet.
Live curl evidence validates source data; it does not establish
that the Go reader has been exercised against live RPC.

### Captured samples

Directory: backend/internal/avalanche/testdata/

- history_data_api_http500.json: initial upstream failure.
- history_data_api_page1.json: successful first page.
- history_data_api_page2.json: successful continuation page.
- receipt_fuji_rpc.json: chain ID, transaction and receipt batch.

History observations:
- Initial HTTP 500: 2026-11-08T05:34:56Z.
- Page 1 HTTP 200: 2026-11-08T05:36:37Z.
- Page 2 HTTP 200: 2026-11-08T05:37:37Z.
- Four distinct transactions across two pages.
- A continuation cursor remains; history is incomplete.
- No automatic RPC history fallback was added.

Receipt observation: 2026-11-08T06:20:03Z.
Transaction:
0x3b61008d6400772ce94848239c431d7dae62fa78360af24a8b334d6ae81b4e72

RPC and Snowtrace Fuji match on chain, hash, block 60111917,
execution success, sender, destination, zero native value and
six logs. See docs/evidence.md.

Samples are historical evidence, not fresh wallet state.
Backend reads do not submit transactions.