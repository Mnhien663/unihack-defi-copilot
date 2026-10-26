# Kế hoạch chạy ứng dụng

## Kiến trúc dự kiến
- React gọi API của Go backend.
- Go backend gọi Gemini hoặc OpenAI API.
- Go backend sử dụng PostgreSQL và Avalanche RPC.

## Đã kiểm tra
- PostgreSQL chạy bằng Docker Compose.
- Go backend build và chạy bằng Docker Compose.
- Endpoint /health trả HTTP 200.
- Backend kết nối và xác minh Fuji chain ID 43113.

## Chờ thống nhất với Dev 1
- Provider AI chính.
- Model sử dụng.
- Tên biến môi trường mà API client thực sự đọc.
- Giữ AI_API_KEY hay chuyển sang biến riêng theo provider.
- Cách kiểm tra kết nối API và giới hạn sử dụng.

## Quản lý cấu hình
- .env.example chỉ chứa cấu hình mẫu, không có key thật.
- Key thật được truyền vào backend ở runtime.
- Không đưa key vào frontend hoặc biến VITE_*.
- Không đưa key vào Dockerfile, image hoặc log.
- Cần kiểm tra Docker ignore theo build context ./backend.

## Chưa kiểm tra
- Kết nối AI API từ backend.
- Kết nối PostgreSQL từ code backend.
- Khởi động và sử dụng frontend.