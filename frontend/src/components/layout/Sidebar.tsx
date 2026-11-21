
import './Sidebar.css';

export type SidebarPage =
  | 'chat'
  | 'portfolio'
  | 'risk'
  | 'history';

type SidebarProps = {
  activePage: SidebarPage;
  onNavigate: (page: SidebarPage) => void;
};

export function Sidebar({
  activePage,
  onNavigate,
}: SidebarProps) {
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

        <nav
          className="dc-nav"
          aria-label="Main navigation"
        >
          <button
            type="button"
            className={`dc-nav-item ${
              activePage === 'chat' ? 'dc-nav-active' : ''
            }`}
            aria-current={
              activePage === 'chat' ? 'page' : undefined
            }
            onClick={() => onNavigate('chat')}
          >
            <span>◉</span>
            AI Chat
          </button>

          <button
            type="button"
            className={`dc-nav-item ${
              activePage === 'portfolio' ? 'dc-nav-active' : ''
            }`}
            aria-current={
              activePage === 'portfolio' ? 'page' : undefined
            }
            onClick={() => onNavigate('portfolio')}
          >
            <span>◫</span>
            Portfolio Breakdown
          </button>

          <button
            type="button"
            className={`dc-nav-item ${
              activePage === 'risk' ? 'dc-nav-active' : ''
            }`}
            aria-current={
              activePage === 'risk' ? 'page' : undefined
            }
            onClick={() => onNavigate('risk')}
          >
            <span>◇</span>
            Risk &amp; Health Analysis
          </button>

          <button
            type="button"
            className={`dc-nav-item ${
              activePage === 'history' ? 'dc-nav-active' : ''
            }`}
            aria-current={
              activePage === 'history' ? 'page' : undefined
            }
            onClick={() => onNavigate('history')}
          >
            <span>◷</span>
            Transaction History
          </button>
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
