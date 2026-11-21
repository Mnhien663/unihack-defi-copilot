import { useEffect, useMemo, useState } from 'react';
import { useConnection, useSendTransaction, useSignMessage } from 'wagmi';
import {
  executeSignedDecision,
  type PendingDecision,
} from '../../services/pendingActionApi';
import { useSwapReceiptSigning } from '../../hooks/useSwapReceiptSigning';
import {
  buildReviewedDepositSendRequest,
  type DepositSendRequest,
} from '../../types/depositTransaction';
import {
  hasStoredPendingDeposit,
  savePendingDeposit,
  updatePendingDepositReceipt,
} from '../../utils/pendingDepositStorage';
import { hasStoredPendingSwap } from '../../utils/pendingSwapStorage';
import { canonicalDepositJson } from '../../utils/canonicalDepositJson';
import './DepositReviewModal.css';

const FUJI_CHAIN_ID = 43113;
const HASH_RE = /^[0-9a-f]{64}$/;
const ADDRESS_RE = /^0x[0-9a-fA-F]{40}$/;

type JsonObject = Record<string, unknown>;

export type DepositPendingAction = {
  tool_name: 'prepare_vault_deposit';
  pending_action_id?: string;
  normalized_intent_hash?: string;
  chain_id?: number;
  wallet_address?: string;
  expires_at?: string;
  arguments?: { summary?: unknown; transaction?: unknown };
};

function isRecord(value: unknown): value is JsonObject {
  return value !== null && typeof value === 'object' && !Array.isArray(value);
}

export function isDepositPendingAction(value: unknown): value is DepositPendingAction {
  return isRecord(value) && value.tool_name === 'prepare_vault_deposit';
}

function readString(source: JsonObject, key: string): string | null {
  const value = source[key];
  return typeof value === 'string' && value.trim() ? value.trim() : null;
}

function amount(summary: JsonObject, key: string): string {
  const info = isRecord(summary[key]) ? summary[key] : {};
  const raw = readString(info, 'raw');
  const formatted = readString(info, 'formatted');
  return raw ? `${formatted ? `${formatted} · ` : ''}raw: ${raw}` : 'Thiếu raw string';
}

function displayTime(ms: number): string {
  return Number.isFinite(ms)
    ? new Date(ms).toLocaleString('vi-VN', { timeZone: 'Asia/Ho_Chi_Minh' })
    : 'Thiếu / không hợp lệ';
}

type DepositReviewModalProps = {
  isOpen: boolean;
  pendingAction: DepositPendingAction;
  sessionId: string;
  onClose: () => void;
};

type ApprovedScope = {
  actionId: string;
  reviewHash: string;
  sessionId: string;
  wallet: string;
  expiresAtMs: number;
  summarySnapshot: string;
};

export default function DepositReviewModal({
  isOpen,
  pendingAction,
  sessionId,
  onClose,
}: DepositReviewModalProps) {
  const { address, chainId, isConnected } = useConnection();
  const signMessage = useSignMessage();
  const sendTransaction = useSendTransaction();
  const {
    submitHashWithWallet,
    refreshReceiptWithWallet,
    isSigning,
  } = useSwapReceiptSigning(false);

  const [now, setNow] = useState(Date.now);
  const [busy, setBusy] = useState(false);
  const [decisionStatus, setDecisionStatus] = useState<'confirmed' | 'rejected' | null>(null);
  const [decisionError, setDecisionError] = useState<string | null>(null);
  const [approvedScope, setApprovedScope] = useState<ApprovedScope | null>(null);
  const [sendRequest, setSendRequest] = useState<DepositSendRequest | null>(null);
  const [txHash, setTxHash] = useState<`0x${string}` | null>(null);
  const [sendError, setSendError] = useState<string | null>(null);
  const [hashRecorded, setHashRecorded] = useState(false);
  const [hashStatus, setHashStatus] = useState<string | null>(null);
  const [hashError, setHashError] = useState<string | null>(null);
  const [hashBusy, setHashBusy] = useState(false);
  const [receiptStatus, setReceiptStatus] = useState<string | null>(null);
  const [receiptError, setReceiptError] = useState<string | null>(null);
  const [receiptBusy, setReceiptBusy] = useState(false);

  useEffect(() => {
    if (!isOpen) return;
    setNow(Date.now());
    const timer = window.setInterval(() => setNow(Date.now()), 1000);
    function onKeyDown(event: KeyboardEvent) {
      if (event.key === 'Escape' && !busy && !sendTransaction.isPending &&
          !hashBusy && !receiptBusy && !signMessage.isPending && !isSigning) onClose();
    }
    window.addEventListener('keydown', onKeyDown);
    return () => {
      window.clearInterval(timer);
      window.removeEventListener('keydown', onKeyDown);
    };
  }, [isOpen, onClose, busy, sendTransaction.isPending,
      hashBusy, receiptBusy, signMessage.isPending, isSigning]);

  // Keep the state when the same review is reopened; discard it for a new action.
  useEffect(() => {
    setDecisionStatus(null);
    setDecisionError(null);
    setApprovedScope(null);
    setSendRequest(null);
    setTxHash(null);
    setSendError(null);
    setHashRecorded(false);
    setHashStatus(null);
    setHashError(null);
    setReceiptStatus(null);
    setReceiptError(null);
  }, [pendingAction.pending_action_id, sessionId]);

  const summary = useMemo<JsonObject>(
    () => isRecord(pendingAction.arguments?.summary) ? pendingAction.arguments.summary : {},
    [pendingAction.arguments?.summary],
  );

  const preflight = useMemo(() => {
    if (!address) return { error: 'Connect Core Wallet before deposit review.' };
    try {
      const request = buildReviewedDepositSendRequest(
        pendingAction.arguments,
        pendingAction.arguments,
        address,
        pendingAction.expires_at ?? '',
        pendingAction.expires_at ?? '',
      );
      return { request };
    } catch (error) {
      return { error: error instanceof Error ? error.message : 'Invalid deposit review.' };
    }
    // `now` deliberately recomputes freshness every second.
  }, [address, pendingAction.arguments, pendingAction.expires_at, now]);

  const reviewReady = Boolean(
    pendingAction.tool_name === 'prepare_vault_deposit' &&
    pendingAction.pending_action_id?.trim() &&
    HASH_RE.test(pendingAction.normalized_intent_hash ?? '') &&
    sessionId.trim() && sessionId.length <= 128 &&
    isConnected && chainId === FUJI_CHAIN_ID &&
    pendingAction.chain_id === FUJI_CHAIN_ID &&
    address && ADDRESS_RE.test(address) &&
    pendingAction.wallet_address?.toLowerCase() === address.toLowerCase() &&
    !preflight.error,
  );

  const actionId = pendingAction.pending_action_id ?? '';
  const reviewHash = pendingAction.normalized_intent_hash ?? '';
  const approvedScopeMatches = Boolean(
    approvedScope &&
    approvedScope.actionId === actionId &&
    approvedScope.reviewHash === reviewHash &&
    approvedScope.sessionId === sessionId &&
    address && approvedScope.wallet.toLowerCase() === address.toLowerCase() &&
    chainId === FUJI_CHAIN_ID &&
    canonicalDepositJson(summary) === approvedScope.summarySnapshot,
  );
  const isProcessing = busy || sendTransaction.isPending || hashBusy || receiptBusy ||
    signMessage.isPending || isSigning;

  async function handleDecision(decision: PendingDecision) {
    if (!reviewReady || isProcessing || decisionStatus || !address) return;
    setBusy(true);
    setDecisionError(null);
    try {
      const result = await executeSignedDecision(
        actionId,
        sessionId,
        reviewHash,
        decision,
        (message) => signMessage.mutateAsync({ message, account: address }),
      );
      if (result.pending_action_id !== actionId ||
          result.status !== (decision === 'confirm' ? 'confirmed' : 'rejected')) {
        throw new Error('Backend returned a different action or decision.');
      }
      // A confirmed action must not be signed twice, even if client-side
      // validation below fails. Reprepare rather than retrying a decision.
      setDecisionStatus(result.status);
      if (result.status === 'confirmed') {
        const request = buildReviewedDepositSendRequest(
          result.arguments,
          pendingAction.arguments,
          address,
          pendingAction.expires_at ?? '',
          result.expires_at,
        );
        setSendRequest(request);
        setApprovedScope({
          actionId,
          reviewHash,
          sessionId,
          wallet: address,
          expiresAtMs: Date.parse(result.expires_at),
          summarySnapshot: canonicalDepositJson(summary),
        });
      }
    } catch (error) {
      setDecisionError(error instanceof Error ? error.message : 'Unable to confirm Deposit.');
    } finally {
      setBusy(false);
    }
  }

  async function handleSend() {
    if (!address || !sendRequest || !approvedScope || !approvedScopeMatches ||
        !reviewReady || decisionStatus !== 'confirmed' || txHash || isProcessing) return;
    setSendError(null);
    if (Date.now() >= approvedScope.expiresAtMs) {
      setSendError('Confirmed Deposit expired. Prepare a new pending action.');
      return;
    }
    // The preview may have expired while Core was displaying a prior prompt.
    try {
      buildReviewedDepositSendRequest(
        pendingAction.arguments, pendingAction.arguments,
        address, pendingAction.expires_at ?? '',
        new Date(approvedScope.expiresAtMs).toISOString(),
      );
      if (hasStoredPendingSwap() || hasStoredPendingDeposit()) {
        throw new Error('An earlier Swap or Deposit transaction still needs receipt recovery.');
      }
    } catch (error) {
      setSendError(error instanceof Error ? error.message : 'Deposit validation failed.');
      return;
    }
    try {
      const hash = await sendTransaction.mutateAsync({
        ...sendRequest,
        account: address,
        chainId: FUJI_CHAIN_ID,
      });
      setTxHash(hash); // A returned hash means broadcast, not successful mining.
      try {
        savePendingDeposit({
          actionId: approvedScope.actionId,
          sessionId: approvedScope.sessionId,
          reviewHash: approvedScope.reviewHash,
          walletAddress: approvedScope.wallet,
          chainId: FUJI_CHAIN_ID,
          transactionHash: hash,
          savedAt: Date.now(),
        });
      } catch {
        setSendError('TX broadcast nhưng không lưu được recovery. Sao chép hash trước khi đóng trang!');
      }
    } catch (error) {
      setSendError(error instanceof Error ? error.message : 'Core Wallet could not broadcast Deposit.');
    }
  }

  async function handleSubmitHash() {
    if (!txHash || !approvedScope || !approvedScopeMatches || hashRecorded || isProcessing) return;
    setHashBusy(true);
    setHashError(null);
    try {
      const result = await submitHashWithWallet(approvedScope.actionId, {
        session_id: approvedScope.sessionId,
        review_hash: approvedScope.reviewHash,
        wallet_address: approvedScope.wallet,
        transaction_hash: txHash,
      });
      if (result.pending_action_id !== approvedScope.actionId ||
          result.transaction_hash.toLowerCase() !== txHash.toLowerCase()) {
        throw new Error('Backend returned a different Deposit transaction hash.');
      }
      setHashRecorded(true);
      setHashStatus(result.status);
      updatePendingDepositReceipt(txHash, { hashRecorded: true });
    } catch (error) {
      setHashError(error instanceof Error ? error.message : 'Submit Hash failed.');
    } finally {
      setHashBusy(false);
    }
  }

  async function handleRefresh() {
    if (!txHash || !approvedScope || !approvedScopeMatches ||
        !hashRecorded || isProcessing || receiptStatus === 'mined') return;
    setReceiptBusy(true);
    setReceiptError(null);
    try {
      const result = await refreshReceiptWithWallet(approvedScope.actionId, {
        session_id: approvedScope.sessionId,
        review_hash: approvedScope.reviewHash,
        wallet_address: approvedScope.wallet,
      });
      if (result.pending_action_id !== approvedScope.actionId ||
          result.transaction_hash.toLowerCase() !== txHash.toLowerCase()) {
        throw new Error('Backend returned a different Deposit receipt.');
      }
      setReceiptStatus(result.status);
      updatePendingDepositReceipt(txHash, { hashRecorded: true, receiptStatus: result.status });
    } catch (error) {
      setReceiptError(error instanceof Error ? error.message : 'Refresh Receipt failed.');
    } finally {
      setReceiptBusy(false);
    }
  }

  if (!isOpen) return null;
  const assetToken =
    isRecord(summary.assets) && isRecord(summary.assets.asset)
      ? summary.assets.asset
      : {};
  const rows: [string, string][] = [
    ['Vault (DemoVault)', readString(summary, 'vault_address') ?? 'Thiếu'],
    ['Assets', amount(summary, 'assets')],

    ['Asset token', readString(assetToken, 'symbol') ?? 'Thiếu'],

    ['Asset address', readString(assetToken, 'address') ?? 'Thiếu'],

    ['Asset decimals',
      typeof assetToken.decimals === 'number' &&
      Number.isInteger(assetToken.decimals)
        ? String(assetToken.decimals)
        : 'Thiếu'],

    ['Preview shares', amount(summary, 'preview_shares')],
    ['Minimum shares', amount(summary, 'min_shares')],
    ['Execution Manager', readString(summary, 'manager_address') ?? 'Thiếu'],
    ['Deposit deadline', displayTime(typeof summary.deadline === 'number' ? summary.deadline * 1000 : NaN)],
    ['Preview expires', displayTime(Date.parse(String(summary.preview_expires_at ?? '')))],
    ['Action expires', displayTime(Date.parse(pendingAction.expires_at ?? ''))],
    ['Network', pendingAction.chain_id === FUJI_CHAIN_ID ? 'Avalanche Fuji (43113)' : 'Sai mạng'],
    ['Wallet', pendingAction.wallet_address ?? 'Thiếu'],
    ['Action ID', actionId || 'Thiếu'],
  ];

  return (
    <div className="deposit-review-backdrop">
      <section className="deposit-review-modal" role="dialog" aria-modal="true" aria-labelledby="deposit-review-title">
        <header className="deposit-review-header">
          <div>
            <p className="deposit-review-eyebrow">DemoVault · Avalanche Fuji</p>
            <h2 id="deposit-review-title">Deposit Review</h2>
          </div>
          <button type="button" disabled={isProcessing} onClick={onClose} aria-label="Đóng Deposit Review">×</button>
        </header>

        <p role="status" className="deposit-review-warning">
          Core ký challenge trước, sau đó mới có thể gửi transaction. Chỉ dùng Fuji testnet.
          Preview/deadline hết hạn phải tạo pending action mới, không gửi lại giao dịch cũ.
        </p>
        <dl className="deposit-review-fields">
          {rows.map(([label, value]) => <div key={label}><dt>{label}</dt><dd>{value}</dd></div>)}
        </dl>

        {!reviewReady && !txHash && (
          <p role="alert" className="deposit-review-error">
            Review chưa hợp lệ: {preflight.error ?? 'Ví, chain, session hoặc pending action không khớp.'}
          </p>
        )}
        {decisionError && <p role="alert" className="deposit-review-error">{decisionError}</p>}
        {sendError && <p role="alert" className="deposit-review-error">{sendError}</p>}
        {hashError && <p role="alert" className="deposit-review-error">{hashError}</p>}
        {receiptError && <p role="alert" className="deposit-review-error">{receiptError}</p>}

        {decisionStatus && <p role="status">Review decision: <strong>{decisionStatus}</strong></p>}
        {txHash && (
          <div role="status" className="deposit-review-warning">
            <strong>Transaction đã gửi, chưa xác nhận mined:</strong>
            <p style={{ overflowWrap: 'anywhere' }}><code>{txHash}</code></p>
            <a href={`https://testnet.snowtrace.io/tx/${txHash}`}
               target="_blank" rel="noreferrer">Kiểm tra trên Fuji Explorer</a>
          </div>
        )}
        {hashStatus && <p role="status">Submit Hash: <strong>{hashStatus}</strong></p>}
        {receiptStatus && <p role="status">Receipt: <strong>{receiptStatus}</strong></p>}

        <details>
          <summary>Chi tiết summary do Backend trả về</summary>
          <pre>{JSON.stringify(summary, null, 2)}</pre>
        </details>

        <footer className="deposit-review-actions" style={{ flexWrap: 'wrap' }}>
          <button type="button" disabled={isProcessing} onClick={onClose}>Đóng review</button>
          <button type="button" onClick={() => void handleDecision('reject')}
            disabled={!reviewReady || isProcessing || decisionStatus !== null}>Từ chối</button>
          <button type="button" onClick={() => void handleDecision('confirm')}
            disabled={!reviewReady || isProcessing || decisionStatus !== null}>
            {busy || signMessage.isPending ? 'Core ký challenge...' : 'Confirm Review'}
          </button>
          <button type="button" onClick={() => void handleSend()}
            disabled={!reviewReady || !approvedScopeMatches || !sendRequest || decisionStatus !== 'confirmed' ||
              isProcessing || Boolean(txHash)}>
            {sendTransaction.isPending ? 'Core gửi Deposit...' : 'Send Deposit Transaction'}
          </button>
          <button type="button" onClick={() => void handleSubmitHash()}
            disabled={!txHash || !approvedScopeMatches || hashRecorded || isProcessing}>
            {hashBusy ? 'Đang ký Submit Hash...' : 'Submit Hash'}
          </button>
          <button type="button" onClick={() => void handleRefresh()}
            disabled={!txHash || !approvedScopeMatches || !hashRecorded ||
              isProcessing || receiptStatus === 'mined' || receiptStatus === 'failed'}>
            {receiptBusy ? 'Đang kiểm tra receipt...' : 'Refresh Receipt'}
          </button>
        </footer>
      </section>
    </div>
  );
}
