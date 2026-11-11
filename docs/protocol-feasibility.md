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

## Aave position and oracle reads — roadmap 2026-11-05

### Verified source

- Network: Avalanche Fuji, chain ID 43113.
- Block: 60724294.
- Observed at: 2026-11-08T09:23:31.121Z.
- Wallet: 0x4DFb0d91C07A7C706F65F974953cE8b111D27Ee5.
- Addresses Provider: 0x07D04EfAAA0Ac69D19d107795aF247C42Eb50F1C.
- Pool: 0x8B9b2AF4afB389b4a70A474dfD4AdCD4a302bb40.
- Data Provider: 0xC65cbd1e309Bf0e841Ee6f6E786480598e6a4014.
- Oracle: 0xd36338d0F231446b36008310f1DE0812784ADeBC.
- USDC: 0x5425890298aed601595a70AB815c96711a31Bc65.
- Price source: 0x97FE42a7E96640D932bbc0e1580c73E705A8EB73.

Pool and oracle were resolved through the Addresses Provider.
The Data Provider's ADDRESSES_PROVIDER matched the selected market.
All state reads used the same block.

### Read methods

- Addresses Provider: getPool(), getPriceOracle().
- Pool: getUserAccountData(address).
- Data Provider: ADDRESSES_PROVIDER(),
  getReserveConfigurationData(address),
  getUserReserveData(address,address).
- Oracle: BASE_CURRENCY(), BASE_CURRENCY_UNIT(),
  getAssetPrice(address), getSourceOfAsset(address).
- Price source: decimals(), latestRoundData().

Minimal read ABIs are in:
contracts/scripts/check-aave-position.mjs.

### Units and observations

- USDC token decimals: 6.
- Oracle base currency address: zero address, representing USD.
- Base currency unit: 100000000.
- USDC price: 99975000 raw, equivalent to 0.99975 USD.
- Source decimals: 8; source answer matched oracle price.
- Reserve LTV: 8250 bps.
- Reserve liquidation threshold: 8600 bps.
- Reserve active, not frozen; collateral and borrowing enabled.
- Wallet supplied USDC, stable debt and variable debt were all zero.
- Wallet USDC collateral flag was false.
- Aggregate account collateral and debt were both zero.
- Account health factor returned uint256 maximum for no debt.

Account collateral/debt use oracle base-currency units.
Per-reserve balances use underlying token units.
Account liquidation threshold is distinct from reserve threshold.
Do not display the no-debt health-factor sentinel as a normal value.

### Oracle freshness

- Block timestamp: 1794129792.
- Source updatedAt: 1794096753.
- Source age at the selected block: 33039 seconds.
- answeredInRound equalled roundId.
- Feed heartbeat and acceptable age remain unverified.
- A positive price and valid round identifiers do not establish freshness.
- RPC observation time must not replace source updatedAt.
- Do not present freshness as verified or use these observations
  to claim a validated liquidation-price calculation.

### Evidence and scope

- Fixture:
  backend/internal/avalanche/testdata/position_fuji_oracle.json.
- Reproduction: contracts/scripts/check-aave-position.mjs.
- Reads require no private key and submit no transactions.
- The sample verifies an empty position, not a borrowed position.
- No demo position contract is currently required solely because
  the tested wallet has zero collateral or debt.
- Multi-asset liquidation modelling, eMode and isolation-mode
  handling have not been validated.

## Vault preparation — roadmap 2026-11-05

- Contract: contracts/contracts/DemoVault.sol.
- Base implementation: OpenZeppelin ERC4626.
- ABI: contracts/abi/DemoVault.json.
- Intended underlying: the team's DemoToken on Fuji.
- Status: compiled; ABI exported; not deployed.
- Existing contract suite: 92 passing.
- Dedicated DemoVault behavior tests remain pending.

The vault holds its underlying token directly.
No yield strategy, yield history or APY calculation is implemented.
APY must remain unknown.

Read methods prepared for the next integration:
asset(), decimals(), totalAssets(), totalSupply(), balanceOf(address),
convertToAssets(uint256), maxWithdraw(address) and maxRedeem(address).

Shares and underlying assets must retain their respective decimals.
convertToAssets is a conversion estimate, not a withdrawal limit.
Read maxWithdraw/maxRedeem separately when reporting availability.

No vault address is recorded because deployment has not occurred.
The vault is not yet connected to a Go reader, AI tool or
AllowanceManager adapter.

## Vault integration design — roadmap 2026-11-06

- Implementation: DemoVault, based on OpenZeppelin ERC4626.
- Selected underlying: DemoToken (DDEMO) on Avalanche Fuji.
- Underlying address:
  0xcCCb83Fbe349fAebDE74Ce633D25B282E6FDd21A.
- Vault shares: DeFi Copilot Demo Vault Share (dvDDEMO).
- Deployment remains pending; no vault address is assigned yet.

### Ownership and execution

- AllowanceManager.executeDeposit fixes the share recipient to msg.sender.
- VaultAdapter must accept calls only from its configured manager.
- The adapter must validate underlying, deadline and minimum shares.
- Shares must be deposited directly to the specified recipient.
- Check actual recipient share balance changes, not preview alone.
- Temporary approvals must be cleared after successful execution.

### Read semantics

- Read underlying and share decimals separately.
- previewDeposit estimates shares with rounding; it is not a guarantee.
- maxDeposit is a vault limit, not wallet balance or allowance.
- convertToAssets does not establish immediate withdrawal availability.
- Read maxWithdraw/maxRedeem separately.
- Donation and rounding behavior require local tests.

### Yield and valuation

- No yield strategy or measured yield history is implemented.
- APY remains null.
- No verified DDEMO price source is configured; USD TVL remains null.
- Label the integration as demo even when using live Fuji RPC.

### Vault deployment update — 2026-11-11

The earlier pending-deployment status is superseded by this update.

- DemoVault: 0x896C05deae3d6e35021B12DD361401736Bb8aAda.
- Vault deployment block: 60884688.
- AllowanceManager: 0xA7c9881eb43902Ca668bb864668eAA3795f2283B.
- Manager deployment block: 60886568.
- VaultAdapter: 0x78b5aADB9a8570C7B4387F29119201AF53008b7D.
- Adapter deployment block: 60886681.
- Underlying and shares enabled; deposit route configured and verified.
- ABI exported for manager, vault and adapter.
- Go vault reader implemented and mock-tested.
- Contract suite: 100 passing, including eight vault integration tests.
- Dedicated local vault behavior tests are now complete for that scope.
- No live Fuji deposit/redeem smoke test has been performed.
- No yield strategy or verified DDEMO price source was added.
- APY and USD TVL remain unknown.
- See docs/chain-interface.md and contracts/deployments/fuji.json.