import TransactionCard, {
  type PendingAction,
} from './TransactionCard';

import './ConfirmationModal.css';
import { useEffect, useState } from 'react';
import { useConnection } from 'wagmi';

const FUJI_MANAGER_ADDRESS =
  '0xA7c9881eb43902Ca668bb864668eAA3795f2283B';

type ConfirmationModalProps = {
  isOpen: boolean;
  pendingAction: PendingAction | null;
  onClose: () => void;
};

function ConfirmationModal({
  isOpen,
  pendingAction,
  onClose,
}: ConfirmationModalProps) {
  const { address, isConnected, chainId } = useConnection();

  const isCorrectChain = isConnected && chainId === 43113;

  const isWalletMatch =
  Boolean(address && pendingAction?.wallet_address) &&
  address!.toLowerCase() ===
  pendingAction!.wallet_address!.toLowerCase();

  const isActionChainMatch =
  isCorrectChain &&
  pendingAction?.chain_id === chainId;

  const isManagerMatch =
  pendingAction?.arguments.summary.manager_address?.toLowerCase() ===
  FUJI_MANAGER_ADDRESS.toLowerCase();

  const hasActionId =
  Boolean(pendingAction?.pending_action_id?.trim());

  const hasReviewHash =
    Boolean(pendingAction?.normalized_intent_hash?.trim());

  const [now, setNow] = useState(Date.now);

  const expiresAt = pendingAction?.expires_at
    ? new Date(pendingAction.expires_at).getTime()
    : NaN;

  const isExpired =
    !Number.isFinite(expiresAt) || now >= expiresAt;

  const isReadyForChallenge =
    hasActionId &&
    hasReviewHash &&
    !isExpired &&
    isCorrectChain &&
    isWalletMatch &&
    isActionChainMatch &&
    isManagerMatch;

    useEffect(() => {
    if (!isOpen) return;

    // Cập nhật ngay khi mở Modal
    setNow(Date.now());

    // Kiểm tra lại mỗi giây
    const timer = setInterval(() => {
        setNow(Date.now());
    }, 1000);

    return () => clearInterval(timer);
    }, [isOpen]);

    useEffect(() => {
        if (!isOpen) return;

        function handleKeyDown(event: KeyboardEvent) {
            if (event.key === 'Escape') {
            onClose();
            }
        }

        window.addEventListener('keydown', handleKeyDown);

        return () => {
            window.removeEventListener('keydown', handleKeyDown);
        };
        }, [isOpen, onClose]);

  // Không có giao dịch hoặc modal chưa mở thì không hiển thị
  if (!isOpen || !pendingAction) {
    return null;
  }

  return (
    <div className="confirmation-backdrop">
      <div
        className="confirmation-modal"
        role="dialog"
        aria-modal="true"
        aria-labelledby="confirmation-title"
      >
        {/* Header */}
        <div className="confirmation-header">
          <h2 id="confirmation-title">
            Confirm Transaction
          </h2>

          <button
            type="button"
            onClick={onClose}
            aria-label="Close modal"
          >
            ×
          </button>
        </div>

        {/* MOCK warning */}
        <p>MOCK PREVIEW — No real transaction</p>
        <div className="confirmation-network-status" role="status">
          {!isWalletMatch && (
            <p role="alert">
              ⚠️ Wallet does not match transaction owner.
              Transaction confirmation is disabled.
            </p>
          )}

          {!isActionChainMatch && (
            <p role="alert">
              ⚠️ Transaction network mismatch.
              Please switch to Avalanche Fuji.
            </p>
          )}

          {!isManagerMatch && (
            <p role="alert">
              ⚠️ Manager Contract mismatch.
              Transaction confirmation is disabled.
            </p>
          )}

          <p role="status">
            {isReadyForChallenge
              ? 'Basic checks passed — Awaiting Challenge API.'
              : 'Transaction not ready for confirmation.'}
          </p>

          {!isConnected
            ? '⚠️ Wallet not connected'
            : !isCorrectChain
              ? '⚠️ Wrong network — Switch to Avalanche Fuji'
              : '✓ Wallet connected to Avalanche Fuji'}
        </div>

        {isExpired && (
        <p role="alert">
            ⚠️ Transaction expired — Cannot confirm.
        </p>
        )}

        {/* Transaction information */}
        <TransactionCard
          pendingAction={pendingAction}
          isMock={true}
        />

        {/* Confirmation buttons */}
        <div className="confirmation-actions">
          <button type="button" disabled>
            Reject
          </button>

          <button type="button" disabled>
            Confirm
          </button>
        </div>

        {/* Close modal */}
        <button type="button" onClick={onClose}>
          Close Preview
        </button>
      </div>
    </div>
  );
}

export default ConfirmationModal;
