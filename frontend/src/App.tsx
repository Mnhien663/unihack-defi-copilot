import './components/Chat/ChatStitchPolish.css';
import { useState } from 'react';

import './App.css';
import './styles/tokens.css';

import Header from './components/layout/Header';
import Sidebar, {
  type SidebarPage,
} from './components/layout/Sidebar';

import Chatbot from './components/Chat/Chatbot';
import PortfolioBreakdown from './components/Portfolio/PortfolioBreakdown';
import RiskHealthAnalysis from './components/Risk/RiskHealthAnalysis';
import TransactionHistory from './components/Transaction/TransactionHistory';

function App() {
  const [activePage, setActivePage] =
    useState<SidebarPage>('chat');

  const openChat = () => setActivePage('chat');

  return (
    <div className="app-layout">
      <Sidebar
        activePage={activePage}
        onNavigate={setActivePage}
      />

      <div className="main-area">
        <Header />

        <main className="content">
          {/* Preserve chatbot state between pages */}
          <div
            className="chat-page"
            hidden={activePage !== 'chat'}
          >
            <Chatbot />
          </div>

          {activePage === 'portfolio' && (
            <PortfolioBreakdown onOpenChat={openChat} />
          )}

          {activePage === 'risk' && (
            <RiskHealthAnalysis onOpenChat={openChat} />
          )}

          {activePage === 'history' && (
            <TransactionHistory />
          )}
        </main>
      </div>
    </div>
  );
}

export default App;
