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