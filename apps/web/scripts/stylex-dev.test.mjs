import assert from 'node:assert/strict';
import { test } from 'node:test';
import { fileURLToPath } from 'node:url';
import { createServer } from 'vite';

const root = fileURLToPath(new URL('../', import.meta.url));
const indicator = fileURLToPath(new URL('../../../packages/ui/src/activity-indicator.tsx', import.meta.url));

test('cold dev CSS resolves shared constants before their browser import arrives', async () => {
  const server = await createServer({
    root, configFile: `${root}/vite.config.ts`, logLevel: 'silent',
    // Reproduce a slow token import deterministically instead of racing the browser.
    server: { host: '127.0.0.1', port: 0, preTransformRequests: false },
  });
  try {
    await server.listen();
    const origin = `http://127.0.0.1:${server.httpServer.address().port}`;
    const module = await fetch(`${origin}/@fs/${indicator}`);
    assert.equal(module.status, 200);
    const css = await fetch(`${origin}/virtual:stylex.css`);
    assert.equal(css.status, 200, await css.clone().text());
    const text = await css.text();
    assert.match(text, /@media\s*\(prefers-reduced-motion:\s*reduce\)/);
    assert.doesNotMatch(text, /var\(--[^)]+\)\s*\{/);
  } finally { await server.close(); }
});
