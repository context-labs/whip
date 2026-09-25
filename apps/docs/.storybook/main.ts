import type { StorybookConfig } from '@storybook/react-vite';
import { docsMdx } from '../scripts/mdx-plugins.mjs';

const config: StorybookConfig = {
  stories: ['../src/**/*.stories.tsx'],
  addons: ['@storybook/addon-a11y'],
  core: { disableTelemetry: true },
  framework: { name: '@storybook/react-vite', options: { builder: { viteConfigPath: '.storybook/vite.config.ts' } } },
  async viteFinal(config) {
    // Don't load TanStack Start or the production route compiler in Storybook.
    config.plugins = [docsMdx(), ...(config.plugins || [])];
    return config;
  },
};
export default config;
