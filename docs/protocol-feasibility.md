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

## Swap integration scope — roadmap 2026-11-04

### Read and execution boundaries

- Quote reads expected output for an exact input amount.
- A quote does not approve tokens, sign or broadcast a transaction.
- Approval grants an explicitly identified spender access to tokens.
- Swap execution requires wallet review and signing.
- The application must display chain, token addresses, input amount,
  minimum output, spender and deadline before signing.
- The LLM must not invent prices, routes or spender addresses.

### Selected scope

- Start with two ERC-20 tokens on Avalanche Fuji, chain ID 43113.
- Native AVAX wrapping and unwrapping are outside the initial scope.
- Recheck the previously investigated LFJ router, quoter and pair
  before implementing the Go quote adapter.
- Historical quote evidence does not establish current liquidity.
- Do not reuse an external quote service until its supported network,
  authentication requirements and usage limits are verified.

### Manager adapter boundary

ISwapAdapter.swap is an execution interface, not a quote interface.

The adapter pulls approved input from its authorized manager and
delivers output to the specified recipient. It must enforce minimum
output and deadline, and restrict calls to the authorized manager.

For direct wallet execution, the approval spender depends on the
selected protocol call path.

For AllowanceManager execution, wallet-to-manager approval and
manager-to-adapter approval are distinct permissions. Protocol-level
approvals inside an adapter depend on its implementation.

No production swap adapter deployment is recorded in fuji.json yet.

### LFJ router quote recheck — 2026-11-08

- Network: Avalanche Fuji, chain ID 43113.
- Observation: 2026-11-08T07:33:09.163Z.
- All pair and quote reads used block 60720761.
- Router: 0x18556DA13313f3532c54711497A8FedAC273220E.
- Pair: 0xc3b7307184381f65B398d385a71EF0490239a2b3.
- Token X (USDT): 0xAb231A5744C8E6c45481754928cCfFFFD4aa0732.
- Token Y (USDC): 0xB6076C93701D6a07266c31066B298AeC6dd65c2d.
- Both tokens returned 6 decimals.
- Reserves: X=3138039978, Y=1960697332 raw units.
- Router getSwapOut returned, in both directions:
  input=1000000, output=999900, inputLeft=0, fee=100 raw units.
- Fee is denominated in the input token.
- Fixture: backend/internal/avalanche/testdata/quote_fuji_router.json.
- Reproduction: contracts/scripts/check-swap-quote.mjs.

Decision: implement the initial Go quote adapter using the
configured router and direct pair. Query fresh output at request
time; do not reuse the captured amounts as fixed prices.

The quoter returned bytecode in a separate check, but its route
selection was not re-executed during this verification.
No demo AMM fallback is currently needed for this quote path.
Wallet swap execution and AllowanceManager integration remain
unverified. No transaction was signed or broadcast.