
import { useState, useRef, useEffect } from 'react';
import './Chatbot.css';
import ChatWelcome from "./ChatWelcome";

import PortfolioRiskCard from "../Portfolio/PortfolioRiskCard";
import { sendChatMessage } from '../../services/chatApi';

import TransactionCard, { type PendingAction } from '../Transaction/TransactionCard';
import prepareSwapMock from '../../mocks/prepareSwapMock.json';
import ConfirmationModal from '../Transaction/ConfirmationModal';

import { useConnection } from 'wagmi';

import { loadPendingSwap } from '../../utils/pendingSwapStorage';

import { useSwapReceiptSigning } from '../../hooks/useSwapReceiptSigning';

// Chỉ bật khi kiểm thử API cùng Dev 1
const ENABLE_DEV_API_TEST = true;

type ChatMessage = {
  id: number;
  role: 'user' | 'assistant';
  content: string;
};

function Chatbot() {
  const [draft, setDraft] = useState('');

  const { address, isConnected, chainId } = useConnection();

  const [pendingAction, setPendingAction] =
  useState<PendingAction | null>(null);
  const [isMockAction, setIsMockAction] = useState(false);

  const [isConfirmationOpen, setIsConfirmationOpen] = useState(false);

  const [isLoading, setIsLoading] = useState(false);
  const [apiError, setApiError] = useState<string | null>(null);

  const [sessionId] = useState<string>(() => {
    const key = 'deficopilot_chat_session_id';

    const saved = sessionStorage.getItem(key);

    if (
      saved &&
      /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(saved)
    ) {
      return saved;
    }

    const newSessionId = crypto.randomUUID();
    sessionStorage.setItem(key, newSessionId);

    return newSessionId;
  });

  const [recoveredSwap] = useState(() => {
    try {
      return loadPendingSwap();
    } catch {
      return null;
    }
  });

  const {
    submitHashWithWallet,
    refreshReceiptWithWallet,
    isSigning,
  } = useSwapReceiptSigning(recoveredSwap === null);

  const [isRecoveringHash, setIsRecoveringHash] =
    useState(false);

  const [recoveredHashRecorded, setRecoveredHashRecorded] =
    useState(false);

  const [recoveryStatus, setRecoveryStatus] =
    useState<string | null>(null);

  const [recoveryError, setRecoveryError] =
    useState<string | null>(null);

  const [isRecoveringReceipt, setIsRecoveringReceipt] =
    useState(false);

  const [recoveredReceiptStatus, setRecoveredReceiptStatus] =
    useState<string | null>(null);

  const [recoveredReceiptError, setRecoveredReceiptError] =
    useState<string | null>(null);

  const [messages, setMessages] = useState<ChatMessage[]>([
    {
      id: 1,
      role: 'assistant',
      content: 'Xin chào! Tôi là DeFi Copilot.',
    },
    {
      id: 2,
      role: 'user',
      content: 'Avalanche là gì?',
    },
    {
      id: 3,
      role: 'assistant',
      content: 'Avalanche là một nền tảng blockchain.',
    },
  ]);

    const messagesContainerRef = useRef<HTMLDivElement>(null);
    useEffect(() => {
    const container = messagesContainerRef.current;

    if (container) {
        container.scrollTop = container.scrollHeight;
    }
    }, [messages]);

    async function handleSend() {
      const text = draft.trim();

      if (!text || isLoading) return;

      if (!ENABLE_DEV_API_TEST) {
        setApiError('Chat API is disabled.');
        return;
      }

      // Kiểm tra ví ở cả development và production
      if (!isConnected || !address) {
        setApiError('Please connect Core Wallet first.');
        return;
      }

      if (chainId !== 43113) {
        setApiError('Please switch to Avalanche Fuji.');
        return;
      }

      setApiError(null);
      setIsLoading(true);
      setDraft('');

      // Hiển thị tin nhắn người dùng
      setMessages((prev) => [
        ...prev,
        {
          id: Date.now(),
          role: 'user',
          content: text,
        },
      ]);

      try {
        // Gọi Backend thật
        const response = await sendChatMessage({
          session_id: sessionId,
          wallet_address: address,
          chain_id: 43113,
          locale: 'vi',
          message: text,
        });

        // Hiển thị phản hồi AI
        if (typeof response.message === 'string' &&
            response.message.trim()) {
          setMessages((prev) => [
            ...prev,
            {
              id: Date.now() + 1,
              role: 'assistant',
              content: response.message,
            },
          ]);
        }

        // Đọc pending_action từ Backend
        const candidate: unknown = response.pending_action;

        if (!candidate) {
          setPendingAction(null);
          setIsMockAction(false);
          setIsConfirmationOpen(false);
          return;
        }

        const nextAction = candidate as PendingAction;
        const summary = nextAction.arguments?.summary;

        // Chỉ hiển thị modal nếu payload đủ dữ liệu review
        const isValidAction =
          Boolean(nextAction.pending_action_id) &&
          /^[0-9a-f]{64}$/.test(
            nextAction.normalized_intent_hash ?? ''
          ) &&
          nextAction.chain_id === 43113 &&
          nextAction.wallet_address?.toLowerCase() ===
            address.toLowerCase() &&
          Boolean(
            nextAction.expires_at &&
            Date.parse(nextAction.expires_at) > Date.now()
          ) &&
          Boolean(
            summary?.token_in?.symbol &&
            summary?.token_out?.symbol &&
            summary?.amount_in?.raw &&
            summary?.min_amount_out?.raw &&
            summary?.recipient_address &&
            Number.isSafeInteger(summary?.deadline)
          );

        if (!isValidAction) {
          setPendingAction(null);
          setIsMockAction(false);
          setIsConfirmationOpen(false);
          setApiError(
            'Backend returned an incomplete pending action. ' +
            'Transaction signing is disabled.'
          );
          return;
        }

        // Đây là pending_action từ API, không phải MOCK
        setPendingAction(nextAction);
        setIsMockAction(false);
        setIsConfirmationOpen(true);

      } catch (error) {
        setApiError(
          error instanceof Error
            ? error.message
            : 'Unable to connect to Backend.'
        );
      } finally {
        setIsLoading(false);
      }
    }

  async function handleRecoverSubmitHash() {
    if (
      !recoveredSwap ||
      recoveredSwap.sessionId !== sessionId ||
      !isConnected ||
      !address ||
      chainId !== 43113 ||
      address.toLowerCase() !==
        recoveredSwap.walletAddress.toLowerCase() ||
      isRecoveringHash ||
      isSigning ||
      recoveredHashRecorded
    ) {
      return;
    }

    setIsRecoveringHash(true);
    setRecoveryError(null);

    try {
      const result = await submitHashWithWallet(
        recoveredSwap.actionId,
        {
          session_id: recoveredSwap.sessionId,
          review_hash: recoveredSwap.reviewHash,
          wallet_address: recoveredSwap.walletAddress,
          transaction_hash: recoveredSwap.transactionHash,
        }
      );

      // Backend phải trả về đúng action và hash
      if (
        result.pending_action_id !== recoveredSwap.actionId ||
        result.transaction_hash.toLowerCase() !==
          recoveredSwap.transactionHash.toLowerCase()
      ) {
        throw new Error(
          'Backend returned a different transaction.'
        );
      }

      setRecoveryStatus(result.status);
      setRecoveredHashRecorded(true);
    } catch (error) {
      setRecoveryError(
        error instanceof Error
          ? error.message
          : 'Unable to recover transaction hash.'
      );
    } finally {
      setIsRecoveringHash(false);
    }
  }

  async function handleRecoverRefreshReceipt() {
    if (
      !recoveredSwap ||
      recoveredSwap.sessionId !== sessionId ||
      !recoveredHashRecorded ||
      !isConnected ||
      !address ||
      chainId !== 43113 ||
      address.toLowerCase() !==
        recoveredSwap.walletAddress.toLowerCase() ||
      isRecoveringReceipt ||
      isSigning ||
      recoveredReceiptStatus === 'mined'
    ) {
      return;
    }

    setIsRecoveringReceipt(true);
    setRecoveredReceiptError(null);

    try {
      const result = await refreshReceiptWithWallet(
        recoveredSwap.actionId,
        {
          session_id: recoveredSwap.sessionId,
          review_hash: recoveredSwap.reviewHash,
          wallet_address: recoveredSwap.walletAddress,
        }
      );

      // Phải khớp giao dịch đã lưu
      if (
        result.pending_action_id !== recoveredSwap.actionId ||
        result.transaction_hash.toLowerCase() !==
          recoveredSwap.transactionHash.toLowerCase()
      ) {
        throw new Error(
          'Backend returned a different transaction receipt.'
        );
      }

      setRecoveredReceiptStatus(result.status);
    } catch (error) {
      setRecoveredReceiptError(
        error instanceof Error
          ? error.message
          : 'Unable to refresh saved transaction receipt.'
      );
    } finally {
      setIsRecoveringReceipt(false);
    }
  }

  return (
    <section className="chatbot">
      <ChatWelcome onSelect={setDraft} />

      {import.meta.env.DEV && (
        <button
          type="button"
          onClick={() => {
            const nextAction = pendingAction
              ? null
              : prepareSwapMock.pending_action;

            setPendingAction(nextAction);
            setIsMockAction(nextAction !== null);
          }}
        >
          {pendingAction
            ? 'Hide MOCK Transaction'
            : 'Show MOCK Transaction'}
        </button>
      )}

      {import.meta.env.DEV && isMockAction && pendingAction && (
        <button
          type="button"
          onClick={() => setIsConfirmationOpen(true)}
        >
          Open MOCK Modal
        </button>
      )}

      <div
        className="chat-messages"
        ref={messagesContainerRef}
      >
        <PortfolioRiskCard />
        <TransactionCard
          pendingAction={pendingAction}
          isMock={isMockAction}
        />

        {messages.map((message) => (
          <div
            key={message.id}
            className={`chat-bubble ${message.role}`}
          >
            {message.content}
          </div>
        ))}
      </div>

      {recoveredSwap &&
        recoveredSwap.sessionId === sessionId && (
          <div className="pending-swap-recovery" role="status">
            <h3>Previous Swap Detected</h3>

            <p>
              A transaction hash was saved before this page
              was reloaded.
            </p>

            <p>
              <strong>Transaction Hash:</strong>
            </p>

            <p style={{ overflowWrap: 'anywhere' }}>
              <code>{recoveredSwap.transactionHash}</code>
            </p>

            <p>
              <strong>Receipt:</strong> Not verified.
            </p>

            {address?.toLowerCase() !==
              recoveredSwap.walletAddress.toLowerCase() ||
            chainId !== 43113 ? (
              <p role="alert">
                Connect the original wallet on Avalanche Fuji
                to continue recovery.
              </p>
            ) : (
              <p>
                Original wallet connected. Recovery information
                is available.
              </p>
            )}

            <p>
              Do not send the swap again before checking
              the previous transaction.
            </p>

            <button
              type="button"
              onClick={() => void handleRecoverSubmitHash()}
              disabled={
                !isConnected ||
                !address ||
                chainId !== 43113 ||
                address.toLowerCase() !==
                  recoveredSwap.walletAddress.toLowerCase() ||
                isRecoveringHash ||
                isSigning ||
                recoveredHashRecorded
              }
            >
              {isRecoveringHash || isSigning
                ? 'Signing / Recovering...'
                : recoveredHashRecorded
                  ? 'Hash Recorded'
                  : 'Submit Saved Hash'}
            </button>

            {recoveryError && (
              <p role="alert">{recoveryError}</p>
            )}

            {recoveryStatus && (
              <p role="status">
                Backend status: {recoveryStatus}
              </p>
            )}

            {recoveredHashRecorded && recoveredSwap && (
              <div className="recovery-receipt-section">
                <button
                  type="button"
                  onClick={() => void handleRecoverRefreshReceipt()}
                  disabled={
                    !isConnected ||
                    !address ||
                    chainId !== 43113 ||
                    address.toLowerCase() !==
                      recoveredSwap.walletAddress.toLowerCase() ||
                    isRecoveringReceipt ||
                    isSigning ||
                    recoveredReceiptStatus === 'mined'
                  }
                >
                  {isRecoveringReceipt || isSigning
                    ? 'Checking Receipt...'
                    : recoveredReceiptStatus === 'mined'
                      ? 'Receipt Verified'
                      : 'Refresh Saved Receipt'}
                </button>

                {recoveredReceiptError && (
                  <p role="alert">{recoveredReceiptError}</p>
                )}

                {recoveredReceiptStatus && (
                  <p role="status">
                    {recoveredReceiptStatus === 'mined'
                      ? 'Backend verified the mined transaction.'
                      : `Transaction status: ${recoveredReceiptStatus}`}
                  </p>
                )}
              </div>
            )}

          </div>
        )}

        {apiError && (
          <p role="alert" className="chat-api-error">
            {apiError}
          </p>
        )}

    <div className="dc-chat-composer">
      <div className="chat-input-row">
      <input
          id="chat-input"
          type="text"
          value={draft}
          onChange={(event) => setDraft(event.target.value)}
          onKeyDown={(event) => {
              if (
                  event.key === 'Enter' &&
                  !event.nativeEvent.isComposing
              ) {
                  handleSend();
              }
          }}
          placeholder="Nhập câu hỏi về Avalanche..."
      />

      <button
          type="button"
          onClick={handleSend}
          disabled={!draft.trim()}
      >
          Send
      </button>
      </div>
    </div>

    <ConfirmationModal
      isOpen={
        isConfirmationOpen &&
        pendingAction !== null
      }
      pendingAction={pendingAction}
      sessionId={sessionId}
      isMock={isMockAction}
      onClose={() => setIsConfirmationOpen(false)}
    />

    </section>
  );
}

export default Chatbot;