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
| SwapQuoter | QuoteExactInput | Implemented by LFJQuoteReader for the configured Fuji USDT/USDC pair; not wired into an AI tool or main.go |
| LendingPositionReader | ReadLendingPosition | DTO/interface defined; adapter pending |
| VaultPositionReader | ReadVaultPosition | Implemented by RPCVaultReader for the configured Fuji DemoVault; not wired into main.go or an AI tool |
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

## Swap quote handoff — roadmap 2026-11-04

### Implementation and scope

- Implementation: backend/internal/avalanche/quote.go.
- Tests: backend/internal/avalanche/quote_test.go.
- Constructor: NewLFJQuoteReader(client RPCReader).
- Interface: SwapQuoter.QuoteExactInput(ctx, request).
- Supported network: Avalanche Fuji, chain ID 43113.
- Supported route: the configured LFJ V2.2 USDT/USDC pair,
  in either direction.
- Route discovery and native AVAX wrapping are not implemented.
- The reader is not wired into main.go or an AI-facing tool yet.
- Dev 1 owns get_swap_quote schema, tool registration and AI wording.

### Request

SwapQuoteRequest contains:
- token_path: exactly two Asset objects.
- amount_in_raw: positive decimal integer string, within uint128.
- slippage_bps: optional integer; default 50, maximum 500.
  Explicit zero is supported.

Token addresses and chain IDs must match the configured route.
Asset decimals must match on-chain metadata.
Caller-supplied symbols are not trusted.

Convert user-entered decimal amounts using ParseTokenAmount before
calling the reader. Reject excess fractional digits without rounding.
Do not convert amounts through floating point.

### Response

SwapQuoteResult contains:
- protocol and router_address.
- token_path and hops describing the configured direct route.
- amount_in and amount_out.
- min_amount_out.
- fee, denominated in the input token.
- slippage_bps.
- expires_at.
- provenance, including the pinned block number.

All amounts use TokenAmount: asset, raw and formatted.

The reader checks pair token identities, reads token decimals and
calls router getSwapOut at the same selected block.

A quote is rejected if any input remains unconsumed or output is zero.
Router output already reflects swap fees; do not subtract fee again.

### Slippage and expiry

minOut = floor(amountOut * (10000 - slippageBps) / 10000).

A minimum output that rounds to zero is rejected.

Quote validity is 60 seconds from the start of the read.
RPC read time consumes this validity.
The read has a five-second timeout.

Call ValidateQuoteExpiry(quote, time.Now().UTC()) before using a quote.
At expires_at, the quote is already expired.
This helper checks freshness only; it does not validate calldata.

A future execution integration must enforce minimum output and a
deadline in the transaction itself. A fresh quote does not guarantee
execution or reserve availability.

No price-impact field is returned because a validated price-impact
calculation has not been implemented.

### Errors

Use errors.Is where a sentinel is available:

| Error | Meaning |
|---|---|
| ErrUnsupportedSwapRoute | Unsupported path, including identical tokens |
| ErrWrongChain | Request or RPC chain is not Fuji |
| ErrInvalidAddress | Malformed token address |
| ErrInsufficientLiquidity | Partial input consumption or zero output |
| ErrQuoteExpired | Quote validity has elapsed |
| ErrTokenMetadataUnavailable | Token decimals could not be read |
| context.Canceled | Caller canceled the read |
| context.DeadlineExceeded | Read deadline exceeded |

Other validation and upstream failures return Go errors.
Never convert errors into a successful zero quote.
AI-facing error codes remain the responsibility of the tool adapter.

### Source and configuration

- Source: LFJ Fuji LBRouter.
- source_type=rpc, mode=live, is_demo=false.
- Protocol manifest: contracts/deployments/fuji.json.
- The current Go reader uses address constants in quote.go;
  it does not load the JSON manifest automatically.
- Keep manifest and Go configuration consistent when changing routes.
- Minimal ABI definitions are embedded in quote.go.

Router:
0x18556DA13313f3532c54711497A8FedAC273220E

Pair:
0xc3b7307184381f65B398d385a71EF0490239a2b3

Token X, USDT:
0xAb231A5744C8E6c45481754928cCfFFFD4aa0732

Token Y, USDC:
0xB6076C93701D6a07266c31066B298AeC6dd65c2d

Both tokens use 6 decimals.
LFJ USDC is distinct from the previously tested Aave USDC.

### Evidence and limitations

- Verification block: 60720761.
- Observation: 2026-11-08T07:33:09.163Z.
- Both directions returned output=999900 for input=1000000,
  with inputLeft=0 and fee=100 raw input-token units.
- Fixture: backend/internal/avalanche/testdata/quote_fuji_router.json.
- Script: contracts/scripts/check-swap-quote.mjs.
- Bytecode capture:
  backend/internal/avalanche/testdata/quote_fuji_contract_code.json.

The Node script exercised live router reads.
Go adapter tests used mocks; live Go execution remains unverified.
Captured amounts are historical evidence, not fixed prices.

Quoter route selection was not re-executed in this verification.
No demo AMM was deployed.
Wallet swap execution and AllowanceManager integration remain unverified.
The reader does not approve, sign or broadcast transactions.

## Aave position/oracle handoff — roadmap 2026-11-05

### Implementation

- Reader and DTOs: backend/internal/avalanche/position.go.
- Tests: backend/internal/avalanche/position_test.go.
- Constructor:
  NewAavePositionReader(client PositionRPCClient, config AavePositionConfig).
- Method:
  ReadPosition(ctx, walletAddress) (AavePositionResult, error).
- PositionRPCClient embeds RPCReader and adds HeaderByNumber.
- The caller owns the RPC client and must close it.
- The whole read has a ten-second timeout.
- Reader is not wired into main.go or an AI-facing tool yet.

This specialized result supplements LendingPositionResult.
It contains account aggregates, one configured reserve and oracle data.
It does not implement the existing LendingPositionReader interface.

### Supported scope

- Aave V3 Fuji, chain ID 43113.
- The configured USDC reserve only.
- Account aggregates cover all reserves used by the wallet.
- Reserve details and oracle price cover only the configured USDC.
- These scopes must not be presented as equivalent.
- Other reserve enumeration, eMode and isolation-mode modelling
  are not implemented.
- No borrowing or liquidation transaction is submitted.

### Source validation

The reader:
- Validates a nonzero wallet address and Fuji RPC chain ID.
- Selects a block and reads its timestamp.
- Resolves Pool and oracle through Addresses Provider.
- Rejects a Pool different from the configured Pool.
- Checks the Data Provider belongs to the configured market.
- Pins contract reads to the selected block.
- Rechecks the block header after reading and rejects an observed
  block change. This does not guarantee finality.
- Returns errors rather than partial position results.

### Units

| Field | Unit |
|---|---|
| Account collateral/debt/available borrow | Oracle base-currency raw units |
| Oracle base_currency_unit_raw | Raw units per one base-currency unit |
| Reserve supplied/stable debt/variable debt | Underlying USDC raw units |
| USDC decimals | 6 |
| LTV and liquidation thresholds | Basis points; 10000 = 100% |
| Account health_factor_raw | Scale 10^18 |
| Oracle price_raw | Base-currency units per whole underlying token |
| Oracle updated_at and block_timestamp | Unix seconds |
| Oracle age_seconds | Block timestamp minus source updatedAt |

Base currency is currently USD, represented by the zero address.
The verified base currency unit is 100000000.
The reader checks source decimals against the oracle unit.

Keep integer amounts as strings. Do not use float64 for risk math.
Reserve thresholds are not account-weighted thresholds.

### Empty positions and collateral

- Zero supplied balance, collateral or debt is valid data.
- Account.HasDebt is based on aggregate total debt.
- When there is no debt, Aave may return uint256 maximum as health factor.
- Preserve that raw sentinel but display a no-debt state rather than
  a normal numeric health factor.
- UserCollateralEnabled is distinct from ReserveCollateralEnabled.
- A supplied asset is not automatically active collateral.
- Frozen or inactive reserve flags are returned without discarding
  existing balances or debt.

### Oracle validation and freshness

The reader requires a direct price source supporting decimals()
and latestRoundData().

It rejects:
- Missing direct source or nonpositive oracle price.
- Nonpositive source answer or answer different from oracle price.
- Source scale different from oracle base-currency unit.
- Invalid round identifiers.
- Missing, inconsistent or future source timestamps.

Unsupported source metadata returns an error.
There is no application fallback to a guessed or fixed price.

AavePositionConfig.MaxOracleAgeSeconds:
- 0: no age policy configured; freshness_status is unknown.
- Positive: age is checked against this application policy.
- Age equal to the limit is accepted.
- Age above the limit returns ErrOraclePriceStale.
- An accepted age returns freshness_status=fresh under that policy only.

The policy is not a verified feed heartbeat.
No default maximum age has been established for this deployment.
Do not label unknown freshness as verified.
Do not replace source updatedAt with RPC observation time.

### Errors

Use errors.Is where applicable:

| Error | Meaning |
|---|---|
| ErrInvalidAddress | Invalid or zero wallet |
| ErrWrongChain | RPC endpoint is not Fuji |
| ErrPositionSourceMismatch | Market, Pool or configured asset metadata mismatch |
| ErrOraclePriceUnavailable | Invalid price, source scale, round or timestamps |
| ErrOraclePriceStale | Source age exceeds the configured application limit |
| context.Canceled | Read canceled |
| context.DeadlineExceeded | Read timed out |

Other RPC, ABI and source metadata errors remain Go errors.
Do not convert errors into successful zero debt or zero collateral.
Do not calculate liquidation values from an errored or partial result.

### Verified sample

- Fixture:
  backend/internal/avalanche/testdata/position_fuji_oracle.json.
- Script: contracts/scripts/check-aave-position.mjs.
- Manifest: contracts/deployments/fuji.json, protocols.AaveV3.
- Observation: 2026-11-08T09:23:31.121Z.
- Block: 60724294.
- Wallet aggregate collateral and debt: zero.
- USDC supplied, stable debt and variable debt: zero.
- Reserve LTV: 8250 bps; liquidation threshold: 8600 bps.
- USDC oracle price: 99975000 with base unit 100000000.
- Source updatedAt: 1794096753.
- Source age at block: 33039 seconds.
- Feed heartbeat and acceptable freshness remain unverified.

The Node script exercised live reads.
Go reader verification currently uses mocks.
A borrowed live position has not been verified.
Samples are historical, not current wallet state.

### Dev 1 integration

Dev 1 owns the AI tool, registration and deterministic risk calculations.
Use the specialized reader without changing the chat response schema
implicitly.

Account aggregates can support account-level reporting.
One USDC reserve observation is insufficient to reconstruct a complete
multi-asset account or a general liquidation price.

Keep freshness limitations visible.
A liquidation threshold calculation is conditional on its model and
inputs; it is not a forecast of market prices.

## Vault handoff — roadmap 2026-11-06

### Deployed Fuji contracts

| Contract | Address |
|---|---|
| DemoToken | 0xcCCb83Fbe349fAebDE74Ce633D25B282E6FDd21A |
| DemoVault | 0x896C05deae3d6e35021B12DD361401736Bb8aAda |
| AllowanceManager | 0xA7c9881eb43902Ca668bb864668eAA3795f2283B |
| VaultAdapter | 0x78b5aADB9a8570C7B4387F29119201AF53008b7D |

Network: Avalanche Fuji, chain ID 43113.
Deployment transactions and configuration records are in
contracts/deployments/fuji.json.

Manager policy admin:
0x4DFb0d91C07A7C706F65F974953cE8b111D27Ee5.

Manager supports DDEMO and vault shares.
The DDEMO deposit route points to VaultAdapter and DemoVault shares.
Adapter manager, vault and underlying bindings were checked.

This configures deposit only.
No LFJ swap execution adapter or swap route was deployed here.

### Go read interfaces

Implementation: backend/internal/avalanche/vault.go.
Tests: backend/internal/avalanche/vault_test.go.

Constructor: NewRPCVaultReader(client RPCReader).

Supported methods:
- ReadVault(ctx, VaultReadRequest) returns VaultMetricsResult.
- ReadVaultPosition(ctx, vaultAddress, walletAddress) returns
  the existing VaultPositionResult.

VaultReadRequest contains:
vault_address, wallet_address and deposit_assets_raw.

- Only the configured DemoVault is supported.
- Wallet must be a valid nonzero address.
- deposit_assets_raw is a uint256 decimal string.
- Zero is valid for a read-only preview.
- Wallet is also the preview deposit receiver.
- Reads verify Fuji and the expected underlying.
- Decimals are read separately for underlying and shares.
- Contract reads use one selected block number.
- Total read timeout is ten seconds.
- No block-hash recheck or finality guarantee is implemented here.

Metrics contain wallet position, total assets/shares, deposit input,
preview shares, maxDeposit, maxWithdraw and maxRedeem.

convertToAssets is not a withdrawal availability guarantee.
Preview does not establish wallet balance, allowance or execution success.
Preserve zero-share previews; execution must enforce a positive minimum.

### Valuation and provenance

- Underlying: DDEMO.
- Shares: dvDDEMO.
- APY is null: no yield strategy or measured yield history.
- USD TVL is null: no verified underlying price source.
- source_type=rpc, mode=live, is_demo=true.
- Live Fuji access does not make this a non-demo financial product.
- Go unit tests use mocks; live Go reads remain unverified.
- Reader is not wired into main.go or an AI tool.

### Deposit execution

User-facing manager method:

executeDeposit(token, amount, minSharesOut, deadline)

- token is DDEMO.
- amount uses underlying raw units.
- minSharesOut uses vault share raw units and must be positive.
- deadline is a Unix timestamp in seconds.
- The manager fixes share recipient to msg.sender.
- The user's transaction sender must be the intended owner.

The owner separately needs:
1. Sufficient underlying balance.
2. ERC-20 approval granting the manager sufficient allowance.
3. A sufficient, unexpired manager budget.

Budget does not replace ERC-20 approval.
Setting a budget does not transfer tokens.
Redeeming shares does not restore spent manager budget.

The adapter accepts calls only from its configured manager.
It approves the vault for the exact input and clears approval afterward.
Both manager and adapter check actual recipient share changes.
Below-minimum output or downstream failures revert the whole execution.

The current adapter implements deposit only.
Redeem tests call DemoVault directly; no manager redeem flow exists.

### ABI files

Export script: contracts/scripts/export.ts.

Contract ABIs:
- contracts/abi/AllowanceManager.json
- contracts/abi/DemoVault.json
- contracts/abi/VaultAdapter.json

Backend copies:
- backend/internal/avalanche/abi/AllowanceManager.json
- backend/internal/avalanche/abi/DemoVault.json
- backend/internal/avalanche/abi/VaultAdapter.json

ABI availability does not automatically register AI tools.
Dev 1 owns tool integration and unsigned transaction preparation.
Product transactions require user wallet review and signing.

### Validation and limits

- Contract suite: 100 passing.
- Includes local manager-to-adapter-to-vault deposit,
  actual minimum shares, donation rounding, redeem ownership,
  underlying/share allowances and downstream rollback.
- Backend suite passed after vault reader additions.
- Deployment and manager route configuration succeeded on Fuji.
- No live Fuji deposit/redeem through this integration was performed.