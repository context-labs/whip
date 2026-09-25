import { defineConfig, devices } from '@playwright/test'
export default defineConfig({
  testDir: './tests/browser',
  fullyParallel: true,
  timeout: 30000,
  forbidOnly: Boolean(process.env.CI),
  retries: process.env.CI ? 1 : 0,
  reporter: 'list',
  use: { baseURL: 'http://127.0.0.1:3101', trace: 'retain-on-failure' },
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
  webServer: [
    { command: 'node scripts/preview.mjs', url: 'http://127.0.0.1:3101', reuseExistingServer: false, timeout: 15000 },
    { command: 'node scripts/preview.mjs --storybook --port 6008', url: 'http://127.0.0.1:6008/iframe.html', reuseExistingServer: false, timeout: 15000 },
  ],
})
