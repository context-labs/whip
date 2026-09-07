import { fileURLToPath } from 'node:url';
import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import stylex from '@stylexjs/unplugin';
import { tanstackRouter } from '@tanstack/router-plugin/vite';

export default defineConfig({
  plugins: [
    tanstackRouter({ target: 'react', routesDirectory: '../../packages/app/src/routes', generatedRouteTree: '../../packages/app/src/routeTree.gen.ts', autoCodeSplitting: true }),
    stylex.vite({
      useCSSLayers: true, runtimeInjection: false,
      unstable_moduleResolution: { type: 'commonJS', rootDir: fileURLToPath(new URL('../../', import.meta.url)) },
    }),
    react(),
  ],
  // StyleX source packages skip prebundling; their CommonJS store shims must not.
  optimizeDeps: { include: ['use-sync-external-store/shim', 'use-sync-external-store/shim/with-selector'] },
  server: {
    strictPort: true,
    ...(process.env.WHIP_WEB_DAEMON ? { proxy: { '/api': { target: process.env.WHIP_WEB_DAEMON, ws: true, changeOrigin: true } } } : {}),
  },
  build: { target: 'es2022', sourcemap: false, assetsInlineLimit: 0 },
});
