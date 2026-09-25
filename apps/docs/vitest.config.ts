import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'
import stylex from '@stylexjs/unplugin'
import { fileURLToPath } from 'node:url'
export default defineConfig({
  plugins: [stylex.rollup({ runtimeInjection: false, aliases: { '~/*': ['/ROOT/apps/docs/src/*'] }, unstable_moduleResolution: { type: 'commonJS', rootDir: fileURLToPath(new URL('../../', import.meta.url)) } }), react()],
  resolve: { alias: { '~': fileURLToPath(new URL('./src', import.meta.url)) } },
  test: { environment: 'jsdom', include: ['src/**/*.test.{ts,tsx}', 'tests/**/*.test.{ts,tsx,mjs}'], restoreMocks: true },
})
