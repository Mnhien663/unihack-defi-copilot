import './TransactionCard.css';

export type SwapAsset = {
  chain_id?: number;
  address?: string;
  symbol: string;
  decimals?: number;
};

export type PendingAction = {
  pending_action_id?: string;
  arguments: {
    summary: {
      token_in: SwapAsset;
      token_out: SwapAsset;

      amount_in: {
        raw: string;
        formatted: string;
      };
      min_amount_out: {
        raw: string;
        formatted: string;
      };
      recipient_address: string;
      deadline: number;
      owner_address?: string;
      manager_address?: string;
      quote_expires_at?: string;
    };
  };
  expires_at?: string;
  chain_id?: number;
  wallet_address?: string;
  normalized_intent_hash?: string;
};

type TransactionCardProps = {
  pendingAction: PendingAction | null;
  isMock?: boolean;
};

function TransactionCard({
  pendingAction,
  isMock = false,
}: TransactionCardProps) {
  // Không có pending_action thì không hiển thị Review.
  if (!pendingAction) return null;

  const summary = pendingAction.arguments.summary;

  const hasValidId = Boolean(pendingAction.pending_action_id);

    const expiresAt = pendingAction.expires_at
    ? new Date(pendingAction.expires_at).getTime()
    : NaN;

    const isExpired =
    !Number.isFinite(expiresAt) || Date.now() >= expiresAt;

  // Backend trả deadline dưới dạng Unix timestamp (giây).
  const deadline = new Date(
    summary.deadline * 1000
  ).toLocaleString('vi-VN', {
    timeZone: 'Asia/Ho_Chi_Minh',
  });

  return (
    <section className="transaction-review">
      <h3>Transaction Review</h3>

      {isMock && <p>MOCK DATA — Preview only</p>}

      {/* Main information */}
      <p>
        <strong>You send: </strong>
        {summary.amount_in.formatted} {summary.token_in.symbol}
      </p>

      <p>
        <strong>Minimum received: </strong>
        {summary.min_amount_out.formatted}{' '}
        {summary.token_out.symbol}
      </p>

      <p>
        <strong>Network: </strong>
        {pendingAction.chain_id === 43113
          ? 'Avalanche Fuji (43113)'
          : pendingAction.chain_id != null
            ? `Chain ID ${pendingAction.chain_id}`
            : 'Not provided'}
      </p>

      <p>
        <strong>Recipient: </strong>
        <span title={summary.recipient_address}>
          {summary.recipient_address.slice(0, 6)}
          ...
          {summary.recipient_address.slice(-4)}
        </span>
      </p>

      <p>
        <strong>Deadline: </strong>
        {deadline}
      </p>

      {/* Additional technical information */}
      <details className="transaction-details">
        <summary>View details</summary>

        <div className="transaction-details-content">
          <p>
            <strong>Wallet: </strong>
            {pendingAction.wallet_address ?? 'Not provided'}
          </p>

          <p>
            <strong>Manager Contract: </strong>
            {summary.manager_address ?? 'Not provided'}
          </p>

          <p>
            <strong>Quote expires: </strong>
            {summary.quote_expires_at &&
            Number.isFinite(Date.parse(summary.quote_expires_at))
              ? new Date(
                  summary.quote_expires_at
                ).toLocaleString('vi-VN', {
                  timeZone: 'Asia/Ho_Chi_Minh',
                })
              : 'Not provided'}
          </p>

          <p>
            <strong>Slippage tolerance: </strong>
            Not provided
          </p>

          <p>
            <strong>Estimated gas: </strong>
            Not available
          </p>

          <p>
            <strong>Spender (approval): </strong>
            Not provided
          </p>

          <p>
            <strong>Quote source: </strong>
            Not provided
          </p>
        </div>
      </details>

      {/* Safety status */}
      <p>
        {!hasValidId
          ? 'Missing action ID — Cannot confirm.'
          : isExpired
            ? 'Action expired — Cannot confirm.'
            : isMock
              ? 'MOCK DATA — Signing is disabled.'
              : 'Review only — Awaiting confirmation API.'}
      </p>
    </section>
  );

}

export default TransactionCard;
