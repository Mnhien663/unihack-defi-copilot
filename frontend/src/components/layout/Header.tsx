
import "./Header.css";
import WalletConnect from '../Wallet/WalletConnect';

export function Header() {
  return (
    <header className="dc-header">
      <div className="dc-header-title">
        <h1>DeFi Copilot</h1>
        <p>AI Assistant</p>
      </div>

      <div className="dc-header-actions">
        <div className="dc-network-badge">
          <span className="dc-network-dot" />
          <span>Avalanche Defi</span>
        </div>

        <WalletConnect />
      </div>
    </header>
  );
}

export default Header;
