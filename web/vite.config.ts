import { svelte } from '@sveltejs/vite-plugin-svelte'
import tailwindcss from '@tailwindcss/vite'
import { defineConfig } from 'vitest/config'

export default defineConfig({
  plugins: [svelte(), tailwindcss()],
  // Emit fonts as files: the server's CSP does not allow data: fonts.
  build: { outDir: 'dist/app', assetsInlineLimit: 0 },
  server: { proxy: { '/api': 'http://localhost:8080' } },
  test: { environment: 'node' },
})
