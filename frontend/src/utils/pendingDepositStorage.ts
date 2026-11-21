// Separate from Swap storage so existing Swap recovery/idempotency stays intact.
const STORAGE_KEY = 'deficopilot_pending_deposit_v1';
const HEX_HASH_RE = /^0x[0-9a-fA-F]{64}$/;
const ADDRESS_RE = /^0x[0-9a-fA-F]{40}$/;
const REVIEW_RE = /^[0-9a-f]{64}$/;

export type StoredPendingDeposit = {
  actionId: string;
  sessionId: string;
  reviewHash: string;
  walletAddress: string;
  chainId: 43113;
  transactionHash: `0x${string}`;
  savedAt: number;
  hashRecorded?: boolean;
  receiptStatus?: string;
};

export function hasStoredPendingDeposit(): boolean {
  return localStorage.getItem(STORAGE_KEY) !== null;
}

export function loadPendingDeposit(): StoredPendingDeposit | null {
  const raw = localStorage.getItem(STORAGE_KEY);
  if (raw === null) return null;
  try {
    const candidate: unknown = JSON.parse(raw);
    if (!candidate || typeof candidate !== 'object') return null;
    const value = candidate as Partial<StoredPendingDeposit>;
    if (
      typeof value.actionId !== 'string' || !value.actionId ||
      typeof value.sessionId !== 'string' || !value.sessionId || value.sessionId.length > 128 ||
      typeof value.reviewHash !== 'string' || !REVIEW_RE.test(value.reviewHash) ||
      typeof value.walletAddress !== 'string' || !ADDRESS_RE.test(value.walletAddress) ||
      value.chainId !== 43113 ||
      typeof value.transactionHash !== 'string' || !HEX_HASH_RE.test(value.transactionHash) ||
      typeof value.savedAt !== 'number' || !Number.isFinite(value.savedAt)
    ) return null;
    return value as StoredPendingDeposit;
  } catch {
    return null;
  }
}

export function savePendingDeposit(pending: StoredPendingDeposit): void {
  // Never overwrite a broadcast tx; manual recovery is safer than sending twice.
  if (hasStoredPendingDeposit()) {
    throw new Error('An earlier Deposit transaction needs receipt verification.');
  }
  localStorage.setItem(STORAGE_KEY, JSON.stringify(pending));
  window.dispatchEvent(new Event('deficopilot:deposit-broadcast'));
}

export function updatePendingDepositReceipt(
  expectedHash: string,
  patch: { hashRecorded?: boolean; receiptStatus?: string },
): void {
  const saved = loadPendingDeposit();
  if (!saved || saved.transactionHash.toLowerCase() !== expectedHash.toLowerCase()) {
    throw new Error('Stored Deposit transaction does not match.');
  }
  const next = { ...saved, ...patch };
  localStorage.setItem(STORAGE_KEY, JSON.stringify(next));
  window.dispatchEvent(new Event('deficopilot:deposit-broadcast'));
}

export function clearPendingDeposit(expectedHash: string): void {
  const saved = loadPendingDeposit();
  if (!saved || saved.transactionHash.toLowerCase() !== expectedHash.toLowerCase()) {
    throw new Error('Stored Deposit transaction does not match.');
  }
  localStorage.removeItem(STORAGE_KEY);
  window.dispatchEvent(new Event('deficopilot:deposit-broadcast'));
}
