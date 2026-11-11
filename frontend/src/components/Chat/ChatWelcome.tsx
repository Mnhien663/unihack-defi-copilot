
import "./ChatWelcome.css";

type ChatWelcomeProps = {
  onSelect: (prompt: string) => void;
};

const suggestions = [
  {
    label: "Check wallet balance",
    prompt: "What is my current wallet balance?",
  },
  {
    label: "Understand health factor",
    prompt: "Can you explain my lending health factor?",
  },
  {
    label: "Review portfolio risk",
    prompt: "Can you analyze my portfolio risk?",
  },
  {
    label: "Prepare a Swap",
    prompt: "How can I prepare a token swap using Avalanche DeFi?",
  },
];

export default function ChatWelcome({
  onSelect,
}: ChatWelcomeProps) {
  return (
    <section className="dc-chat-welcome">
      <div className="dc-welcome-card">
        <div className="dc-welcome-icon">✦</div>

        <div>
          <h2>Welcome to Avalanche DeFi Copilot</h2>
          <p>
            Ask questions about your wallet, DeFi positions
            and potential risks on Avalanche Defi Testnet.
          </p>
        </div>
      </div>

      <div className="dc-suggestions">
        {suggestions.map((item) => (
          <button
            key={item.label}
            type="button"
            className="dc-suggestion-button"
            onClick={() => onSelect(item.prompt)}
          >
            {item.label}
          </button>
        ))}
      </div>
    </section>
  );
}
