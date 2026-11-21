import { useEffect, useMemo, useState } from 'react';
import { useConnection, useSignMessage } from 'wagmi';
import {
  executeSignedDecision,
  type PendingDecision,
} from '../../services/pendingActionApi';
import './DepositReviewModal.css';

const FUJI_CHAIN_ID = 43113;
const ADDRESS_RE = /^0x[0-9a-fA-F]{40}$/;
const HASH_RE = /^[0-9a-f]{64}$/;
const RAW_RE = /^(0|[1-9][0-9]*)$/;

type JsonObject = Record<string, unknown>;

export type DepositPendingAction = {
  tool_name: 'prepare_vault_deposit';
  pending_action_id?: string;
  normalized_intent_hash?: string;
  chain_id?: number;
  wallet_address?: string;
  expires_at?: string;
  arguments?: { summary?: unknown };
};

export function isDepositPendingAction(
  value: unknown,
): value is DepositPendingAction {
  return isRecord(value) && value.tool_name === 'prepare_vault_deposit';
}

function isRecord(value: unknown): value is JsonObject {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function readString(source: JsonObject, key: string): string | null {
  const value = source[key];
  return typeof value === 'string' && value.trim() ? value.trim() : null;
}

// Never turn financial raw amounts into JS numbers: precision must be preserved.
function rawAmount(source: JsonObject, ...keys: string[]): string | null {
  for (const key of keys) {
    const candidate = source[key];
    const value = isRecord(candidate) ? candidate.raw : candidate;
    if (typeof value === 'string' && RAW_RE.test(value)) return value;
  }
  return null;
}

function formattedAmount(source: JsonObject, key: string): string | null {
  const item = source[key];
  return isRecord(item) ? readString(item, 'formatted') : null;
}

function readVault(summary: JsonObject): string | null {
  const direct = readString(summary, 'vault_address');
  if (direct) return direct;
  if (typeof summary.vault === 'string') return summary.vault;
  return isRecord(summary.vault) ? readString(summary.vault, 'address') : null;
}

function readDeadlineMs(raw: unknown): number {
  // Backend deadline is Unix seconds. Also accept RFC3339 for defensive display.
  if (typeof raw === 'number' && Number.isSafeInteger(raw) && raw > 0) {
    return raw * 1000;
  }
  if (typeof raw === 'string' && /^\d+$/.test(raw)) {
    const seconds = Number(raw);
    return Number.isSafeInteger(seconds) && seconds > 0 ? seconds * 1000 : NaN;
  }
  return typeof raw === 'string' ? Date.parse(raw) : NaN;
}

function displayTime(ms: number): string {
  return Number.isFinite(ms)
    ? new Date(ms).toLocaleString('vi-VN', {
        timeZone: 'Asia/Ho_Chi_Minh',
      })
    : 'Thiếu / không hợp lệ';
}

type DepositReviewModalProps = {
  isOpen: boolean;
  pendingAction: DepositPendingAction;
  sessionId: string;
  onClose: () => void;
};

export default function DepositReviewModal({
  isOpen,
  pendingAction,
  sessionId,
  onClose,
}: DepositReviewModalProps) {
  const { address, chainId, isConnected } = useConnection();
  const signMessage = useSignMessage();
  const [now, setNow] = useState(Date.now);
  const [busy, setBusy] = useState(false);
  const [decisionStatus, setDecisionStatus] = useState<'confirmed' | 'rejected' | null>(null);
  const [decisionError, setDecisionError] = useState<string | null>(null);

  useEffect(() => {
    if (!isOpen) return;
    setNow(Date.now());
    const timer = window.setInterval(() => setNow(Date.now()), 1000);
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') onClose();
    };
    window.addEventListener('keydown', handleKeyDown);
    return () => {
      window.clearInterval(timer);
      window.removeEventListener('keydown', handleKeyDown);
    };
  }, [isOpen, onClose]);

  // Reset only when a *different* pending action is reviewed, not on close/reopen.
  useEffect(() => {
    setDecisionStatus(null);
    setDecisionError(null);
  }, [pendingAction.pending_action_id]);

  const summary = useMemo<JsonObject>(() =>
    isRecord(pendingAction.arguments?.summary)
      ? pendingAction.arguments.summary
      : {},
  [pendingAction.arguments?.summary]);

  const vault = readVault(summary);
  const assetsRaw = rawAmount(summary, 'assets_raw', 'assets');
  const previewSharesRaw = rawAmount(summary, 'preview_shares_raw', 'preview_shares');
  const minSharesRaw = rawAmount(summary, 'min_shares_raw', 'min_shares');
  const assetToken = isRecord(summary.asset)
    ? readString(summary.asset, 'symbol')
    : readString(summary, 'asset_symbol');
  const deadlineMs = readDeadlineMs(summary.deadline);
  const expiresAtMs = pendingAction.expires_at
    ? Date.parse(pendingAction.expires_at)
    : NaN;

  const sameWallet = Boolean(
    isConnected && address && pendingAction.wallet_address &&
    address.toLowerCase() === pendingAction.wallet_address.toLowerCase(),
  );
  const owner = readString(summary, 'owner_address');
  const ownerMatches = !owner || (ADDRESS_RE.test(owner) &&
    Boolean(address && owner.toLowerCase() === address.toLowerCase()));

  const amountRelationshipValid = Boolean(
    assetsRaw && previewSharesRaw && minSharesRaw &&
    BigInt(assetsRaw!) > 0n &&
    BigInt(previewSharesRaw!) > 0n &&
    BigInt(minSharesRaw!) <= BigInt(previewSharesRaw!),
  );

  const isReviewValid = Boolean(
    pendingAction.tool_name === 'prepare_vault_deposit' &&
    pendingAction.pending_action_id?.trim() &&
    HASH_RE.test(pendingAction.normalized_intent_hash ?? '') &&
    sessionId.trim() && sessionId.length <= 128 &&
    chainId === FUJI_CHAIN_ID &&
    pendingAction.chain_id === FUJI_CHAIN_ID &&
    sameWallet && ownerMatches &&
    vault && ADDRESS_RE.test(vault) &&
    amountRelationshipValid &&
    Number.isFinite(deadlineMs) && now < deadlineMs &&
    Number.isFinite(expiresAtMs) && now < expiresAtMs,
  );

  async function handleDecision(decision: PendingDecision) {
    if (
      !isReviewValid || busy || signMessage.isPending || decisionStatus ||
      !pendingAction.pending_action_id || !pendingAction.normalized_intent_hash ||
      !address
    ) return;

    setBusy(true);
    setDecisionError(null);
    try {
      // Core signs ONLY the challenge message supplied by the Backend.
      // This stage does NOT broadcast an on-chain transaction.
      const result = await executeSignedDecision(
        pendingAction.pending_action_id,
        sessionId,
        pendingAction.normalized_intent_hash,
        decision,
        (message) => signMessage.mutateAsync({ message, account: address }),
      );
      if (result.pending_action_id !== pendingAction.pending_action_id) {
        throw new Error('Backend returned a different pending action ID.');
      }
      if (result.status !== (decision === 'confirm' ? 'confirmed' : 'rejected')) {
        throw new Error('Backend decision does not match the requested decision.');
      }
      if (result.status === 'confirmed' &&
          (!Number.isFinite(Date.parse(result.expires_at)) ||
            Date.now() >= Date.parse(result.expires_at))) {
        throw new Error('Confirmed action is expired. Prepare a new deposit.');
      }
      setDecisionStatus(result.status);
    } catch (error) {
      setDecisionError(error instanceof Error ? error.message : 'Unable to sign deposit decision.');
    } finally {
      setBusy(false);
    }
  }

  if (!isOpen) return null;

  const rows: [string, string][] = [
    ['Vault', vault ?? 'Thiếu'],
    ['Assets', assetsRaw ? `${formattedAmount(summary, 'assets') ?? ''} (raw: ${assetsRaw})` : 'Thiếu raw string'],
    ['Asset token', assetToken ?? 'Chưa cung cấp'],
    ['Preview shares', previewSharesRaw ? `${formattedAmount(summary, 'preview_shares') ?? ''} (raw: ${previewSharesRaw})` : 'Thiếu raw string'],
    ['Minimum shares', minSharesRaw ? `${formattedAmount(summary, 'min_shares') ?? ''} (raw: ${minSharesRaw})` : 'Thiếu raw string'],
    ['Deadline', displayTime(deadlineMs)],
    ['Pending action expires', displayTime(expiresAtMs)],
    ['Wallet', pendingAction.wallet_address ?? 'Thiếu'],
    ['Network', pendingAction.chain_id === FUJI_CHAIN_ID ? 'Avalanche Fuji (43113)' : 'Sai mạng / thiếu chain'],
    ['Action ID', pendingAction.pending_action_id ?? 'Thiếu'],
  ];

  return (
    <div className="deposit-review-backdrop">
      <section className="deposit-review-modal" role="dialog" aria-modal="true" aria-labelledby="deposit-review-title">
        <header className="deposit-review-header">
          <div>
            <p className="deposit-review-eyebrow">DemoVault · Avalanche Fuji (verify contract address)</p>
            <h2 id="deposit-review-title">Deposit Review</h2>
          </div>
          <button type="button" onClick={onClose} aria-label="Đóng Deposit Review">×</button>
        </header>

        <p role="status" className="deposit-review-warning">
          Dữ liệu được lấy từ pending action của Backend. Ký Confirm chỉ ký thông điệp challenge;
          chưa gửi transaction lên blockchain. Hãy đối chiếu địa chỉ vault với Dev 1/Dev 2.
        </p>

        <dl className="deposit-review-fields">
          {rows.map(([label, value]) => (
            <div key={label}><dt>{label}</dt><dd>{value}</dd></div>
          ))}
        </dl>

        {!isReviewValid && (
          <p className="deposit-review-error" role="alert">
            Review chưa đạt kiểm tra: ví, chain, vault, số lượng raw (string), preview/min shares,
            deadline, expiry hoặc review hash. Không được xác nhận giao dịch cũ hoặc thiếu dữ liệu.
          </p>
        )}
        {decisionError && <p className="deposit-review-error" role="alert">{decisionError}</p>}
        {decisionStatus === 'confirmed' && (
          <p role="status" className="deposit-review-warning">
            Backend đã xác nhận quyết định. <strong>Deposit chưa được ký và phát lên Fuji.</strong>
            Bước Send Transaction/Submit Hash/Receipt chỉ được mở sau khi đối chiếu
            ABI và calldata của Deposit với bản review.
          </p>
        )}
        {decisionStatus === 'rejected' && <p role="status">Đã từ chối pending action.</p>}

        <details>
          <summary>Chi tiết summary do Backend trả về</summary>
          <pre>{JSON.stringify(summary, null, 2)}</pre>
        </details>

        <footer className="deposit-review-actions">
          <button type="button" onClick={onClose}>Đóng review</button>
          <button type="button" onClick={() => void handleDecision('reject')}
            disabled={!isReviewValid || busy || signMessage.isPending || decisionStatus !== null}>
            Từ chối
          </button>
          <button type="button" onClick={() => void handleDecision('confirm')}
            disabled={!isReviewValid || busy || signMessage.isPending || decisionStatus !== null}>
            {busy || signMessage.isPending ? 'Đang ký challenge...' : 'Ký Confirm Review'}
          </button>
        </footer>
      </section>
    </div>
  );
}
