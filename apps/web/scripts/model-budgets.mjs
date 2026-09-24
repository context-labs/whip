import assert from 'node:assert/strict';
import { mkdir } from 'node:fs/promises';
import { join } from 'node:path';
import { chromium, firefox } from '@playwright/test';
import { createWhipClient } from '../../../packages/sdk/dist/index.js';
import { eventually, startFixture } from '../../../packages/sdk/scripts/fixture.mjs';

// Run after npm run pack:web. The real protocol and production UI use a fresh
// temporary daemon, without a user's runtime home or provider credentials.
const output = process.env.WHIP_BUDGET_RESULTS ?? '/tmp/whip-model-budget-browser';
await mkdir(output, { recursive: true });
const fixture = await startFixture();
const client = createWhipClient({ endpoint: fixture.info.endpoint, clientKind: 'human', clientId: 'budget-browser' });
try {
  await client.connect();
  const created = await client.sessions.create({ cwd: fixture.directory, model: 'model', provider: 'provider' }).result({ signal: AbortSignal.timeout(15_000) });
  assert.equal(created.status, 'succeeded');
  const rootId = created.result.root_id;
  const session = client.session(rootId);
  const snapshot = await session.snapshot();
  for (const kind of ['cost', 'tokens', 'elapsed']) {
    const state = snapshot.budgets.find(item => item.agent_id === '' && item.state.kind === kind).state;
    assert.equal(state.limit, null);
    assert.equal(state.remaining, null);
    assert.equal(state.used, '0');
    assert.equal(state.uncertain, '0');
    assert.equal(state.incomplete, false);
  }
  assert.equal(snapshot.budgets.find(item => item.state.kind === 'active_operations').state.limit, '64');
  const origin = fixture.info.endpoint.replace(/^ws/, 'http').replace('/api/v3/ws', '');
  const route = `${origin}/h/${fixture.info.runtime_id}/s/${rootId}?panel=limits`;
  for (const [name, engine] of [['chromium', chromium], ['firefox', firefox]]) {
    const browser = await engine.launch({ headless: true });
    try {
      for (const colorScheme of ['dark', 'light']) {
        for (const width of [1280, 390]) {
          const context = await browser.newContext({ viewport: { width, height: 960 }, colorScheme });
          try {
            const page = await context.newPage();
            const errors = [];
            const commands = [];
            page.on('pageerror', error => errors.push(error.message));
            page.on('websocket', socket => socket.on('framesent', ({ payload }) => {
              const frame = JSON.parse(String(payload));
              if (frame.method === 'command.submit') commands.push(frame.params.operation);
            }));
            const response = await page.goto(route);
            assert.equal(response.status(), 200);
            const details = page.getByRole('dialog', { name: 'Session details', exact: true });
            await details.getByRole('heading', { name: 'Usage', exact: true }).waitFor();
            await eventually(async () => await details.getByText('Unlimited', { exact: true }).count() === 3);
            assert.ok(await details.getByText('$0.000000 used · $0.000000 in flight', { exact: true }).isVisible());
            assert.equal(await details.getByRole('button', { name: 'Set cap', exact: true }).count(), 0);
            assert.equal(await details.getByLabel('Budget agent', { exact: true }).count(), 0);
            assert.equal(commands.includes('budget.cap'), false);
            await page.screenshot({ path: join(output, `${name}-${colorScheme}-${width}.png`) });
            await page.reload();
            await eventually(async () => await details.getByText('Unlimited', { exact: true }).count() === 3);
            assert.deepEqual(errors, []);
            console.log(`${name} ${colorScheme} ${width}px: read-only unlimited usage and reload passed`);
          } finally {
            await context.close();
          }
        }
      }
    } finally {
      await browser.close();
    }
  }
} finally {
  client.close();
  await fixture.close();
}
