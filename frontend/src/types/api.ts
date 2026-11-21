import type { PendingAction } from '../components/Transaction/TransactionCard';

export interface ChatRequest {
  session_id: string;
  wallet_address: string;
  chain_id: number;
  locale: string;
  message: string;
}

export interface ChatApiError {
  code: string;
  message: string;
}

export interface ChatSource {
  name: string;
  chain_id?: number | null;
  block_number?: number | null;
  observed_at?: string | null;
}

export interface ChatResponse {
  message: string;
  tool_calls: unknown[];
  pending_action: PendingAction | null;
  sources: ChatSource[];
  request_id: string;
  error: ChatApiError | null;
}