import { defineConfig } from '@playwright/test';
import { fileURLToPath } from 'node:url';

// macOS rasterization is the reviewed reference. Never update these from Linux.
if (process.platform !== 'darwin') throw new Error('WHIP visual baselines require macOS. Linux CI runs the portable UI interaction/Axe/CSP suites; it does not establish this visual gate.');
if (!process.env.WHIP_UI_VISUAL_ORIGIN) throw new Error('Run npm run test:visual -w @whip/ui so the isolated Storybook server is started.');

export default defineConfig({
  testDir: fileURLToPath(new URL('./', import.meta.url)),
  testMatch: 'visual.spec.mjs',
  snapshotPathTemplate: '{testDir}/visual-baselines/macos/{arg}{ext}',
  outputDir: fileURLToPath(new URL('../ui-test-results/visual/', import.meta.url)),
  updateSnapshots: 'none',
  workers: 1,
  retries: 0,
  reporter: [['line'], ['json', { outputFile: fileURLToPath(new URL('../ui-test-results/visual-report.json', import.meta.url)) }]],
  use: {
    browserName: 'chromium', headless: true,
    baseURL: process.env.WHIP_UI_VISUAL_ORIGIN,
    viewport: { width: 1080, height: 960 }, deviceScaleFactor: 1,
    reducedMotion: 'reduce', locale: 'en-US', timezoneId: 'UTC',
    screenshot: 'only-on-failure', trace: 'retain-on-failure',
  },
  expect: { toHaveScreenshot: { animations: 'disabled', caret: 'hide', scale: 'css', threshold: 0.1, maxDiffPixels: 0 } },
});
