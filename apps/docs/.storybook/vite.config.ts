import { fileURLToPath } from 'node:url';
import { defineConfig } from 'vite';
import stylex from '@stylexjs/unplugin';

export default defineConfig({
  plugins: [stylex.vite({ runtimeInjection: false, aliases: { '~/*': ['/ROOT/apps/docs/src/*'] }, unstable_moduleResolution: { type: 'commonJS', rootDir: fileURLToPath(new URL('../../..', import.meta.url)) } })],
});
