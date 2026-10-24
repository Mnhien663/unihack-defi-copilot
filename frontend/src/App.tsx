import './App.css';

import Header from './components/layout/Header';
import Sidebar from './components/layout/Sidebar';
import Footer from './components/layout/Footer';

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

        <Footer />

      </div>
    </div>
  );
}

export default App;
