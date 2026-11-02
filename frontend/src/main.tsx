/**
 * Mount the client application with browser-based routing and global styles.
 * StrictMode keeps React development checks enabled; effects must tolerate cleanup
 * and remounting. Authentication restoration starts inside App.
 */
import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { BrowserRouter } from 'react-router-dom';
import App from './App';
import './styles/fonts.css';
import './styles/index.css';

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <BrowserRouter>
      <App />
    </BrowserRouter>
  </StrictMode>,
);
