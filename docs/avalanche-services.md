# Avalanche Data API — Integration Evidence

## Configuration

- Base URL: `https://data-api.avax.network`
- Network: Avalanche Fuji C-Chain
- Chain ID: `43113`
- Authentication header: `x-glacier-api-key`
- Backend environment variable: `AVALANCHE_DATA_API_KEY`
- Never store the API key value in this document.

## Verified endpoints

- Native balance:
  `GET /v1/chains/43113/addresses/{address}/balances:getNative`
- Address transaction history:
  `GET /v1/chains/43113/addresses/{address}/transactions`
- History request uses `pageSize=1`, `sortOrder=desc`, and
  `pageToken` for the next page.

## Live Fuji smoke test

Command, run from `backend/` with the API key supplied through
the environment:

```bash
go run ./cmd/data-api-spike 0x4DFb0d91C07A7C706F65F974953cE8b111D27Ee5
```

Observed at: `2026-10-28T16:02:10Z`

| Field | Result |
|---|---|
| Source | Avalanche Data API |
| Chain ID | 43113 |
| Wallet | 0x4DFb0d91C07A7C706F65F974953cE8b111D27Ee5 |
| Native balance, raw Wei | 99999999831919520 |
| Native balance, AVAX | 0.09999999983191952 |
| Decimals | 18 |

### Transaction pagination

Two pages returned one transaction each, with distinct hashes
and descending block numbers.

| Page | Block | API txStatus | Value, Wei |
|---|---|---|---|
| 1 | 60111917 | 1 | 0 |
| 2 | 60111857 | 1 | 0 |

Page 1 transaction:
`0x3b61008d6400772ce94848239c431d7dae62fa78360af24a8b334d6ae81b4e72`

Page 2 transaction:
`0x8fc60d9892b76ba211c48d8fd01d5e17db5195569fb19c6d570f83943d6d3d4b`

The spike stops after two pages. This does not establish that
the wallet has only two transactions.

## Authentication troubleshooting

- Initial requests sent the Key ID instead of the secret API key value.
- Authenticated native balance and chain metadata requests returned HTTP 500.
- The mainnet chain metadata endpoint returned HTTP 200 without the key header.
- After supplying the secret value of a newly created key,
  authenticated mainnet chain metadata returned HTTP 200.
- The Fuji Go spike then retrieved native balance and transaction history
  successfully.
- An HTTP 500 alone must not be treated as proof that Fuji is unsupported.

## Quota and endpoint weights

Verified against the official rate-limit documentation on 2026-10-28.

### Published limits

| Tier | CU/minute | CU/day |
|---|---:|---:|
| Unauthenticated | 6,000 | 1,200,000 |
| Free | 8,000 | 2,000,000 |

### Account quota verification

- Checked the account dashboard on 2026-10-31.
- Account tier, account-specific CU limits, and current CU usage
  were not visible in the inspected interface.
- These account values remain unverified.
- Published limits above are reference values recorded on
  2026-10-28, not confirmed limits for this account.
- The 60 CU smoke-test estimate is calculated from published
  endpoint weights; it is not measured account usage.
- Pagination and retry limits bound attempts per operation;
  they do not enforce an account-wide daily CU budget.

### Endpoint weights

All paths below are relative to `/v1/chains/{chainId}`.

| Endpoint | Weight | CU/request |
|---|---|---:|
| /addresses/{address}/balances:getNative | Medium | 20 |
| /addresses/{address}/balances:listErc20 | Medium | 20 |
| /addresses/{address}/transactions | Medium | 20 |

The successful spike made three application-level requests:
one native balance request and two transaction page requests.

Estimated cost using published weights: 3 × 20 = 60 CU.
This is a local calculation, not a measured account usage value.
Each additional history page requires another request.

Reference:
https://build.avax.network/docs/api-reference/data-api/rate-limits

## Evidence limitations

- `Observed at` is the local timestamp printed by the spike,
  not a block timestamp supplied by the balance API.
- The spike does not print a block number for the native balance.
- ERC-20 balances were not tested in this run.
- No RPC fallback was exercised in this run.

## Client verification

- Base URL is stored on the client and defaults to
  `https://data-api.avax.network`.
- `.env.example` declares `AVALANCHE_DATA_API_KEY` without a secret value.
- Default HTTP client timeout: 5 seconds per HTTP attempt.
- A caller-supplied HTTP client can override this timeout.
- Retries can extend total duration; callers should supply a deadline.
- Requests use context and check HTTP response status.
- Native/history and legacy ERC-20 response bodies are limited to 1 MiB.
- The AI-facing ListTokenBalances response body is limited to 2 MiB.
- Local Data API tests passed for valid responses, authentication,
  pagination, input/response validation, provenance, HTTP errors,
  malformed JSON, bounded retry, and cancellation.
- Tests use local HTTP servers or fake transports, not live API calls.

Command, run from `backend/`:

    go test ./internal/avalanche -run '^TestDataAPI' -count=1 -v

### RPC test verification

- Changed TestInitClient to use a local JSON-RPC test server.
- The Avalanche package tests passed with live testing disabled:

      AVALANCHE_LIVE_TEST=0 go test ./internal/avalanche -count=1

- The separate TestFujiLiveSnapshot passed against the real Fuji RPC:

      AVALANCHE_LIVE_TEST=1 go test ./internal/avalanche -run '^TestFujiLiveSnapshot$' -count=1 -v

- The earlier timeout was not reproduced in the successful live run.
  This does not guarantee future RPC availability.
- Both checks only read data; no transaction was submitted.

## ERC-20 balances and bounded history

- Added ListERC20Balances with a page size limit of 1–100.
- Invalid wallet addresses and page sizes are rejected before HTTP requests.
- ERC-20 response validation checks chain ID, token address,
  decimals (0–255), and integer balance within uint256 bounds.
- Local tests passed for valid/empty responses, cursor handling,
  invalid input, and malformed balance metadata.
- Live ERC-20 Data API verification remains pending.

### History limits

- ReadTransactionHistory reads at most two pages and returns
  at most 100 transactions per call, requesting up to 50 per page.
- ListTransactions uses doGET with configured retries.
- The default is two retries: at most three HTTP attempts per page.
- A two-page history read therefore permits at most six attempts
  with the default configuration, subject to context and local budget.
- Stops early when no continuation cursor remains.
- Returns Truncated=true and NextPageToken when more pages remain.
- Rejects repeated cursors.
- Local tests passed for end of history, record limit,
  page limit below 100 records, and repeated cursors.
- No live requests were made for these new tests.

## Data API provenance

- Added SourceType and Mode to Provenance.
- Native balance, ERC-20 balance pages, and bounded transaction
  history now include Data API provenance.
- Source: Avalanche Data API; source_type: data_api; mode: live.
- BlockNumber remains nil when no result-level block is available.
- ObservedAt records the observation time, not a block timestamp.
- For bounded history, ObservedAt records the last successful page.
- Local tests passed for provenance and ERC-20 response validation.
- The Avalanche package tests passed with live tests disabled:

      AVALANCHE_LIVE_TEST=0 go test ./internal/avalanche -count=1

- These tests do not establish live API availability.

## RPC snapshot loading

- Added LoadRPCSnapshot to read saved Fuji RPC data without network calls.
- Validates chain ID, required block and observation time,
  token/owner/spender addresses, decimals, and uint256 raw amounts.
- Preserves the saved block number and observation time.
- Sets provenance source_type=rpc, mode=snapshot, and is_demo=true
  for this demo snapshot flow.
- Local tests passed for the saved fixture, invalid data,
  and snapshot provenance.
- Loading the file does not reverify its independent_check evidence.
- Snapshot loading is not yet wired into the server or an automatic fallback.

## Bounded read retry

Two HTTP helpers currently exist.

### doGET in data_api_http.go

Used by GetNativeBalance, ListTransactions and ListTokenBalances.

- GET requests only.
- Default MaxRetries=2: at most three HTTP attempts per operation/page.
- Retries HTTP 429, 500, 502 and 503.
- Transport errors are also retried within the configured bound.
- Retry delay uses configured exponential backoff.
- This helper does not currently interpret Retry-After.
- Response decoding failures are not retried.
- Each HTTP attempt consumes the client instance's local request budget.
- Caller cancellation/deadline bounds the operation.

### doRead in glacier.go

Used by the separate legacy ListERC20Balances method.

- At most two attempts.
- Retries HTTP 429 once.
- Without Retry-After, waits 250 ms.
- Retry-After: 0 permits an immediate retry.
- Other non-empty Retry-After values return the response without retry.
- Other HTTP statuses and transport errors are not retried.

### Scope

- These helpers do not submit transactions.
- The two helpers currently have different policies.
- Do not describe all Data API methods as having one shared retry policy.
- Limits count HTTP attempts, not account CU consumption.

## Saved Data API response fixtures

### Native balance

- File: backend/internal/avalanche/testdata/fuji_native_balance.json
- Response chain ID: 43113.
- Asset: AVAX, decimals: 18.
- Raw balance: 99999999903593280 Wei.
- The file contains no API key.
- Capture time, HTTP status, request headers, and balance block
  are not recorded in this fixture.

### Transaction history

- File: backend/internal/avalanche/testdata/fuji_transactions.json
- Contains the DemoToken deployment transaction:
  0xc522af1f87362c123dec3df453e154970d429357e008a46a2734e2c26ede81d3
- Transaction block: 60107076.
- Includes a continuation cursor; this is not complete history.
- The file contains no API key.
- Capture time, HTTP status, and full request parameters
  are not recorded in this fixture.

### Evidence scope

- These fixtures are separate from the documented
  2026-10-28 smoke-test run.
- Endpoint paths are documented under Verified endpoints.
- Exact original requests cannot be reconstructed from
  response bodies alone.
- RPC snapshots are separate evidence and must not be
  presented as Data API responses or current live data.

## Local request budget

- Each NewDataAPIClient instance allows 100 application-level
  HTTP attempts by default.
- Attempts are counted before handing a request to the HTTP client.
- Retries and transport failures consume attempts.
- Requests rejected before sending do not consume attempts.
- Once exhausted, the client rejects further attempts locally.
- ReadUsage reports attempts and the limit for that instance.
- Counter updates are protected by a mutex.
- Local tests passed for exhaustion, blocked requests,
  and retry accounting.

### Scope

- This is an instance-level request counter, not measured CU usage.
- It is not an account-wide or daily quota.
- Creating a new client resets the counter.
- Multiple instances or processes have separate budgets.
- Account tier and actual account usage remain unverified.

## Live capture on 2026-10-31

- Native balance request returned HTTP 500.
- Capture started at 2026-10-31T08:26:48Z.
- Redacted request details, response body, HTTP status, and
  capture time are saved under docs/evidence/.
- This failed attempt does not establish unsupported Fuji indexing.
- No retry or RPC fallback was performed.
- Current live availability remains unconfirmed.

## Balance verification — 2026-11-08

### Native AVAX

Wallet:
0x4DFb0d91C07A7C706F65F974953cE8b111D27Ee5

| Source | Observation time, UTC | Raw balance |
|---|---|---|
| Fuji RPC | 2026-11-08T04:28:35Z | 99999999831919520 |
| Data API | 2026-11-08T04:32:31Z | 99999999831919520 |

- Decimals: 18.
- Formatted amount: 0.09999999983191952 AVAX.
- Raw balances matched.
- The reads were taken at different times.
- Same-block equality was not verified.
- Observation timestamps were recorded after displaying the responses.

### DemoToken RPC

- Token: 0xcCCb83Fbe349fAebDE74Ce633D25B282E6FDd21A.
- Observation time: 2026-11-08T04:35:00Z.
- balanceOf returned a valid 32-byte ABI word representing zero.
- decimals returned 18.
- Calls used latest; no fixed result block was recorded.

### ERC-20 Data API failure

Request:
GET /v1/chains/43113/addresses/{wallet}/balances:listErc20

Parameters:
- pageSize=10
- filterSpamTokens=true

Results:
- HTTP 500 at 2026-11-08T04:38:55Z.
- HTTP 500 on one manual retry at 2026-11-08T04:40:05Z.
- Both bodies reported Internal server error.
- Failure cause remains undetermined.
- No further live retry was performed in this verification.
- This is not a successful empty-token response.
- Live ERC-20 RPC/Data API comparison remains incomplete.
- This does not establish that Fuji ERC-20 indexing is unsupported.
- No automatic RPC fallback was exercised.

### AI-facing token adapter

ListTokenBalances is separate from legacy ListERC20Balances.

- Accepts page sizes 1–500.
- Enables spam filtering.
- Returns one page and an optional continuation cursor.
- Rejects a response containing more entries than requested.
- Rejects missing/null decimals instead of assuming zero.
- Accepts an explicit decimals value of zero.
- Validates decimal-digit raw amounts within uint256 bounds.
- Uses exact formatting without float64.
- Unknown token reputation is preserved, not treated as verified.
- Provenance includes source_type=data_api and mode=live.
- Does not invent a block number.

### Saved evidence

Directory: backend/internal/avalanche/testdata/

- balances_rpc_native.json
- balances_data_api_native.json
- balances_rpc_demotoken.json
- balances_data_api_erc20_http500.json
- balances_data_api_erc20_retry_http500.json
- balances_verification.json

These files contain historical observations, not fresh runtime balances.
Error fixtures must not be consumed as successful balance fixtures.
API keys and authentication headers must not be committed.

### Local verification commands

Run from backend:

```bash
AVALANCHE_LIVE_TEST=0 go test -count=1 ./...
```

Focused balance tests:

```bash
go test -v -count=1 ./internal/avalanche -run '^(TestGetERC20Balance|TestGetERC20Decimals|TestERC20ContextErrors|TestListTokenBalances)'
```

Local adapter tests use mocks or HTTP test servers.
They do not prove current Data API availability.