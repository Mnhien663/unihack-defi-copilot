
import type { ChatRequest, ChatResponse } from '../types/api';

export async function sendChatMessage(
  request: ChatRequest,
  signal?: AbortSignal
): Promise<ChatResponse> {
  // 1. Gửi request đến Go Backend qua Vite Proxy
  const response = await fetch('/api/v1/chat', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify(request),
    signal,
  });

  // 2. Đọc JSON backend trả về
  let data: ChatResponse;

  try {
    data = (await response.json()) as ChatResponse;
  } catch {
    throw new Error('Backend trả về dữ liệu không hợp lệ.');
  }

  // 3. Kiểm tra lỗi HTTP và lỗi nghiệp vụ
  if (!response.ok || data.error) {
    const code = data.error?.code ?? `HTTP_${response.status}`;
    const message =
      data.error?.message ?? 'Không thể kết nối với Chat API.';

    throw new Error(`${code}: ${message}`);
  }

  // 4. Trả dữ liệu cho Chatbot
  return data;
}
