
import TransactionCard, {
  type PendingAction,
} from './TransactionCard';

import './ConfirmationModal.css';
import { useEffect, useState } from 'react';

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
  // Không có giao dịch hoặc modal chưa mở thì không hiển thị
  if (!isOpen || !pendingAction) {
    return null;
  }

  const [now, setNow] = useState(Date.now);

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

    const expiresAt = pendingAction?.expires_at
    ? new Date(pendingAction.expires_at).getTime()
    : NaN;

    const isExpired =
    !Number.isFinite(expiresAt) || now >= expiresAt;

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
