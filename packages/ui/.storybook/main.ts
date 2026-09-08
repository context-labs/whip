import type { StorybookConfig } from '@storybook/react-vite';
import stylex from '@stylexjs/unplugin';
import { fileURLToPath } from 'node:url';
const config: StorybookConfig = {
  stories: ['../stories/**/*.stories.tsx'],
  framework: '@storybook/react-vite',
  addons: ['@storybook/addon-a11y'],
  viteFinal(config) {
    config.plugins = [stylex.vite({useCSSLayers: {before: ['whip-reset']}, runtimeInjection: false, unstable_moduleResolution: {type: 'commonJS', rootDir: fileURLToPath(new URL('../../../', import.meta.url))}}), ...(config.plugins ?? [])];
    return config;
  },
};
export default config;
