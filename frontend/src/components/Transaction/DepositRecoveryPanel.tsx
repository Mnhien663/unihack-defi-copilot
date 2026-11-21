import { useState } from 'react';
import { useConnection } from 'wagmi';
import { useSwapReceiptSigning } from '../../hooks/useSwapReceiptSigning';
import {
  clearPendingDeposit,
  updatePendingDepositReceipt,
  type StoredPendingDeposit,
} from '../../utils/pendingDepositStorage';

type Props = { deposit: StoredPendingDeposit };

// Recovery stays available after F5 even when the original pending action
// is no longer in Chatbot memory. Never sign a fresh transaction here.
export default function DepositRecoveryPanel({ deposit }: Props) {
  const { address, chainId, isConnected } = useConnection();
  const { submitHashWithWallet, refreshReceiptWithWallet, isSigning } = useSwapReceiptSigning(false);
  const [recorded, setRecorded] = useState(deposit.hashRecorded === true);
  const [status, setStatus] = useState<string | null>(deposit.receiptStatus ?? null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const isWalletReady = Boolean(isConnected && address && chainId === 43113 &&
    address.toLowerCase() === deposit.walletAddress.toLowerCase());

  async function submitHash() {
    if (!isWalletReady || isSigning || busy || recorded) return;
    setBusy(true);
    setError(null);
    try {
      const result = await submitHashWithWallet(deposit.actionId, {
        session_id: deposit.sessionId,
        review_hash: deposit.reviewHash,
        wallet_address: deposit.walletAddress,
        transaction_hash: deposit.transactionHash,
      });
      if (result.pending_action_id !== deposit.actionId ||
          result.transaction_hash.toLowerCase() !== deposit.transactionHash.toLowerCase()) {
        throw new Error('Backend returned a different Deposit transaction.');
      }
      setRecorded(true);
      setStatus(result.status);
      updatePendingDepositReceipt(deposit.transactionHash, { hashRecorded: true });
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Submit Hash recovery failed.');
    } finally {
      setBusy(false);
    }
  }

  async function refresh() {
    if (!isWalletReady || !recorded || isSigning || busy || status === 'mined' || status === 'failed') return;
    setBusy(true);
    setError(null);
    try {
      const result = await refreshReceiptWithWallet(deposit.actionId, {
        session_id: deposit.sessionId,
        review_hash: deposit.reviewHash,
        wallet_address: deposit.walletAddress,
      });
      if (result.pending_action_id !== deposit.actionId ||
          result.transaction_hash.toLowerCase() !== deposit.transactionHash.toLowerCase()) {
        throw new Error('Backend receipt does not match the saved Deposit hash.');
      }
      setStatus(result.status);
      updatePendingDepositReceipt(deposit.transactionHash, { hashRecorded: true, receiptStatus: result.status });
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Refresh Deposit receipt failed.');
    } finally {
      setBusy(false);
    }
  }

  function finishRecovery() {
    if (status !== 'mined' && status !== 'failed') return;
    try {
      clearPendingDeposit(deposit.transactionHash);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Could not clear Deposit recovery data.');
    }
  }

  return (
    <section className="pending-deposit-recovery" aria-label="Deposit receipt recovery"
      style={{ padding: 16, border: '1px solid #999', borderRadius: 12, margin: '12px 0' }}>
      <h3>Deposit transaction cần xác minh</h3>
      <p>Core đã trả về transaction hash. Đây <strong>không phải</strong> xác nhận giao dịch mined.</p>
      <p style={{ overflowWrap: 'anywhere' }}><code>{deposit.transactionHash}</code></p>
      <p>Session: <code>{deposit.sessionId}</code></p>
      {!isWalletReady && <p role="alert">Kết nối lại đúng ví trên Avalanche Fuji (43113).</p>}
      {error && <p role="alert" style={{ color: '#cc3333' }}>{error}</p>}
      {status && <p role="status">Backend receipt status: <strong>{status}</strong></p>}
      <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
        <button type="button" onClick={() => void submitHash()}
          disabled={!isWalletReady || recorded || isSigning || busy}>Submit Hash</button>
        <button type="button" onClick={() => void refresh()}
          disabled={!isWalletReady || !recorded || isSigning || busy || status === 'mined' || status === 'failed'}>
          Refresh Receipt
        </button>
        <button type="button" onClick={finishRecovery}
          disabled={status !== 'mined' && status !== 'failed'}>
          Đã xác minh — đóng recovery
        </button>
      </div>
    </section>
  );
}
