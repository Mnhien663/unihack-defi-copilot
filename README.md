# defi-copilot-team1vn

DeFi Copilot là trợ lý Việt–Anh cho Avalanche Fuji, giúp người dùng đọc số dư, lịch sử giao dịch và vị thế DeFi từ dữ liệu có nguồn. Với thao tác ghi, AI chỉ đề xuất giao dịch; backend kiểm tra điều kiện, tạo pending action và xác minh lại trước khi xác nhận. Người dùng ký bằng ví frontend, backend đối chiếu transaction và receipt trước khi ghi nhận mined.

## DEX quote trên Avalanche Fuji

`get_swap_quote` đọc báo giá **exact-input**: đưa vào một lượng token xác định và nhận lượng đầu ra dự kiến. Tool gọi router **LFJ Liquidity Book V2.2** qua Avalanche RPC trên Fuji C-Chain (`43113`). Tool chỉ đọc dữ liệu; không approve, ký hoặc gửi giao dịch.

Hiện hỗ trợ hai chiều **USDT ↔ USDC** qua một pool đã cấu hình. Địa chỉ token, pool và router do backend cấu hình, không lấy từ LLM. Tool không tự tìm tuyến nhiều hop. Luồng hiện tại dùng LFJ trực tiếp, không dùng quoter fallback.

### Đọc kết quả

| Trường | Ý nghĩa |
|---|---|
| `token_path`, `hops` | Token đầu vào/đầu ra và pool được dùng. |
| `amount_in`, `amount_out` | Lượng đầu vào và lượng đầu ra dự kiến. Mỗi lượng có `raw`, `formatted` và metadata token. |
| `min_out`, `slippage_bps` | Đầu ra tối thiểu, tính bằng số nguyên: `floor(amount_out.raw × (10000 − slippage_bps) / 10000)`. |
| `fee` | Phí swap theo đơn vị token đầu vào, không phải phí gas. |
| `provenance`, `expires_at` | Nguồn, chain ID, block, thời điểm quan sát và thời điểm hết hạn. |

Backend từ chối amount không dương, cặp cùng token, token ngoài allowlist, decimals không khớp, quote chỉ khớp một phần và quote quá hạn. **Quote không phải dự báo giá hay cam kết thực thi.** Khi quote hết hạn, hãy lấy quote mới.

### Smoke test: AI mock + Fuji RPC thật

Khởi động PostgreSQL và nạp cấu hình database cùng `AVALANCHE_RPC_URL` vào terminal backend trước khi chạy. `go run` không tự nạp `.env`.

Tại thư mục `backend`, chạy trong PowerShell:

```powershell
$env:CGO_ENABLED = "0"
$env:AI_PROVIDER = "mock"
$env:DATA_MODE = "data_api"
go run ./cmd/server
```

Mở terminal khác và gửi request:

```powershell
$quoteArgs = @{
    chain_id      = 43113
    token_in      = "0xAb231A5744C8E6c45481754928cCfFFFD4aa0732"
    token_out     = "0xB6076C93701D6a07266c31066B298AeC6dd65c2d"
    amount_in_raw = "1000000"
    slippage_bps  = 50
} | ConvertTo-Json -Compress

$body = @{
    session_id     = "lfj-smoke-" + [guid]::NewGuid().ToString()
    wallet_address = "0x4DFb0d91C07A7C706F65F974953cE8b111D27Ee5"
    chain_id       = 43113
    locale         = "en"
    message        = "quote-test:" + $quoteArgs
} | ConvertTo-Json -Compress

$response = Invoke-RestMethod `
    -Uri "http://localhost:8080/api/v1/chat" `
    -Method Post `
    -ContentType "application/json; charset=utf-8" `
    -Body $body

$response | ConvertTo-Json -Depth 15
```

Smoke test pass khi tool được gọi là `get_swap_quote`, `result.error` là `null`, nguồn là `LFJ V2.2 via Avalanche RPC`, `provenance.is_demo` là `false`, và `pending_action` là `null`. `quote-test:` là cú pháp kiểm thử của `DemoLLM`; **model là mock nhưng dữ liệu quote đến từ Fuji RPC thật**. Không cần Gemini API key cho smoke test này.

Ví dụ đã quan sát tại block `60843862`: `1 USDT` → expected out `0.9999 USDC`, min out `0.9949 USDC` với `50 bps`, phí `0.0001 USDT`. Đây là kết quả lịch sử; lần chạy mới không cần trả đúng các số này.

### Kiểm thử

Chạy từ thư mục `backend`:

```powershell
go test -v ./internal/tools -run '^TestGetSwapQuoteToolContract$' -count=1
go test -v ./internal/avalanche -run '^TestLFJQuoterExactInput$' -count=1
```

Contract test của tool gồm 5 case offline: quote hai chiều, slippage bằng 0, amount bằng 0 và quote hết hạn. Luồng HTTP với AI mock + Fuji RPC đã xác nhận thành công. Luồng Gemini chọn tool quote chưa xác nhận do request thử nghiệm trả HTTP 504.

## Validation and current limitations

### Verified

- Backend unit tests cover confirmation signatures, wrong-session
  challenges, expired actions, changed quotes, revalidation timeouts,
  transaction calldata mismatch and receipt block identity.
- PostgreSQL integration tests cover concurrent pending-action decisions,
  transaction-hash uniqueness and atomic turn persistence.
- Fuji swap E2E passed on 21 November 2026 at 12:56:30 ICT:
  0.01 USDT → 0.009999 USDC. The app reported mined and the backend
  verified the receipt.
  Transaction:
  `0x3d2b33cae48274f62b4743afeaadd96be5c276250a54cbba827ff7cb35877bdd`
- Vault deposit preparation through chat returned an unsigned pending
  action for 0.1 DDEMO with a minimum of 0.099 dvDDEMO.
- A separate deposit through core-check.html succeeded on Fuji:
  0.1 DDEMO → 0.1 dvDDEMO, block 61414919.
  Transaction:
  `0x00286e361665632943d77c8c6250615ee374e48b622b8b5225651f02608d2f56`

### App-based verification on Avalanche Fuji C-Chain

The following tests ran through DeFi Copilot and Core Wallet on
Avalanche Fuji C-Chain (chain ID 43113).

| Flow | Frontend | Backend | Result |
|---|---|---|---|
| Swap E2E | `32ed58e` | `46f69b4` | Swapped 0.01 LFJ USDT for 0.009999 LFJ USDC at block `61429763`. The app displayed Receipt Verified. |
| Deposit E2E | `f1c4553` | `46f69b4` | Deposited 0.1 DDEMO and received 0.1 dvDDEMO at block `61428583`, with a backend-verified receipt. |

Both flows covered preparation, review confirmation, wallet signing
and submission, transaction-hash recording, and receipt refresh.

Deposit transaction:
`0xd026371430225e5f2da59e4b332b51b7dc40aa58bb0683738939052a46ce9b48`

These app-based tests are separate from the earlier deposit through
`core-check.html`.

On frontend `50aecee`, `npm run build` passed and Deposit Review
displayed the DDEMO symbol, contract address, and 18 decimals.
That check stopped at Review and did not submit another transaction.

The integrated frontend includes the Asset Token display correction
and the Swap receipt-status display fix. The E2E results above apply
to the exact versions listed, rather than constituting a new E2E
test of every subsequent UI change.

### Evaluation

The latest reviewed bilingual run contains 30 cases:
28 HTTP 200 responses and 2 skipped upstream-fault cases.
HTTP success is not a semantic pass score.
See `eval/eval_report.md` for captured responses and manual findings.

### Remaining limitations

- The incorrect Vietnamese liquidation formula observed during evaluation
  was addressed with a deterministic empty-position response.
  Vietnamese and English runtime checks passed on 21 November 2026
  at 16:11 ICT. Liquidation calculations for funded borrowing positions
  remain unverified.
## App-based verification on Avalanche Fuji

Network: Avalanche Fuji C-Chain, chain ID 43113.

### Swap E2E

- Frontend: `32ed58e`.
- Backend: `46f69b4`.
- Completed Prepare, Confirm, Core Wallet signing and submission,
  Submit Hash, and Refresh Receipt.
- Swapped 0.01 LFJ USDT for 0.009999 LFJ USDC.
- Block: `61429763`.
- The app displayed Receipt Verified.

### Deposit E2E

- Frontend: `f1c4553`, including the Deposit fix `9f62624`.
- Backend: `46f69b4`.
- Completed Prepare, Confirm Review, Core Wallet signing and
  submission, Submit Hash, and Refresh Receipt.
- Deposited 0.1 DDEMO with a minimum of 0.099 dvDDEMO.
- Received 0.1 dvDDEMO.
- Block: `61428583`.
- Backend verified the receipt.
- Transaction:
  `0xd026371430225e5f2da59e4b332b51b7dc40aa58bb0683738939052a46ce9b48`

This app-based deposit is separate from the earlier test through
core-check.html.

### Subsequent UI checks

On frontend `50aecee`, npm run build passed. Deposit Review displayed
the DDEMO symbol, contract address, and 18 decimals. This check stopped
at Review without submitting another transaction.

Integrated frontend `37ef72d` includes the Asset Token display fix
and Swap receipt-status display corrections. The E2E evidence above
applies to the versions explicitly listed.
## Demo runbook

See [Fuji demo runbook](docs/runbook.md) for setup and demo instructions.
Commands were checked against Compose and Vite.
Docker verification from a clean clone is still pending.
