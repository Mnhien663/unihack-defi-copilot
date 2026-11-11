# defi-copilot-team1vn

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