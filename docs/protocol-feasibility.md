# Fuji data-source feasibility

## Current evidence

- Fuji chain ID: 43113.
- Data API native balance and transaction pagination succeeded
  in the documented smoke test.
- Live Data API ERC-20 balance verification remains pending.
- RPC native balance, DemoToken balance, allowance, and decimals
  were read in the saved RPC snapshot.
- Historical success does not guarantee current availability.

## Proposed RPC fallback allowlist

This is a design policy, not an implemented automatic fallback.

| Read operation | Allowed RPC fallback | Scope |
|---|---|---|
| Native balance | eth_getBalance | One validated wallet |
| ERC-20 balance | eth_call: balanceOf | Explicitly configured token addresses only |
| ERC-20 decimals | eth_call: decimals | Explicitly configured token addresses only |
| ERC-20 allowance | eth_call: allowance | Validated owner/spender and configured token |
| Indexed token discovery | None | Do not claim a complete token list |
| Address transaction history | None | Do not scan blocks as an automatic replacement |

Initial token allowlist:
- DemoToken: 0xcCCb83Fbe349fAebDE74Ce633D25B282E6FDd21A

A configured-token RPC balance is not equivalent to the Data API
ERC-20 token list. Report its limited coverage explicitly.

## Fallback conditions

- Use RPC only when indexed data is explicitly unavailable for
  the requested chain or operation, with the reason recorded.
- HTTP 401 is an authentication error, not evidence of an
  unsupported chain.
- HTTP 429 is a rate-limit response; apply bounded retry and
  report the error if exhausted.
- HTTP 500 alone does not establish that Fuji is unsupported.
- Empty balances or history alone do not prove indexing failure.
- Do not silently convert errors into zero balances or empty history.
- Automatic fallback for these cases is not currently implemented.

## RPC execution requirements

- Use the configured Fuji RPC endpoint and verify chain ID 43113.
- Validate addresses before requests.
- Pin related balance/allowance reads to one selected block.
- Apply context deadlines and bounded calls; do not scan indefinitely.
- This fallback policy permits reads only, not transaction submission.

## Provenance

- Successful network RPC reads: source_type=rpc, mode=live.
- Saved-file reads: source_type=rpc, mode=snapshot.
- Preserve the snapshot's original block and observation time.
- Never label a saved snapshot as current live data.
- Set is_demo according to the data's purpose, independently of mode.
- Do not invent a block number for a Data API balance.

## Remaining work

- Implement and test source selection before claiming automatic fallback.
- Local per-client request accounting and a 100-attempt limit
  are implemented and tested.
- Shared account-wide accounting and CU budget enforcement
  remain unimplemented.
- Account tier, account-specific limits, and measured usage
  remain unverified.
- Preserve a redacted request/response capture with HTTP status
  and capture time for future live verification.