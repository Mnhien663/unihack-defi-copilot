import TransactionCard, {
  type PendingAction,
} from './TransactionCard';

import './ConfirmationModal.css';
import { useEffect, useState } from 'react';
import {
  useConnection,
  useSignMessage,
  useSendTransaction,
} from 'wagmi';

import {
  executeSignedDecision,
  type PendingDecision,
} from '../../services/pendingActionApi';

import {
  buildReviewedSwapSendRequest,
  type SwapSendRequest,
} from '../../types/swapTransaction';

import { useSwapReceiptSigning } from '../../hooks/useSwapReceiptSigning';

import {
  savePendingSwap,
  loadPendingSwap,
  hasStoredPendingSwap,
} from '../../utils/pendingSwapStorage';

const FUJI_MANAGER_ADDRESS =
  '0xA7c9881eb43902Ca668bb864668eAA3795f2283B';

type ConfirmationModalProps = {
  isOpen: boolean;
  pendingAction: PendingAction | null;
  sessionId: string;
  isMock?: boolean;
  onClose: () => void;
};

function ConfirmationModal({
  isOpen,
  pendingAction,
  sessionId,
  isMock = true,
  onClose,
}: ConfirmationModalProps) {
  const { address, isConnected, chainId } = useConnection();

  const isCorrectChain = isConnected && chainId === 43113;

  const signMessage = useSignMessage();

  const sendTransaction = useSendTransaction();

  const {
    submitHashWithWallet,
    refreshReceiptWithWallet,
    isSigning,
  } = useSwapReceiptSigning(isMock);

  const [isSubmittingHash, setIsSubmittingHash] =
    useState(false);

  const [hashRecorded, setHashRecorded] =
    useState(false);

  const [submitHashStatus, setSubmitHashStatus] =
    useState<string | null>(null);

  const [submitHashError, setSubmitHashError] =
    useState<string | null>(null);

  const [isProcessing, setIsProcessing] = useState(false);
  const [decisionError, setDecisionError] = useState<string | null>(null);
  const [decisionStatus, setDecisionStatus] = useState<string | null>(null);

  const [reviewedSendRequest, setReviewedSendRequest] = useState<SwapSendRequest | null>(null);

  const [preparedScope, setPreparedScope] = useState<{
    actionId: string;
    reviewHash: string;
    sessionId: string;
    walletAddress: string;
    confirmedExpiresAt: number;
    summarySnapshot: string;
  } | null>(null);

  const [transactionHash, setTransactionHash] =
    useState<`0x${string}` | null>(null);

  const [sendError, setSendError] =
    useState<string | null>(null);

  const [isRefreshingReceipt, setIsRefreshingReceipt] =
    useState(false);

  const [receiptStatus, setReceiptStatus] =
    useState<string | null>(null);

  const [receiptError, setReceiptError] =
    useState<string | null>(null);

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
    /^[0-9a-f]{64}$/.test(
      pendingAction?.normalized_intent_hash ?? ''
    );

  const hasValidSessionId =
    sessionId.length > 0 &&
    sessionId.length <= 128;

  const [now, setNow] = useState(Date.now);

  const expiresAt = pendingAction?.expires_at
    ? new Date(pendingAction.expires_at).getTime()
    : NaN;

  const isExpired =
    !Number.isFinite(expiresAt) || now >= expiresAt;

  const isReadyForChallenge =
    hasActionId &&
    hasReviewHash &&
    hasValidSessionId &&
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

  async function handleDecision(decision: PendingDecision) {
    // Không cho phép ký MOCK hoặc giao dịch chưa đạt kiểm tra
    if (
      isMock ||
      !isReadyForChallenge ||
      !pendingAction?.pending_action_id ||
      !pendingAction.normalized_intent_hash ||
      !address ||
      isProcessing ||
      signMessage.isPending ||
      decisionStatus !== null ||
      transactionHash !== null
    ) {
      return;
    }

    setIsProcessing(true);
    setDecisionError(null);
    setDecisionStatus(null);
    setReviewedSendRequest(null);

    setPreparedScope(null);
    setTransactionHash(null);
    setSendError(null);

    setHashRecorded(false);
    setSubmitHashStatus(null);
    setSubmitHashError(null);

    setReceiptStatus(null);
    setReceiptError(null);

    try {
      const result = await executeSignedDecision(
        pendingAction.pending_action_id,
        sessionId,
        pendingAction.normalized_intent_hash,
        decision,

        // Core ký nguyên văn message do Backend trả về
        (message) =>
          signMessage.mutateAsync({
            message,
            account: address,
          })
      );

      // Kiểm tra Backend trả về đúng pending action
      if (
        result.pending_action_id !== pendingAction.pending_action_id
      ) {
        throw new Error('Confirmed action ID mismatch.');
      }

      setDecisionStatus(result.status);

      if (result.status === 'confirmed') {
        // Kiểm tra transaction với thông tin đã review
        const sendRequest = buildReviewedSwapSendRequest(
          result.arguments,
          address,
          pendingAction.arguments.summary
        );

        // Chỉ lưu transaction sau khi kiểm tra thành công
        setReviewedSendRequest(sendRequest);

        setPreparedScope({
          actionId: pendingAction.pending_action_id,
          reviewHash: pendingAction.normalized_intent_hash!,
          sessionId,
          walletAddress: address,
          confirmedExpiresAt: Date.parse(result.expires_at),
          summarySnapshot: JSON.stringify(
            pendingAction.arguments.summary
          ),
        });
      }

      // Chưa gửi arguments.transaction lên blockchain.
      // Confirmed KHÔNG có nghĩa là swap thành công.
    } catch (error) {
      setDecisionError(
        error instanceof Error
          ? error.message
          : 'Unable to process transaction decision.'
      );
    } finally {
      setIsProcessing(false);
    }
  }

async function handleSendTransaction() {
  if (
    isMock ||
    !pendingAction ||
    !reviewedSendRequest ||
    !preparedScope ||
    !address ||
    decisionStatus !== 'confirmed' ||
    sendTransaction.isPending ||
    transactionHash
  ) {
    return;
  }

  setSendError(null);

  // Kiểm tra lại thông tin trước khi mở ví
  if (
    !isReadyForChallenge ||
    pendingAction.pending_action_id !== preparedScope.actionId ||
    pendingAction.normalized_intent_hash !== preparedScope.reviewHash ||
    sessionId !== preparedScope.sessionId ||
    address.toLowerCase() !== preparedScope.walletAddress.toLowerCase() ||
    JSON.stringify(pendingAction.arguments.summary) !==
      preparedScope.summarySnapshot ||
    !Number.isFinite(preparedScope.confirmedExpiresAt) ||
    Date.now() >= preparedScope.confirmedExpiresAt
  ) {
    setSendError(
      'Transaction context changed or expired. Prepare a new swap.'
    );
    return;
  }

  const quoteExpiry =
    pendingAction.arguments.summary.quote_expires_at;

  if (
    !quoteExpiry ||
    !Number.isFinite(Date.parse(quoteExpiry)) ||
    Date.now() >= Date.parse(quoteExpiry)
  ) {
    setSendError('Swap quote expired. Prepare a new swap.');
    return;
  }

  // Không gửi swap mới nếu tab đã lưu giao dịch trước đó.
  try {
    const existingSwap = loadPendingSwap();

    if (hasStoredPendingSwap()) {
      setSendError(
        existingSwap
          ? 'Previous swap detected: ' +
              existingSwap.transactionHash +
              '. Verify it before sending another swap.'
          : 'A saved swap record exists but cannot be read. ' +
              'New transactions are blocked for safety.'
      );
      return;
    }
  } catch {
    setSendError(
      'Cannot check saved transaction state. ' +
        'Transaction sending is disabled for safety.'
    );
    return;
  }

  try {
    const hash = await sendTransaction.mutateAsync({
      ...reviewedSendRequest,
      account: address,
      chainId: 43113,
    });

    // Core Wallet đã trả transaction hash.
    // Lưu ngay thông tin để chuẩn bị khôi phục sau F5.
    try {
      savePendingSwap({
        sessionId: preparedScope.sessionId,
        actionId: preparedScope.actionId,
        reviewHash: preparedScope.reviewHash,
        walletAddress: preparedScope.walletAddress,
        chainId: 43113,
        transactionHash: hash,
        savedAt: Date.now(),
      });
    } catch {
      // Lỗi lưu storage KHÔNG có nghĩa giao dịch thất bại.
      setSendError(
        'Transaction was broadcast, but could not be saved. ' +
        'Please keep the transaction hash before closing this page.'
      );
    }

    // Luôn hiển thị hash, kể cả khi lưu storage thất bại.
    setTransactionHash(hash);

  } catch (error) {
    setSendError(
      error instanceof Error
        ? error.message
        : 'Failed to send transaction.'
    );
  }
}

async function handleSubmitHash() {
  if (
    isMock ||
    !pendingAction ||
    !preparedScope ||
    !transactionHash ||
    !address ||
    chainId !== 43113 ||
    decisionStatus !== 'confirmed' ||
    hashRecorded ||
    isSubmittingHash ||
    isSigning
  ) {
    return;
  }

  // Kiểm tra hash thuộc đúng giao dịch đã Confirm
  if (
    pendingAction.pending_action_id !== preparedScope.actionId ||
    pendingAction.normalized_intent_hash !==
      preparedScope.reviewHash ||
    sessionId !== preparedScope.sessionId ||
    address.toLowerCase() !==
      preparedScope.walletAddress.toLowerCase() ||
    JSON.stringify(pendingAction.arguments.summary) !==
      preparedScope.summarySnapshot
  ) {
    setSubmitHashError(
      'Transaction context changed. Cannot submit hash.'
    );
    return;
  }

  setIsSubmittingHash(true);
  setSubmitHashError(null);

  try {
    const result = await submitHashWithWallet(
      preparedScope.actionId,
      {
        session_id: preparedScope.sessionId,
        review_hash: preparedScope.reviewHash,
        wallet_address: preparedScope.walletAddress,
        transaction_hash: transactionHash,
      }
    );

    // Đảm bảo Backend trả đúng giao dịch
    if (
      result.pending_action_id !== preparedScope.actionId ||
      result.transaction_hash.toLowerCase() !==
        transactionHash.toLowerCase()
    ) {
      throw new Error(
        'Backend returned a different transaction.'
      );
    }

    setSubmitHashStatus(result.status);
    setHashRecorded(true);
  } catch (error) {
    setSubmitHashError(
      error instanceof Error
        ? error.message
        : 'Unable to submit transaction hash.'
    );
  } finally {
    setIsSubmittingHash(false);
  }
}


async function handleRefreshReceipt() {
  if (
    isMock ||
    !pendingAction ||
    !preparedScope ||
    !transactionHash ||
    !hashRecorded ||
    !address ||
    chainId !== 43113 ||
    decisionStatus !== 'confirmed' ||
    receiptStatus === 'mined' ||
    isRefreshingReceipt ||
    isSigning
  ) {
    return;
  }

  // Kiểm tra giao dịch vẫn thuộc đúng phiên đã Confirm
  if (
    pendingAction.pending_action_id !== preparedScope.actionId ||
    pendingAction.normalized_intent_hash !== preparedScope.reviewHash ||
    sessionId !== preparedScope.sessionId ||
    address.toLowerCase() !==
      preparedScope.walletAddress.toLowerCase() ||
    JSON.stringify(pendingAction.arguments.summary) !==
      preparedScope.summarySnapshot
  ) {
    setReceiptError(
      'Transaction context changed. Cannot refresh receipt.'
    );
    return;
  }

  setIsRefreshingReceipt(true);
  setReceiptError(null);

  try {
    const result = await refreshReceiptWithWallet(
      preparedScope.actionId,
      {
        session_id: preparedScope.sessionId,
        review_hash: preparedScope.reviewHash,
        wallet_address: preparedScope.walletAddress,
      }
    );

    // Đảm bảo Backend trả đúng giao dịch
    if (
      result.pending_action_id !== preparedScope.actionId ||
      result.transaction_hash.toLowerCase() !==
        transactionHash.toLowerCase()
    ) {
      throw new Error(
        'Backend returned a different transaction receipt.'
      );
    }

    setReceiptStatus(result.status);
  } catch (error) {
    setReceiptError(
      error instanceof Error
        ? error.message
        : 'Unable to refresh transaction receipt.'
    );
  } finally {
    setIsRefreshingReceipt(false);
  }
}


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
        <p>
          {isMock
            ? 'MOCK PREVIEW — No real transaction'
            : receiptStatus === 'mined'
              ? 'API REVIEW — Receipt verified by Backend'
              : transactionHash
                ? 'API REVIEW — Transaction broadcasted'
                : 'API REVIEW — Transaction not submitted'}
        </p>
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
          isMock={isMock}
        />

        {/* Confirmation buttons */}
        <div className="confirmation-actions">
          <button
            type="button"
            onClick={() => void handleDecision('reject')}
            disabled={
              isMock ||
              !isReadyForChallenge ||
              isProcessing ||
              decisionStatus !== null ||
              transactionHash !== null ||
              signMessage.isPending
            }
          >
            Reject
          </button>

          <button
            type="button"
            onClick={() => void handleDecision('confirm')}
            disabled={
              isMock ||
              !isReadyForChallenge ||
              isProcessing ||
              decisionStatus !== null ||
              transactionHash !== null ||
              signMessage.isPending
            }
          >
            {isProcessing ? 'Processing...' : 'Confirm'}
          </button>
        </div>

        {decisionError && (
          <p role="alert">{decisionError}</p>
        )}

        {decisionStatus && (
          <p role="status">
            {decisionStatus === 'confirmed'
              ? receiptStatus === 'mined'
                ? 'Backend confirmed — Receipt verified.'
                : transactionHash
                  ? 'Backend confirmed — Transaction broadcasted.'
                  : 'Backend confirmed — Transaction not submitted yet.'
              : 'Transaction rejected.'}
          </p>
        )}

        {reviewedSendRequest &&
          decisionStatus === 'confirmed' &&
          !isMock &&
          !transactionHash && (
            <p role="status">
              Transaction passed local validation.
              Not submitted to blockchain.
            </p>
          )}

        {!isMock &&
          decisionStatus === 'confirmed' &&
          reviewedSendRequest && (
            <div className="swap-send-section">
              <button
                type="button"
                onClick={() => void handleSendTransaction()}
                disabled={
                  !preparedScope ||
                  !isReadyForChallenge ||
                  sendTransaction.isPending ||
                  transactionHash !== null
                }
              >
                {sendTransaction.isPending
                  ? 'Check Core Wallet...'
                  : transactionHash
                    ? 'Transaction Broadcasted'
                    : 'Send Transaction'}
              </button>

              {sendError && <p role="alert">{sendError}</p>}

              {transactionHash && (
                <p role="status">
                  {receiptStatus === 'mined'
                    ? 'Transaction mined — Backend verified receipt.'
                    : 'Transaction broadcasted. Receipt not verified.'}
                  <code>{transactionHash}</code>
                </p>
              )}

              {transactionHash && !isMock && (
                <div className="submit-hash-section">
                  <button
                    type="button"
                    onClick={() => void handleSubmitHash()}
                    disabled={
                      hashRecorded ||
                      isSubmittingHash ||
                      isSigning ||
                      !preparedScope ||
                      !address ||
                      chainId !== 43113
                    }
                  >
                    {isSubmittingHash || isSigning
                      ? 'Signing / Submitting...'
                      : hashRecorded
                        ? 'Hash Recorded'
                        : 'Submit Hash'}
                  </button>

                  {submitHashError && (
                    <p role="alert">{submitHashError}</p>
                  )}

                  {hashRecorded && (
                    <p role="status">
                      {receiptStatus === 'mined'
                        ? 'Backend recorded hash — Receipt verified.'
                        : `Backend recorded transaction. Status: ${submitHashStatus}`}
                    </p>
                  )}
                </div>
              )}

              {!isMock && transactionHash && hashRecorded && (
                <div className="refresh-receipt-section">
                  <button
                    type="button"
                    onClick={() => void handleRefreshReceipt()}
                    disabled={
                      isRefreshingReceipt ||
                      isSigning ||
                      !preparedScope ||
                      !address ||
                      chainId !== 43113 ||
                      receiptStatus === 'mined'
                    }
                  >
                    {isRefreshingReceipt || isSigning
                      ? 'Checking Receipt...'
                      : receiptStatus === 'mined'
                        ? 'Receipt Verified'
                        : 'Refresh Receipt'}
                  </button>

                  {receiptError && (
                    <p role="alert">{receiptError}</p>
                  )}

                  {receiptStatus && (
                    <p role="status">
                      {receiptStatus === 'mined'
                        ? 'Transaction mined — Backend verified receipt.'
                        : `Transaction status: ${receiptStatus}`}
                    </p>
                  )}
                </div>
              )}

            </div>
          )}

        {/* Close modal */}
        <button type="button" onClick={onClose}>
          Close Preview
        </button>
      </div>
    </div>
  );
}

export default ConfirmationModal;
