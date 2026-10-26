# Avalanche notes

## Cấu hình đã kiểm tra
- Mạng: Avalanche Fuji C-Chain.
- EVM chain ID: 43113, dạng hex là 0xa869.
- RPC: https://api.avax-test.network/ext/bc/C/rpc
- Máy local gọi được eth_chainId và eth_blockNumber.
- Go backend gọi InitClient() và xác minh đúng chain ID.

## Nội dung cần học
- Primary Network là gì?
- X-Chain, P-Chain và C-Chain khác nhau thế nào?
- Fuji C-Chain khác custom Avalanche L1 thế nào?
- RPC khác indexed Data API thế nào?

## Áp dụng vào dự án
- Core demo sử dụng Fuji C-Chain.
- Bonus native Go precompile cần custom VM/L1.