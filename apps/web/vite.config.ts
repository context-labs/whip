import { fileURLToPath } from 'node:url';
import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import stylex from '@stylexjs/unplugin';
import { tanstackRouter } from '@tanstack/router-plugin/vite';

export default defineConfig({
  plugins: [
    {
      name: 'whip-renderer-boundary',
      generateBundle() {
        for (const id of this.getModuleIds()) {
          const name = id.replaceAll('\\', '/');
          if (name.startsWith('node:') || name.includes('__vite-browser-external') ||
              /(?:^|\/)electron(?:\/|$)/.test(name) || /packages\/sdk\/(?:src|dist)\/node\.[cm]?[jt]s$/.test(name))
            this.error(`Native module entered the shared renderer: ${id}`);
        }
      },
    },
    tanstackRouter({ target: 'react', routesDirectory: '../../packages/app/src/routes', generatedRouteTree: '../../packages/app/src/routeTree.gen.ts', autoCodeSplitting: true }),
    stylex.vite({
      useCSSLayers: {before: ['whip-reset']}, runtimeInjection: false,
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
