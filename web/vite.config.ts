import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

// Dev: Vite serves the client on :5173 and proxies API/WS/files to the Go
// server on :8080, so the browser sees a single origin in both modes.
export default defineConfig({
  plugins: [react(), tailwindcss()],
  build: {
    outDir: '../internal/web/dist',
    emptyOutDir: true,
  },
  server: {
    proxy: {
      '/api': { target: 'http://localhost:8080', ws: true },
      '/files': 'http://localhost:8080',
      '/avatars': 'http://localhost:8080',
      '/healthz': 'http://localhost:8080',
    },
  },
})
