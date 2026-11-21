import './PortfolioBreakdown.css';

type PortfolioBreakdownProps = {
  onOpenChat: () => void;
};

type Asset = {
  symbol: string;
  name: string;
  quantity: string;
  price: string;
  value: string;
  allocation: number;
  tone: 'avax' | 'usdc' | 'savax';
};

// UI illustration only. None of these figures are read from a wallet or API.
const demoAssets: Asset[] = [
  { symbol: 'AVAX', name: 'Avalanche', quantity: '24.50 AVAX', price: '$35.00', value: '$857.50', allocation: 49.6, tone: 'avax' },
  { symbol: 'USDC', name: 'USD Coin', quantity: '450.00 USDC', price: '$1.00', value: '$450.00', allocation: 26.0, tone: 'usdc' },
  { symbol: 'sAVAX', name: 'Staked AVAX', quantity: '12.00 sAVAX', price: '$35.00', value: '$420.00', allocation: 24.4, tone: 'savax' },
];

export default function PortfolioBreakdown({ onOpenChat }: PortfolioBreakdownProps) {
  return (
    <section className="portfolio-page" aria-labelledby="portfolio-title">
      <div className="portfolio-page-heading">
        <div>
          <div className="portfolio-heading-line">
            <h1 id="portfolio-title">Portfolio Breakdown</h1>
            <span className="portfolio-demo-label">SIMULATED DATA</span>
          </div>
          <p>Illustrative asset distribution on Avalanche Fuji Testnet.</p>
        </div>
        <span className="portfolio-network-label">Avalanche Fuji · 43113</span>
      </div>

      <div className="portfolio-hero-border">
        <section className="portfolio-hero" aria-label="Portfolio overview">
          <div className="portfolio-hero-top">
            <span className="portfolio-eyebrow">TOTAL PORTFOLIO VALUE</span>
            <span className="portfolio-change">+4.2% · Demo 24h change</span>
          </div>
          <div className="portfolio-value">$1,727.50 <span>USD</span></div>
          <p className="portfolio-hero-note">
            Example values for interface preview — not your connected wallet balance.
          </p>
          <div className="portfolio-metrics">
            <div className="portfolio-metric"><span>AVAX SPOT · DEMO</span><strong>$35.00</strong></div>
            <div className="portfolio-metric"><span>ASSETS</span><strong>3 Tokens</strong></div>
            <div className="portfolio-metric"><span>HEALTH FACTOR · DEMO</span><strong>2.35</strong></div>
          </div>
        </section>
      </div>

      <div className="portfolio-overview-grid">
        <section className="portfolio-panel portfolio-allocation" aria-labelledby="portfolio-allocation-title">
          <div className="portfolio-panel-heading">
            <h2 id="portfolio-allocation-title">Asset Allocation</h2>
            <span>3 example assets</span>
          </div>
          <div className="portfolio-allocation-body">
            <div
              className="portfolio-donut"
              role="img"
              aria-label="Simulated asset allocation: AVAX 49.6%, USDC 26%, sAVAX 24.4%"
            >
              <div className="portfolio-donut-center">
                <small>TOTAL · DEMO</small>
                <strong>$1,727</strong>
              </div>
            </div>
            <div className="portfolio-legend">
              {demoAssets.map((asset) => (
                <div className="portfolio-legend-row" key={asset.symbol}>
                  <span className={`portfolio-token-dot portfolio-token-dot--${asset.tone}`} />
                  <span className="portfolio-legend-name">{asset.symbol}</span>
                  <span className="portfolio-legend-value">
                    <strong>{asset.value}</strong>
                    <small>{asset.allocation.toFixed(1)}%</small>
                  </span>
                </div>
              ))}
            </div>
          </div>
          <div className="portfolio-exposure">
            <div className="portfolio-exposure-text"><span>74.0% AVAX / sAVAX</span><span>26.0% USDC</span></div>
            <div className="portfolio-exposure-track"><span /></div>
          </div>
        </section>

        <section className="portfolio-panel portfolio-insight" aria-labelledby="portfolio-insight-title">
          <div className="portfolio-panel-heading">
            <h2 id="portfolio-insight-title">Copilot Portfolio Intelligence</h2>
            <span className="portfolio-insight-tag">EXAMPLE INSIGHT</span>
          </div>
          <div className="portfolio-insight-content">
            <strong>Understand your asset exposure</strong>
            <p>
              In this example portfolio, AVAX and sAVAX represent 74% of total value.
              A concentrated position may be more sensitive to AVAX price movements.
            </p>
            <p className="portfolio-insight-disclaimer">
              Illustrative explanation only — not live AI analysis or investment advice.
            </p>
          </div>
          <button type="button" className="portfolio-chat-button" onClick={onOpenChat}>
            Discuss in AI Chat <span aria-hidden="true">→</span>
          </button>
        </section>
      </div>

      <section className="portfolio-panel portfolio-holdings" aria-labelledby="portfolio-holdings-title">
        <div className="portfolio-panel-heading">
          <h2 id="portfolio-holdings-title">Holdings &amp; Positions</h2>
          <span>3 simulated assets</span>
        </div>
        <div className="portfolio-table-scroll">
          <table className="portfolio-table">
            <thead><tr><th scope="col">Asset</th><th scope="col">Balance</th><th scope="col">Price (Demo)</th><th scope="col">Position Value</th><th scope="col">Allocation</th></tr></thead>
            <tbody>
              {demoAssets.map((asset) => (
                <tr key={asset.symbol}>
                  <td><div className="portfolio-asset-cell"><span className={`portfolio-token-icon portfolio-token-icon--${asset.tone}`}>{asset.symbol === 'AVAX' ? 'A' : asset.symbol === 'USDC' ? '$' : 'S'}</span><span><strong>{asset.symbol}</strong><small>{asset.name}</small></span></div></td>
                  <td className="portfolio-number">{asset.quantity}</td>
                  <td className="portfolio-number">{asset.price}</td>
                  <td className="portfolio-number portfolio-bold">{asset.value}</td>
                  <td><div className="portfolio-allocation-percent">{asset.allocation.toFixed(1)}%</div><div className="portfolio-progress"><span className={`portfolio-progress-fill portfolio-progress-fill--${asset.tone}`} style={{ width: `${asset.allocation}%` }} /></div></td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        <p className="portfolio-table-note">SIMULATED DATA · No balances, prices, or positions on this page have been fetched from a wallet.</p>
      </section>
    </section>
  );
}
