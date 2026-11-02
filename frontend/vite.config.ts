/**
 * Vite integration, source alias, local API proxy, and vendor chunk boundaries.
 * NX_BACKEND_URL is read from the process environment at configuration load time.
 * The frontend uses same-origin /api requests; static hosting must provide an
 * equivalent reverse proxy, while Vite preview inherits the server proxy.
 */
import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import tailwindcss from '@tailwindcss/vite';
import path from 'node:path';

const BACKEND = process.env.NX_BACKEND_URL ?? 'http://localhost:8080';

export default defineConfig({
  plugins: [tailwindcss(), react()],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, 'src'),
    },
  },
  server: {
    port: 5173,
    /**
     * Proxy same-origin REST and SSE requests to the separately started API.
     * Vite preview uses this configuration unless a preview-specific proxy overrides it.
     */
    proxy: {
      '/api': { target: BACKEND, changeOrigin: true },
      '/healthz': { target: BACKEND, changeOrigin: true },
    },
  },
  build: {
    sourcemap: false,
    chunkSizeWarningLimit: 1200,
    rollupOptions: {
      output: {
        /**
         * Keep large shared libraries in stable vendor groups instead of duplicating
         * them across the application entry chunk.
         */
        manualChunks: {
          gsap: ['gsap'],
          markdown: ['react-markdown', 'remark-gfm'],
          vendor: ['react', 'react-dom', 'react-router-dom', 'zustand'],
        },
      },
    },
  },
});
