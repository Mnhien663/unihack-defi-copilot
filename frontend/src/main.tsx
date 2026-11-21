
import React from 'react';
import { createRoot } from 'react-dom/client';
import { WagmiProvider } from 'wagmi';
import {
  QueryClient,
  QueryClientProvider,
} from '@tanstack/react-query';

import App from './App';
import { wagmiConfig } from './config/wagmi';

// Quản lý dữ liệu và cache cho các React Query hooks
const queryClient = new QueryClient();

const rootElement = document.getElementById('root');

if (!rootElement) {
  throw new Error('Không tìm thấy phần tử root');
}

createRoot(rootElement).render(
  <React.StrictMode>
    <WagmiProvider config={wagmiConfig}>
      <QueryClientProvider client={queryClient}>
        <App />
      </QueryClientProvider>
    </WagmiProvider>
  </React.StrictMode>
);
