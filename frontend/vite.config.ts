import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

// The build goes to ../web/dist, which the Go binary embeds.
// In dev (`npm run dev`), /api calls are proxied to the Go server on :8000.
export default defineConfig({
  plugins: [react()],
  build: {
    outDir: '../web/dist',
    emptyOutDir: true,
  },
  server: {
    port: 5173,
    strictPort: true,
    open: true,
    proxy: { '/api': 'http://127.0.0.1:8000' },
  },
});
