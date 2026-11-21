import { useMemo, useState } from 'react';
import './TransactionHistory.css';

type TransactionKind = 'swap' | 'lending' | 'staking' | 'borrow';
type TransactionFilter = 'all' | TransactionKind;

type DemoTransaction = {
  id: string;
  kind: TransactionKind;
  title: string;
  protocol: string;
  date: string;
  time: string;
  assets: string;
  sent: string;
  received: string;
  hashPreview: string;
  gas: string;
  route: string;
  note: string;
};

// UI PREVIEW ONLY. These are invented examples, not wallet records or on-chain hashes.
const SAMPLE_TRANSACTIONS: DemoTransaction[] = [
  {
    id: 'demo-swap-01', kind: 'swap', title: 'Token Swap', protocol: 'Trader Joe V2',
    date: '20 Nov 2026', time: '14:32', assets: 'AVAX → USDC',
    sent: '−5.00 AVAX', received: '+174.20 USDC',
    hashPreview: '0x8a3f…4e1b', gas: '0.0018 AVAX (example)',
    route: 'AVAX → Trader Joe → USDC',
    note: 'Illustrative swap preview. No real tokens were exchanged.',
  },
  {
    id: 'demo-lend-02', kind: 'lending', title: 'Supply Collateral', protocol: 'Aave V3',
    date: '20 Nov 2026', time: '13:46', assets: 'USDC → aUSDC',
    sent: '−250.00 USDC', received: '+250.00 aUSDC',
    hashPreview: '0x3c7e…982a', gas: '0.0031 AVAX (example)',
    route: 'USDC → Example lending pool',
    note: 'Illustrative collateral supply; no deposit was executed.',
  },
  {
    id: 'demo-stake-03', kind: 'staking', title: 'Liquid Stake', protocol: 'BENQI',
    date: '20 Nov 2026', time: '11:21', assets: 'AVAX → sAVAX',
    sent: '−12.50 AVAX', received: '+11.89 sAVAX',
    hashPreview: '0x7f20…e0ff', gas: '0.0024 AVAX (example)',
    route: 'AVAX → Example liquid staking',
    note: 'Illustrative staking record; no staking action was executed.',
  },
  {
    id: 'demo-borrow-04', kind: 'borrow', title: 'Borrow Asset', protocol: 'Example Lending',
    date: '20 Nov 2026', time: '07:15', assets: 'USDC',
    sent: '—', received: '+60.00 USDC',
    hashPreview: '0x11a5…e8dd', gas: '0.0039 AVAX (example)',
    route: 'Example lending pool → USDC',
    note: 'Illustrative borrowing event; no debt was created.',
  },
  {
    id: 'demo-swap-05', kind: 'swap', title: 'Token Swap', protocol: 'Example DEX',
    date: '19 Nov 2026', time: '22:04', assets: 'USDC → AVAX',
    sent: '−85.00 USDC', received: '+2.46 AVAX',
    hashPreview: '0x52f8…094b', gas: '0.0014 AVAX (example)',
    route: 'USDC → Example exchange → AVAX',
    note: 'Illustrative swap preview. No real tokens were exchanged.',
  },
];

const FILTERS: Array<{ id: TransactionFilter; label: string }> = [
  { id: 'all', label: 'All' },
  { id: 'swap', label: 'Swaps' },
  { id: 'lending', label: 'Lending' },
  { id: 'staking', label: 'Staking' },
  { id: 'borrow', label: 'Borrow' },
];

function typeSymbol(kind: TransactionKind): string {
  if (kind === 'swap') return '⇄';
  if (kind === 'lending') return '↧';
  if (kind === 'staking') return '◇';
  return '↗';
}

export default function TransactionHistory() {
  const [filter, setFilter] = useState<TransactionFilter>('all');
  const [search, setSearch] = useState('');
  const [selectedId, setSelectedId] = useState(SAMPLE_TRANSACTIONS[0].id);

  const filteredTransactions = useMemo(() => {
    const needle = search.trim().toLowerCase();
    return SAMPLE_TRANSACTIONS.filter((transaction) => {
      if (filter !== 'all' && transaction.kind !== filter) return false;
      if (!needle) return true;
      return [
        transaction.title, transaction.protocol, transaction.assets,
        transaction.sent, transaction.received, transaction.hashPreview,
      ].some((value) => value.toLowerCase().includes(needle));
    });
  }, [filter, search]);

  const selected = filteredTransactions.find((tx) => tx.id === selectedId)
    ?? filteredTransactions[0];

  return (
    <section className="history-page" aria-labelledby="history-title">
      <div className="history-signature">
        <div className="history-signature-main">
          <div className="history-title-line">
            <h1 id="history-title">Transaction History</h1>
            <span className="history-demo-tag">SIMULATED DATA</span>
          </div>
          <p>Browse example DeFi activity in a transaction ledger layout.</p>
          <p className="history-notice">
            Preview only: these entries, timestamps, fees and hash snippets are not actual blockchain transactions.
          </p>
        </div>
        <div className="history-summary" aria-label="Demo overview">
          <div><span>Example entries</span><strong>{SAMPLE_TRANSACTIONS.length}</strong></div>
          <div><span>Verified on-chain</span><strong>0</strong></div>
          <div><span>Target network</span><strong>Fuji · 43113</strong></div>
        </div>
      </div>

      <div className="history-toolbar">
        <div className="history-filters" role="group" aria-label="Filter example transactions">
          {FILTERS.map((item) => {
            const count = item.id === 'all'
              ? SAMPLE_TRANSACTIONS.length
              : SAMPLE_TRANSACTIONS.filter((tx) => tx.kind === item.id).length;
            return (
              <button
                type="button"
                key={item.id}
                className={`history-filter ${filter === item.id ? 'history-filter--active' : ''}`}
                aria-pressed={filter === item.id}
                onClick={() => setFilter(item.id)}
              >
                {item.label} <span>{count}</span>
              </button>
            );
          })}
        </div>
        <label className="history-search">
          <span className="history-visually-hidden">Search example transactions</span>
          <span aria-hidden="true">⌕</span>
          <input
            type="search"
            value={search}
            onChange={(event) => setSearch(event.target.value)}
            placeholder="Search token, protocol, or hash…"
          />
        </label>
      </div>

      <div className="history-content-grid">
        <section className="history-ledger" aria-labelledby="history-ledger-title">
          <div className="history-ledger-heading">
            <h2 id="history-ledger-title">Example Transactions</h2>
            <span>{filteredTransactions.length} shown</span>
          </div>

          {filteredTransactions.length > 0 ? (
            <div className="history-table-scroll">
              <table className="history-table">
                <thead>
                  <tr>
                    <th scope="col">Date</th>
                    <th scope="col">Type / Protocol</th>
                    <th scope="col">Assets</th>
                    <th scope="col">Amount Flow</th>
                    <th scope="col">Status / Sample Hash</th>
                  </tr>
                </thead>
                <tbody>
                  {filteredTransactions.map((transaction) => (
                    <tr
                      key={transaction.id}
                      className={selected?.id === transaction.id ? 'history-row--selected' : undefined}
                    >
                      <td className="history-date-cell">
                        <strong>{transaction.date}</strong>
                        <small>{transaction.time} · demo</small>
                      </td>
                      <td>
                        <button
                          className="history-transaction-select"
                          type="button"
                          onClick={() => setSelectedId(transaction.id)}
                          aria-pressed={selected?.id === transaction.id}
                          aria-label={`View example details for ${transaction.title}, ${transaction.date} ${transaction.time}`}
                        >
                          <span className={`history-type-icon history-type-icon--${transaction.kind}`} aria-hidden="true">
                            {typeSymbol(transaction.kind)}
                          </span>
                          <span className="history-type-name">
                            <strong>{transaction.title}</strong>
                            <small>{transaction.protocol}</small>
                          </span>
                        </button>
                      </td>
                      <td><span className="history-assets">{transaction.assets}</span></td>
                      <td className="history-amount-cell">
                        <span>{transaction.sent}</span>
                        <strong>{transaction.received}</strong>
                      </td>
                      <td className="history-status-cell">
                        <span className="history-status-tag">Example</span>
                        <code>{transaction.hashPreview}</code>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          ) : (
            <div className="history-empty">
              <strong>No matching example transactions</strong>
              <p>Try another search or reset the filters.</p>
              <button type="button" onClick={() => { setFilter('all'); setSearch(''); }}>
                Reset filters
              </button>
            </div>
          )}

          <div className="history-ledger-footer">
            All rows are illustrative. No wallet activity has been fetched for this page.
          </div>
        </section>

        <aside className="history-inspector-column" aria-label="Example transaction details">
          {selected ? (
            <div className="history-inspector-frame">
              <div className="history-inspector">
                <div className="history-inspector-top">
                  <div>
                    <span className="history-inspector-eyebrow">TRANSACTION DETAILS · DEMO</span>
                    <h2>{selected.title}</h2>
                    <p>{selected.date} · {selected.time}</p>
                  </div>
                  <span className="history-status-tag">Simulated</span>
                </div>

                <div className="history-hash-panel">
                  <span>Sample Hash (not on-chain)</span>
                  <code>{selected.hashPreview}</code>
                </div>

                <div className="history-flow-panel">
                  <span className="history-inspector-eyebrow">ILLUSTRATIVE ASSET FLOW</span>
                  <div className="history-flow-values">
                    <div><small>Sent</small><strong>{selected.sent}</strong></div>
                    <span className="history-flow-arrow" aria-hidden="true">→</span>
                    <div><small>Received</small><strong>{selected.received}</strong></div>
                  </div>
                </div>

                <dl className="history-detail-grid">
                  <div><dt>Protocol (example)</dt><dd>{selected.protocol}</dd></div>
                  <div><dt>Gas fee (example)</dt><dd>{selected.gas}</dd></div>
                  <div className="history-detail-wide"><dt>Illustrative route</dt><dd>{selected.route}</dd></div>
                </dl>

                <div className="history-inspector-message" role="note">
                  {selected.note} This sample has no block-explorer record.
                </div>
              </div>
            </div>
          ) : (
            <div className="history-inspector history-inspector-empty">
              Select a matching example transaction to view its details.
            </div>
          )}

          <div className="history-insight-note">
            <strong>Copilot explanation</strong>
            <p>In a live integration, this area can explain transaction status and fees using verified data. This screen currently shows design examples only.</p>
          </div>
        </aside>
      </div>
    </section>
  );
}
