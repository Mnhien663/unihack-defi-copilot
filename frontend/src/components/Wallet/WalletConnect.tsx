
import {
  useConnection,
  useConnect,
  useDisconnect,
  useSwitchChain,
} from 'wagmi';

const FUJI_CHAIN_ID = 43113;

function WalletConnect() {
  const { address, isConnected, chainId, connector } = useConnection();

  const {
    connect,
    connectors,
    isPending: isConnecting,
    error: connectError,
  } = useConnect();

  const { disconnect } = useDisconnect();

  const {
    switchChain,
    isPending: isSwitching,
    error: switchError,
  } = useSwitchChain();

  const isWrongChain =
    isConnected && chainId !== FUJI_CHAIN_ID;

  // Khi chưa kết nối ví
  if (!isConnected) {
    return (
      <div className="dc-wallet-status">
        <button
          type="button"
          onClick={() => {
            if (connectors[0]) {
              connect({ connector: connectors[0] });
            }
          }}
          disabled={isConnecting || connectors.length === 0}
        >
          {isConnecting ? 'Connecting...' : 'Connect Wallet'}
        </button>

        {connectError && (
          <p role="alert">{connectError.message}</p>
        )}
      </div>
    );
  }

  // Khi đã kết nối ví
  return (
    <div className="dc-wallet-status">
      <span>
        {address?.slice(0, 6)}...{address?.slice(-4)}
      </span>

      <small>
        Provider: {connector?.name ?? 'Unknown'}
        {' | '}
        Chain ID: {chainId ?? 'Unknown'}
      </small>

      {isWrongChain ? (
        <button
          type="button"
          onClick={() => switchChain({ chainId: FUJI_CHAIN_ID })}
          disabled={isSwitching}
        >
          {isSwitching ? 'Switching...' : 'Switch to Fuji'}
        </button>
      ) : (
        <span>Avalanche Fuji ✓</span>
      )}

      <button type="button" onClick={() => disconnect()}>
        Disconnect
      </button>

      {switchError && (
        <p role="alert">{switchError.message}</p>
      )}
    </div>
  );
}

export default WalletConnect;
