
import "./Sidebar.css";

export function Sidebar() {
  return (
    <aside className="dc-sidebar">
      <div className="dc-sidebar-top">
        <div className="dc-brand">
          <div className="dc-brand-icon">◈</div>

          <div>
            <strong>DeFi Copilot</strong>
            <p>Avalanche DeFi Assistant</p>
          </div>
        </div>

        <button
          className="dc-new-chat"
          type="button"
          disabled
          title="New Conversation will be enabled later"
        >
          + New Conversation
        </button>

        <nav className="dc-nav" aria-label="Main navigation">
          <div className="dc-nav-item dc-nav-active">
            <span>◉</span>
            AI Chat
          </div>

          <div className="dc-nav-item dc-nav-disabled">
            <span>◫</span>
            Portfolio Breakdown
          </div>

          <div className="dc-nav-item dc-nav-disabled">
            <span>◇</span>
            Risk &amp; Health Analysis
          </div>

          <div className="dc-nav-item dc-nav-disabled">
            <span>◷</span>
            Transaction History
          </div>
        </nav>
      </div>

      <div className="dc-sidebar-bottom">
        <div className="dc-sidebar-network">
          <span className="dc-sidebar-dot" />
          Avalanche Fuji Testnet
        </div>

        <div className="dc-sidebar-secondary">
          <span>⚙ Settings</span>
          <span>▤ Docs</span>
        </div>
      </div>
    </aside>
  );
}

export default Sidebar;
