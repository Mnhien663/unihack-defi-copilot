import './App.css';
import Header from './components/layout/Header';
import Sidebar from './components/layout/Sidebar';
import Footer from './components/layout/Footer';

function App() {
  return (
    <div className="app-layout">

      <Sidebar />

      <div className="main-area">

        <Header />

        <main className="content">
          <h2>Welcome to Avalanche!</h2>
          <p>Your AI-powered DeFi assistant.</p>
        </main>

        <Footer />

      </div>
    </div>
  );
}

export default App;
