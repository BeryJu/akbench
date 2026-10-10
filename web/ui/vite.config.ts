import react from '@vitejs/plugin-react';
import { defineConfig } from 'vitest/config';

// The dev server proxies the API (including the shell's WebSocket) to a
// running cmd/apiserver, so the browser only ever talks to one origin - the
// same shape as production, where cmd/apiserver serves this app's build
// output itself (internal/api/static.go). No CORS setup needed for dev.
const apiTarget = process.env.BENCH_API_URL ?? 'http://localhost:8080';

export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      '/api': { target: apiTarget, ws: true, changeOrigin: true },
      '/healthz': { target: apiTarget },
    },
  },
  build: {
    outDir: 'dist',
    // Monaco is large but lazy-loaded (components/CodeEditor.tsx), so it
    // never sits on the initial page load.
    chunkSizeWarningLimit: 4000,
  },
  test: {
    environment: 'node',
  },
});
