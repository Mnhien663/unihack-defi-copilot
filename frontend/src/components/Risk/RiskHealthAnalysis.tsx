import { useRef, useState } from 'react';
import './RiskHealthAnalysis.css';

type RiskHealthAnalysisProps = {
  onOpenChat: () => void;
};

type Shock = 0 | -15 | -30 | -50;

// UI-only sample data. It is NOT fetched from a wallet, oracle, protocol, or backend.
const DEMO = {
  healthFactor: 2.35,
  avaxPrice: 35,
  collateral: 1277.5,
  borrow: 543.6,
  avaxCollateral: 857.5,
  savaxCollateral: 420,
};

const SHOCKS: Shock[] = [0, -15, -30, -50];
const money = (value: number) =>
  value.toLocaleString('en-US', { style: 'currency', currency: 'USD' });

export default function RiskHealthAnalysis({ onOpenChat }: RiskHealthAnalysisProps) {
  const [shock, setShock] = useState<Shock>(0);
  const stressTestRef = useRef<HTMLElement | null>(null);

  // Illustrative sensitivity calculation, NOT a protocol-specific risk model.
  // Assumes all example collateral changes proportionally to the AVAX price shock,
  // while the example debt remains fixed in USD.
  const multiplier = 1 + shock / 100;
  const simulatedPrice = DEMO.avaxPrice * multiplier;
  const simulatedCollateral = DEMO.collateral * multiplier;
  const simulatedHF = DEMO.healthFactor * multiplier;
  const level = simulatedHF <= 1 ? 'critical' : simulatedHF < 1.5 ? 'caution' : 'healthy';
  const levelText = level === 'critical' ? 'Illustrative liquidation range'
    : level === 'caution' ? 'Illustrative caution range' : 'Illustrative healthy range';
  const circumference = 2 * Math.PI * 68;
  const gaugeFraction = Math.min(DEMO.healthFactor / 3, 1);

  return (
    <section className="risk-page" aria-labelledby="risk-page-title">
      <header className="risk-page-header">
        <div>
          <div className="risk-heading-line">
            <h1 id="risk-page-title">Risk &amp; Health Analysis</h1>
            <span className="risk-demo-tag">SIMULATED DATA</span>
          </div>
          <p>Explore how a sample lending position responds to changes in AVAX price.</p>
        </div>
        <button
          type="button"
          className="risk-jump-button"
          onClick={() => stressTestRef.current?.scrollIntoView({ behavior: 'smooth', block: 'start' })}
        >
          Simulate Drawdown <span aria-hidden="true">↘</span>
        </button>
      </header>

      {/* Main hero follows the green / lavender / blue Stitch treatment. */}
      <section className="risk-hero" aria-labelledby="risk-hero-title">
        <div className="risk-hero-left">
          <div className="risk-health-gauge" role="img" aria-label="Example health factor 2.35 on a scale of zero to three">
            <svg viewBox="0 0 160 160" aria-hidden="true">
              <circle cx="80" cy="80" r="68" fill="none" stroke="#dce6fc" strokeWidth="12" />
              <circle cx="80" cy="80" r="68" fill="none" stroke="#15b782"
                strokeWidth="12" strokeLinecap="round" strokeDasharray={`${circumference * gaugeFraction} ${circumference}`}
                transform="rotate(-90 80 80)" />
            </svg>
            <div className="risk-gauge-text">
              <span>HEALTH FACTOR</span>
              <strong>2.35</strong>
              <small>Example · Healthy</small>
            </div>
          </div>
          <div className="risk-gauge-facts">
            <div><span>Liquidation reference</span><strong>1.00 HF</strong></div>
            <div><span>Margin above 1.00</span><strong>+1.35 HF</strong></div>
            <div><span>Example AVAX price</span><strong>$35.00</strong></div>
          </div>
        </div>
        <div className="risk-hero-right">
          <h2 id="risk-hero-title">Understand your lending safety margin</h2>
          <p>This example position has a Health Factor of 2.35. As collateral value drops, its illustrative Health Factor may also fall. These values are not a live risk assessment.</p>
          <div className="risk-hero-stats">
            <div><span>ILLUSTRATIVE COLLATERAL</span><strong>{money(DEMO.collateral)}</strong><small>AVAX + sAVAX</small></div>
            <div><span>ILLUSTRATIVE DEBT</span><strong>{money(DEMO.borrow)}</strong><small>USDC</small></div>
            <div><span>RISK STATE</span><strong className="risk-healthy-text">Healthy</strong><small>Based on example HF</small></div>
          </div>
          <p className="risk-hero-disclaimer">Example position · Avalanche Fuji Testnet · No real oracle polling</p>
        </div>
      </section>

      <section className="risk-panel risk-spectrum-panel" aria-labelledby="risk-spectrum-title">
        <div className="risk-section-heading">
          <h2 id="risk-spectrum-title">Health Factor &amp; Liquidation Reference</h2>
          <span>Illustrative scale: 0 – 3.00+</span>
        </div>
        <div className="risk-spectrum">
          <div className="risk-spectrum-marker" style={{ left: `${Math.min(DEMO.healthFactor / 3 * 100, 96)}%` }}>
            <span>Current example: 2.35</span>
          </div>
          <div className="risk-spectrum-track" aria-label="Red: at or below one; amber: one to 1.5; green: above 1.5">
            <span className="risk-spectrum-red" /><span className="risk-spectrum-amber" /><span className="risk-spectrum-green" />
          </div>
        </div>
        <div className="risk-spectrum-legend">
          <span><i className="risk-dot risk-dot--red" /> ≤ 1.00 · Liquidation reference</span>
          <span><i className="risk-dot risk-dot--amber" /> 1.00–1.50 · Caution</span>
          <span><i className="risk-dot risk-dot--green" /> &gt; 1.50 · Higher buffer</span>
        </div>
      </section>

      <div className="risk-positions-grid">
        <section className="risk-panel" aria-labelledby="risk-collateral-title">
          <div className="risk-panel-heading">
            <div><span className="risk-eyebrow">TOTAL COLLATERAL SUPPLIED · DEMO</span><h2 id="risk-collateral-title">{money(DEMO.collateral)}</h2></div>
            <span className="risk-soft-pill">2 Assets</span>
          </div>
          <div className="risk-token-list">
            <div className="risk-token-row">
              <span className="risk-token-symbol risk-token-symbol--avax">A</span>
              <div><strong>Avalanche (AVAX)</strong><small>24.50 AVAX × $35.00</small></div>
              <strong>{money(DEMO.avaxCollateral)}</strong>
            </div>
            <div className="risk-token-row">
              <span className="risk-token-symbol risk-token-symbol--savax">S</span>
              <div><strong>Staked AVAX (sAVAX)</strong><small>12.00 sAVAX × $35.00</small></div>
              <strong>{money(DEMO.savaxCollateral)}</strong>
            </div>
          </div>
          <p className="risk-small-note">Sample collateral composition for UI preview only.</p>
        </section>

        <section className="risk-panel" aria-labelledby="risk-borrow-title">
          <div className="risk-panel-heading">
            <div><span className="risk-eyebrow">TOTAL BORROWED LIABILITIES · DEMO</span><h2 id="risk-borrow-title">{money(DEMO.borrow)}</h2></div>
            <span className="risk-soft-pill">USDC Debt</span>
          </div>
          <div className="risk-token-row">
            <span className="risk-token-symbol risk-token-symbol--usdc">$</span>
            <div><strong>USD Coin (USDC)</strong><small>Example debt balance</small></div>
            <strong>543.60 USDC</strong>
          </div>
          <div className="risk-borrow-track-heading"><span>Debt relative to example collateral</span><strong>42.6%</strong></div>
          <div className="risk-borrow-track"><span /></div>
          <div className="risk-borrow-footer"><span>Collateral: {money(DEMO.collateral)}</span><span>Debt: {money(DEMO.borrow)}</span></div>
          <p className="risk-small-note">This ratio is for visualization, not a protocol borrowing limit.</p>
        </section>
      </div>

      <div className="risk-bottom-grid">
        <section ref={stressTestRef} className="risk-panel risk-stress-panel" aria-labelledby="risk-stress-title">
          <div className="risk-section-heading">
            <h2 id="risk-stress-title">Stress-Test Drawdown Simulator</h2>
            <span className="risk-soft-pill">UI Sandbox</span>
          </div>
          <p className="risk-muted">Change the sample AVAX price to explore how the example position might respond.</p>
          <span className="risk-eyebrow">AVAX PRICE SHOCK SCENARIO</span>
          <div className="risk-shock-buttons" role="group" aria-label="Choose illustrative AVAX price change">
            {SHOCKS.map((value) => (
              <button type="button" key={value}
                className={`risk-shock-button ${shock === value ? 'risk-shock-button--selected' : ''}`}
                aria-pressed={shock === value} onClick={() => setShock(value)}>
                {value === 0 ? 'Baseline (0%)' : `${value}% Shock`}
              </button>
            ))}
          </div>
          <div className="risk-stress-result" aria-live="polite">
            <div className="risk-stress-result-header"><span>Simulated Health Degradation</span><span className={`risk-state-tag risk-state-tag--${level}`}>{levelText}</span></div>
            <div className="risk-stress-metrics">
              <div><span>Simulated AVAX Price</span><strong>{money(simulatedPrice)}</strong></div>
              <div><span>Collateral (Demo)</span><strong>{money(simulatedCollateral)}</strong></div>
              <div><span>Resulting HF</span><strong className={`risk-result-hf--${level}`}>{simulatedHF.toFixed(2)}</strong></div>
            </div>
            <p>{level === 'critical' ? 'This simplified example crosses the illustrative 1.00 liquidation reference.' : level === 'caution' ? 'In this scenario the example position has less margin above the liquidation reference.' : 'In this simplified scenario the example position remains above the caution range.'}</p>
          </div>
          <p className="risk-small-note">Illustrative linear sensitivity only. Real Aave risk depends on asset prices, collateral settings, liquidation thresholds, and live debt. Do not use these values to make financial decisions.</p>
        </section>

        <section className="risk-panel risk-insight-panel" aria-labelledby="risk-insight-title">
          <div className="risk-section-heading">
            <h2 id="risk-insight-title">Copilot Risk Assessment</h2>
            <span className="risk-soft-pill">EXAMPLE INSIGHT</span>
          </div>
          <div className="risk-insight-message">
            <strong>How to interpret Health Factor 2.35</strong>
            <p>In this simulated position, a higher Health Factor indicates more room before the illustrative liquidation reference of 1.00.</p>
          </div>
          <div className="risk-insight-message risk-insight-message--alt">
            <strong>What to monitor</strong>
            <p>Asset price changes, outstanding debt, collateral eligibility, and protocol-specific liquidation rules can all change your actual risk.</p>
          </div>
          <p className="risk-small-note">Static example explanation — not a live AI recommendation or automated protection service.</p>
          <button type="button" className="risk-chat-button" onClick={onOpenChat}>Discuss in AI Chat <span aria-hidden="true">→</span></button>
        </section>
      </div>
    </section>
  );
}
