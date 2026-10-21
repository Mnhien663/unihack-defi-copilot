
import { useState, useRef, useEffect } from 'react';
import './Chatbot.css';

type ChatMessage = {
  id: number;
  role: 'user' | 'assistant';
  content: string;
};

function Chatbot() {
  const [draft, setDraft] = useState('');
  
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

  function handleSend() {
    const text = draft.trim();

    if (!text) return;

    setMessages((previous) => [
      ...previous,
      {
        id: previous.length + 1,
        role: 'user',
        content: text,
      },
    ]);

    setDraft('');
  }

  return (
    <section className="chatbot">
      <h2>Chat với DeFi Copilot</h2>

      <div
        className="chat-messages"
        ref={messagesContainerRef}
      >
        {messages.map((message) => (
          <div
            key={message.id}
            className={`chat-bubble ${message.role}`}
          >
            {message.content}
          </div>
        ))}
      </div>

      
<label htmlFor="chat-input">Tin nhắn</label>

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


    <p>Bạn đang nhập: {draft}</p>
    </section>
  );
}

export default Chatbot;