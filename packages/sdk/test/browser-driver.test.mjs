import assert from 'node:assert/strict';
import { test } from 'node:test';
import { Client, DeliveryError } from '../dist/index.js';
const initial = { major: 4, minor: 0, runtime_id: 'runtime', process_epoch: 'boot', network_client: false, builtins: [] };
test('browser driver settings preserve pinned and configured values and never replay a lost CAS', async () => {
 const requests = [];
 let lost = false;
 const client = await Client.connect(async request => {
  if (request.method === 'initialize') return { jsonrpc: '2.0', id: request.id, result: initial };
  requests.push(request);
  if (lost) throw new DeliveryError('lost settings acknowledgement');
  return { jsonrpc: '2.0', id: request.id, result: { revision: 'a'.repeat(64), configured_driver: 'rod', driver: 'chromedp', pinned: true } };
 }, { clientID: 'settings' });
 const result = await client.hosts.browserDriver();
 assert.equal(result.configured_driver, 'rod');
 assert.equal(result.driver, 'chromedp');
 assert.equal(result.pinned, true);
 await client.hosts.setBrowserDriver(result.revision, 'chromedp');
 assert.deepEqual(requests.at(-1).params, { expected_revision: result.revision, driver: 'chromedp' });
 for (const invalid of ['', 'other', 'CHROMEDP']) await assert.rejects(client.hosts.setBrowserDriver(result.revision, invalid), TypeError);
 assert.equal(requests.length, 2);
 lost = true;
 await assert.rejects(client.hosts.setBrowserDriver(result.revision, 'rod'), DeliveryError);
 assert.equal(requests.length, 3);
});
