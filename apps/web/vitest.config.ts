import { fileURLToPath } from 'node:url';
import { defineConfig } from 'vitest/config';
import stylex from '@stylexjs/unplugin';
import react from '@vitejs/plugin-react';

export default defineConfig({
  // Tests need compilation, without the Vite adapter's live-CSS server timer.
  plugins: [stylex.rollup({ runtimeInjection: false, useCSSLayers: {before: ['whip-reset']}, unstable_moduleResolution: { type: 'commonJS', rootDir: fileURLToPath(new URL('../../', import.meta.url)) } }), react()],
  // Desktop renderer contracts reuse this React/StyleX harness at the consumer boundary.
  test: { include: ['packages/app/test/**/*.test.{ts,tsx}', 'apps/desktop/renderer-tests/**/*.test.tsx'], environment: 'jsdom', setupFiles: ['packages/app/test/setup.ts'], restoreMocks: true },
});
