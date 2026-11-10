import { svelte } from '@sveltejs/vite-plugin-svelte'
import tailwindcss from '@tailwindcss/vite'
import { defineConfig } from 'vitest/config'

export default defineConfig({
  plugins: [svelte(), tailwindcss()],
  build: { outDir: 'dist/app' },
  server: { proxy: { '/api': 'http://localhost:8080' } },
  test: { environment: 'node' },
})
