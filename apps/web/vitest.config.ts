import { fileURLToPath } from 'node:url';
import { defineConfig } from 'vitest/config';
import stylex from '@stylexjs/unplugin';
import react from '@vitejs/plugin-react';

export default defineConfig({
  // Tests need compilation, without the Vite adapter's live-CSS server timer.
  plugins: [stylex.rollup({ runtimeInjection: false, useCSSLayers: true, unstable_moduleResolution: { type: 'commonJS', rootDir: fileURLToPath(new URL('../../', import.meta.url)) } }), react()],
  test: { include: ['packages/app/test/**/*.test.{ts,tsx}'], environment: 'jsdom', setupFiles: ['packages/app/test/setup.ts'], restoreMocks: true },
});
