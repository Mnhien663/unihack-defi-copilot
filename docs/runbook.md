# DeFi Copilot demo runbook

## 1. Yêu cầu

- Git, Docker có Compose, Node.js 24 và npm.
- Core Wallet trên Avalanche Fuji C-Chain (chain ID `43113`) cho demo live.
- DeepSeek API key cho chế độ AI live.
- Checkout đúng commit bản nộp do nhóm chốt.

## 2. Cấu hình môi trường

Từ thư mục gốc của repo, tạo `.env` từ file mẫu nếu chưa có:

```bash
test -f .env || cp .env.example .env
```

Đặt `DB_PASSWORD` trong `.env`. File mẫu mặc định dùng mock AI và mock data để kiểm tra khởi động. Để chạy demo live trên Fuji, cấu hình:

```dotenv
AI_PROVIDER=openai
OPENAI_API_KEY=<your DeepSeek API key>
OPENAI_BASE_URL=https://api.deepseek.com
OPENAI_MODEL=deepseek-flash
DATA_MODE=data_api
AVALANCHE_CHAIN_ID=43113
ENABLE_PREPARE_SWAP=1
ENABLE_SWAP_DECISIONS=1
ENABLE_PREPARE_DEPOSIT=1
PREPARE_DEPOSIT_MAX_ASSETS_RAW=100000000000000000
```

Dùng Fuji RPC URL trong `.env.example`. Khi bật transaction decisions, điền `SWAP_DECISION_SIGNING_DOMAIN` theo cấu hình demo của nhóm. Điền `AVALANCHE_DATA_API_KEY` nếu cấu hình Data API của bạn yêu cầu. Có thể để trống `SNOWTRACE_API_KEY` khi chỉ mở link explorer trong trình duyệt. Giữ API key trong `.env` local, ngoài Git và khung quay.

## 3. Khởi động database và backend

Chạy từ thư mục gốc:

```bash
docker compose config --quiet
docker compose up -d db
docker compose --profile tools run --rm migrate
docker compose up -d --build backend
curl -fsS http://localhost:8080/health
```

Nếu backend vừa được tạo, đợi service sẵn sàng rồi thử lại `/health`. Khi có lỗi, xem trạng thái và log:

```bash
docker compose ps
docker compose logs --tail 100 backend
```

## 4. Khởi động frontend

Mở terminal thứ hai ở thư mục gốc. Vite dùng proxy trong repo để chuyển các request `/api` đến backend ở cổng `8080`.

```bash
cd frontend
npm ci
npm run dev -- --host localhost --port 3000 --strictPort
```

Mở http://localhost:3000. Nếu cổng đã có tiến trình sử dụng, kiểm tra instance đang chạy trước khi mở thêm.

## 5. Kiểm tra demo trên Fuji

### Đọc số dư

Kết nối Core Wallet trên Fuji và hỏi số dư của ví đang kết nối. Đối chiếu địa chỉ ví và mạng với phản hồi của app.

### Swap

1. Kiểm tra số dư token đầu vào, ERC-20 allowance tới Manager và effective budget.
2. Yêu cầu quote mới. Kiểm tra số lượng gửi, minimum output và deadline trong Transaction Review.
3. Confirm, ký và gửi giao dịch qua Core. Sau đó bấm Submit Hash rồi Refresh Receipt.
4. Đối chiếu Receipt Verified trong app với status, block và số token nhận trên Fuji explorer.

### Deposit Review

1. Kiểm tra số dư DDEMO, allowance tới Manager và effective budget.
2. Chuẩn bị pending action mới. Kiểm tra symbol DDEMO, địa chỉ token, 18 decimals, preview shares, minimum shares và deadline.
3. Nếu chỉ kiểm tra hiển thị, bấm Đóng review trước Confirm Review.

## 6. Bằng chứng kiểm thử

`/health` xác nhận backend đã khởi động. Xem [kết quả kiểm thử qua app](../README.md#app-based-verification-on-avalanche-fuji-c-chain) và [bằng chứng Fuji](evidence.md) để đối chiếu các phiên bản và giao dịch đã kiểm thử.