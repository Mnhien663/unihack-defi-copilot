# Native balance request evidence

- Capture started at: 2026-10-31T08:26:48Z
- Method: GET
- URL: https://data-api.avax.network/v1/chains/43113/addresses/0x4DFb0d91C07A7C706F65F974953cE8b111D27Ee5/balances:getNative
- Request headers:
  - Accept: application/json
  - x-glacier-api-key: [REDACTED]
- curl timeout: 15 seconds.
- One curl invocation; no retry configured.
- Actual HTTP status: 500.
- Response body: data-api-native-response.json
- Status file: data-api-native-http-status.txt
- Capture time file: data-api-native-observed-at.txt

## Interpretation

- This attempt failed with an internal server error.
- It does not establish whether the key is valid or whether
  Fuji indexing is unavailable.
- No RPC fallback or transaction submission was performed.
- Earlier successful smoke tests are separate historical evidence.
- This curl request is outside the Go client's local request counter.