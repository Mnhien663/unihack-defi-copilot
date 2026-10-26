# Phân công Dev 2

## Dev 2 phụ trách
- docker-compose.yml và các cấu hình Docker Compose bổ sung.
- backend/Dockerfile và Docker ignore đúng build context.
- backend/internal/avalanche/: RPC và các chain adapters.
- contracts/: cấu hình, test và deploy smart contract.
- precompile/: phần bonus khi triển khai.
- Hạ tầng kết nối PostgreSQL và migration runner.

## Dev 1 phụ trách
- LLM provider/model và API client.
- Agent, prompt, memory.
- Tool registry, handlers và eval.
- Schema và truy vấn DB nghiệp vụ.

## Dev 3 phụ trách
- React UI.
- Kết nối ví.
- Giao diện xác nhận và ký giao dịch.

## File chung cần phối hợp
- .env.example
- backend/go.mod và backend/go.sum
- backend/cmd/server/main.go
- Makefile
- README.md
- docs/architecture.md

## Trạng thái hiện tại
- DB chạy được trong Docker.
- Backend build và chạy được trong Docker.
- Backend xác minh được Fuji chain ID 43113 khi khởi động.
- contracts/package.json đang trống.
- contracts/hardhat.config.ts đang trống.
- Chưa xác định phiên bản Hardhat và Solidity.

## Nội dung cần thống nhất với Dev 1
- Provider AI chính và model sử dụng.
- Tên biến môi trường cho AI.
- Cách truyền Avalanche client cho các chức năng cần dùng.
- Ranh giới hạ tầng DB và nghiệp vụ DB.