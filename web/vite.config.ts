import tailwindcss from '@tailwindcss/vite'
import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'

// En desarrollo, Vite sirve el frontend en :5173 y redirige /api al backend Go en :8080.
// En producción, el build (dist/) se embebe dentro del binario de Go.
export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: {
    proxy: {
      '/api': {
        target: 'http://localhost:8080',
        ws: true, // para logs y métricas en tiempo real (WebSockets)
      },
    },
  },
})
