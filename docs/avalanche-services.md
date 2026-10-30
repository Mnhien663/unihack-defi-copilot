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

Account tier, account-specific limits and current usage remain
unverified. Published Free limits are reference values, not
confirmed limits for this account.

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
- HTTP client timeout: 10 seconds.
- Requests use context and check HTTP response status.
- Response bodies are limited to 1 MiB.
- Six Data API tests passed, including authentication header
  verification using a local HTTP test server and a fake key.

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

- ReadTransactionHistory makes at most two requests and returns
  at most 100 transactions per call, requesting up to 50 per page.
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