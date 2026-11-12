## Fuji transaction receipt verification — 2026-11-08

### Sources

- Network: Avalanche Fuji C-Chain, chain ID 43113.
- RPC: https://api.avax-test.network/ext/bc/C/rpc
- Explorer: https://testnet.snowtrace.io/tx/0x3b61008d6400772ce94848239c431d7dae62fa78360af24a8b334d6ae81b4e72
- RPC observed at: 2026-11-08T06:20:03Z.
- Raw fixture: ../backend/internal/avalanche/testdata/receipt_fuji_rpc.json

### Comparison

| Field | RPC | Snowtrace Fuji |
|---|---|---|
| Chain ID | 43113 | Fuji (43113) |
| Transaction hash | 0x3b61008d6400772ce94848239c431d7dae62fa78360af24a8b334d6ae81b4e72 | Same |
| Block number | 60111917 | 60111917 |
| Status | 0x1 | Success |
| From | 0x4DFb0d91C07A7C706F65F974953cE8b111D27Ee5 | Same |
| To | 0x8B9b2AF4afB389b4a70A474dfD4AdCD4a302bb40 | Same |
| Native value | 0 wei | 0 AVAX |
| Log count | 6 | 6 |

### Scope and limitations

- RPC transaction and receipt match the captured Data API history
  transaction hash, block number and block hash.
- The explorer screenshot confirms the fields listed above.
- Native value excludes transaction fees and ERC-20 transfers.
- Receipt status indicates execution success, not application-level
  correctness.
- This comparison validates captured source data. It does not establish
  that the Go receipt reader has been exercised against live RPC.
- The RPC request requires no API key. No private keys or API keys
  are included in this evidence.

### History pagination observations

- First attempt: HTTP 500 at 2026-11-08T05:34:56Z.
- Retry/page 1: HTTP 200 at 2026-11-08T05:36:37Z.
- Page 2: HTTP 200 at 2026-11-08T05:37:37Z.
- Two records per page; four distinct transaction hashes.
- Page 2 returned a different continuation cursor, so the captured
  pages do not represent the complete wallet history.
- One transaction involves the wallet through an ERC-20 transfer,
  although its native transaction from/to do not match the wallet.
- No RPC history fallback was added because the retry succeeded.

## Day 08 — Fuji Manager swap verified

- Network: Avalanche Fuji C-Chain, chain ID 43113.
- Owner/recipient: `0x37E146a4141C8fd82f0c8c5Bb8035930fD0ea588`.
- Manager: `0xA7c9881eb43902Ca668bb864668eAA3795f2283B`.
- Adapter: `0xD5cae0dF0f11a0Cf9818aCc47b11ce183b0E8AF5`.
- Input token: LFJ USDT, `0xAb231A5744C8E6c45481754928cCfFFFD4aa0732`.
- Output token: LFJ USDC, `0xB6076C93701D6a07266c31066B298AeC6dd65c2d`.
- Input: 10000 raw (0.01 USDT).
- Minimum output: 9899 raw (0.009899 USDC).
- Actual output: 9999 raw (0.009999 USDC).
- Native transaction value: 0 AVAX; gas paid separately.
- Transaction: `0xcb6292135bf452bd6669b21cc9fb8dac6bbd4b74854f1502e1cad0724232f0f0`.
- Mined block: 60946687.
- Explorer: https://testnet.snowtrace.io/tx/0xcb6292135bf452bd6669b21cc9fb8dac6bbd4b74854f1502e1cad0724232f0f0

Observed wallet state before and after execution:

| Field | Before | After |
|---|---:|---:|
| USDT balance raw | 14164 | 4164 |
| USDC balance raw | 0 | 9999 |
| USDT allowance to Manager raw | 10000 | 0 |
| Effective USDT budget raw | 10000 | 0 |

Verification:
- TypeScript read-only checker matched transaction fields and the
  Manager's SwapExecuted event against the saved expected intent.
- Backend TestFujiLiveVerifyExecutedSwap passed using live RPC.
- VerifyTx returned status succeeded, verified true and amount_out_raw 9999.
- Intent fixture: backend/internal/avalanche/testdata/swap_fuji_expected_intent.json.

Scope:
- Executed through frontend/core-check.html with explicit Core wallet signing.
- This verifies the wallet harness and deployed Manager/adapter route.
- Full product modal → Confirm API → wallet integration remains pending.
- This does not establish DB-level transaction-hash uniqueness or
  confirm/reject concurrency protection.

## Day 09 — DemoVault deposit and redeem on Fuji

Network: Avalanche Fuji C-Chain, chain ID 43113.
Owner/receiver: `0x37E146a4141C8fd82f0c8c5Bb8035930fD0ea588`.

Contracts:
- DDEMO: `0xcCCb83Fbe349fAebDE74Ce633D25B282E6FDd21A`.
- DemoVault: `0x896C05deae3d6e35021B12DD361401736Bb8aAda`.
- Manager: `0xA7c9881eb43902Ca668bb864668eAA3795f2283B`.
- VaultAdapter: `0x78b5aADB9a8570C7B4387F29119201AF53008b7D`.

Transactions:
- Mint 0.1 DDEMO: `0xb462900f99fb4a66ffa94f0142bc16698abc475f95802b5a7bb6f943a0f9bb0a`, block 60950231.
- Approve 0.1 DDEMO: `0x049d5322ffa2ed60cc4f039ef56acc5a99e43e79a6288c8582ea2bba2bf7693d`, block 60950357.
- Set budget 0.1 DDEMO: `0x8a83ab970a6995763432dbc8e30afa119e280e01c627452cf49cceb5b4c8d32d`, block 60950484.
- Deposit: `0xc82ed0fcba8a995536e2f3bccd1c2f3eb139fe74c008500f26dcdca29649f82d`, block 60950674.
- Redeem: `0xc3e16441e95a7804c7ca8e16d1dd0ed4e797dd894518fdcea6b084be844150af`, block 60951160.

Explorer:
- Deposit: https://testnet.snowtrace.io/tx/0xc82ed0fcba8a995536e2f3bccd1c2f3eb139fe74c008500f26dcdca29649f82d
- Redeem: https://testnet.snowtrace.io/tx/0xc3e16441e95a7804c7ca8e16d1dd0ed4e797dd894518fdcea6b084be844150af

| Wallet state | Before deposit | After deposit | After redeem |
|---|---:|---:|---:|
| DDEMO raw | 100000000000000000 | 0 | 100000000000000000 |
| dvDDEMO raw | 0 | 100000000000000000 | 0 |
| Allowance to Manager raw | 100000000000000000 | 0 | 0 |
| Effective budget raw | 100000000000000000 | 0 | 0 |

Verification:
- Backend TestFujiLiveVerifyExecutedDeposit passed.
- VerifyDepositTx matched transaction fields, Manager DepositExecuted,
  vault Deposit and share mint events.
- Deposit returned verified=true and shares_out_raw=100000000000000000.
- TypeScript checker verified redeem calldata, Withdraw, share burn,
  underlying Transfer and block-end balance changes.
- Redeem was called directly by the owner, with owner as receiver.
- Redeem has no on-chain minimum-assets or deadline parameter.

Scope:
- Wallet signing used frontend/core-check.html, a test harness.
- Full product confirmation/API integration remains separate.
- DemoVault holds DDEMO without a yield strategy; APY remains null.
- No contract redeployment or ABI change was needed.