
import "./PortfolioRiskCard.css";

const assets = [
  {
    symbol: "AVAX",
    amount: "24.50",
    value: "$857.50",
    color: "#d93639",
  },
  {
    symbol: "USDC",
    amount: "450.00",
    value: "$450.00",
    color: "#2563eb",
  },
  {
    symbol: "sAVAX",
    amount: "12.00",
    value: "$420.00",
    color: "#9333ea",
  },
];

export default function PortfolioRiskCard() {
  return (
    <section className="dc-portfolio-card">
      <div className="dc-portfolio-header">
        <div>
          <h3>Portfolio & Risk Assessment</h3>
          <p>Simulated data for UI demonstration.</p>
        </div>

        <span className="dc-demo-label">
          DEMO DATA
        </span>
      </div>

      <div className="dc-assets-grid">
        {assets.map((asset) => (
          <div
            className="dc-asset"
            key={asset.symbol}
          >
            <div className="dc-asset-title">
              <span
                className="dc-token-icon"
                style={{
                  backgroundColor: asset.color,
                }}
              >
                {asset.symbol[0]}
              </span>

              <strong>{asset.symbol}</strong>
            </div>

            <strong className="dc-asset-amount">
              {asset.amount}
            </strong>

            <span className="dc-asset-value">
              ≈ {asset.value} (DEMO)
            </span>
          </div>
        ))}
      </div>

      <div className="dc-health-card">
        <div className="dc-health-score">
          2.35
        </div>

        <div>
          <strong>Lending Health Factor</strong>
          <p>
            Example only. Not a live risk assessment.
          </p>
        </div>

        <span className="dc-health-demo">
          SIMULATED
        </span>
      </div>
    </section>
  );
}
