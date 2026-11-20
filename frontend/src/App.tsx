import './App.css';
import './styles/tokens.css';

import Header from './components/layout/Header';
import Sidebar from './components/layout/Sidebar';

import Chatbot from './components/Chat/Chatbot';

function App() {
  return (
    <div className="app-layout">

      <Sidebar />

      <div className="main-area">

        <Header />

        <main className="content">
          <Chatbot />
        </main>

      </div>
    </div>
  );
}

export default App;
