
const STORAGE_KEY = 'deficopilot_pending_swap_v1';

export interface StoredPendingSwap {
  sessionId: string;
  actionId: string;
  reviewHash: string;
  walletAddress: string;
  chainId: 43113;
  transactionHash: `0x${string}`;
  savedAt: number;
}

export function savePendingSwap(
  swap: StoredPendingSwap
): void {
  sessionStorage.setItem(
    STORAGE_KEY,
    JSON.stringify(swap)
  );
}

export function loadPendingSwap():
  StoredPendingSwap | null {
  const saved = sessionStorage.getItem(STORAGE_KEY);

  if (!saved) return null;

  try {
    const data: unknown = JSON.parse(saved);

    if (!data || typeof data !== 'object') {
      return null;
    }

    const swap = data as Record<string, unknown>;

    if (
      typeof swap.sessionId !== 'string' ||
      !swap.sessionId ||
      swap.sessionId.length > 128 ||
      typeof swap.actionId !== 'string' ||
      !/^[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}$/i.test(
        swap.actionId
      ) ||
      typeof swap.reviewHash !== 'string' ||
      !/^[0-9a-f]{64}$/.test(swap.reviewHash) ||
      typeof swap.walletAddress !== 'string' ||
      !/^0x[0-9a-fA-F]{40}$/.test(swap.walletAddress) ||
      swap.chainId !== 43113 ||
      typeof swap.transactionHash !== 'string' ||
      !/^0x[0-9a-fA-F]{64}$/.test(swap.transactionHash) ||
      typeof swap.savedAt !== 'number' ||
      !Number.isFinite(swap.savedAt)
    ) {
      return null;
    }

    return swap as unknown as StoredPendingSwap;
  } catch {
    return null;
  }
}

export function clearPendingSwap(): void {
  sessionStorage.removeItem(STORAGE_KEY);
}

export function hasStoredPendingSwap(): boolean {
  // Không phân tích JSON tại đây.
  // Chỉ cần có bản ghi là phải chặn gửi swap mới.
  return sessionStorage.getItem(
    'deficopilot_pending_swap_v1'
  ) !== null;
}
