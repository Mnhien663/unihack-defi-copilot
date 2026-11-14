
import { useState, useRef, useEffect } from 'react';
import './Chatbot.css';
import ChatWelcome from "./ChatWelcome";
import PortfolioRiskCard from "../Portfolio/PortfolioRiskCard";
import { sendChatMessage } from '../../services/chatApi';
import TransactionCard, { type PendingAction } from '../Transaction/TransactionCard';
import prepareSwapMock from '../../mocks/prepareSwapMock.json';
import ConfirmationModal from '../Transaction/ConfirmationModal';

// Chỉ bật khi kiểm thử API cùng Dev 1
const ENABLE_DEV_API_TEST = false;

// Ví mẫu từ Dev 1, KHÔNG phải ví người dùng đã kết nối
const DEV_TEST_WALLET =
  '0x4DFb0d91C07A7C706F65F974953cE8b111D27Ee5';

type ChatMessage = {
  id: number;
  role: 'user' | 'assistant';
  content: string;
};

function Chatbot() {
  const [draft, setDraft] = useState('');

  const [pendingAction, setPendingAction] =
  useState<PendingAction | null>(null);
  const [isMockAction, setIsMockAction] = useState(false);

  const [isConfirmationOpen, setIsConfirmationOpen] = useState(false);

  const [isLoading, setIsLoading] = useState(false);
  const [apiError, setApiError] = useState<string | null>(null);
  const sessionIdRef = useRef<string>(crypto.randomUUID());
  
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

  // Hiển thị tin nhắn người dùng
  setMessages((previous) => [
    ...previous,
    {
      id: Date.now(),
      role: 'user',
      content: text,
    },
  ]);

  setDraft('');
  setApiError(null);

  // Chưa bật API: giữ hành vi chat thử nghiệm hiện tại
  if (!import.meta.env.DEV || !ENABLE_DEV_API_TEST) {
    return;
  }

  setIsLoading(true);

  try {
    const response = await sendChatMessage({
      session_id: sessionIdRef.current,
      wallet_address: DEV_TEST_WALLET,
      chain_id: 43113,
      locale: 'vi',
      message: text,
    });

    // Hiển thị câu trả lời Backend
    if (response.message) {
      setMessages((previous) => [
        ...previous,
        {
          id: Date.now(),
          role: 'assistant',
          content: response.message,
        },
      ]);
    }

    // Quan trọng: nhận pending_action từ Backend
    setPendingAction(response.pending_action);
    setIsMockAction(false);

  } catch (error) {
    setApiError(
      error instanceof Error
        ? error.message
        : 'Không thể kết nối Backend.'
    );
  } finally {
    setIsLoading(false);
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
        isMockAction &&
        pendingAction !== null
      }
      pendingAction={pendingAction}
      onClose={() => setIsConfirmationOpen(false)}
    />

    </section>
  );
}

export default Chatbot;