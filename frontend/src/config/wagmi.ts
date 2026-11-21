
import { createConfig, http } from 'wagmi';
import { avalancheFuji } from 'wagmi/chains';
import { injected } from 'wagmi/connectors';

export const wagmiConfig = createConfig({
  chains: [avalancheFuji],

  connectors: [injected()],

  transports: {
    [avalancheFuji.id]: http(
      'https://api.avax-test.network/ext/bc/C/rpc'
    ),
  },
});
