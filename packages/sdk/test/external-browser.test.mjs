import assert from 'node:assert/strict';
import { test } from 'node:test';
import { Client, DeliveryError } from '../dist/index.js';
const initial = { major: 4, minor: 0, runtime_id: 'runtime', process_epoch: 'boot', network_client: false, builtins: [] };
const configuration = { mode: 'live', executable: '', live_endpoint: 'http://127.0.0.1:9222', live_profile: '', allow_private_urls: false };
const status = { revision: 'a'.repeat(64), configuration, driver: 'rod', driver_pinned: false };
const entry = { root_id: 'root', name: 'default', mode: 'live', driver: 'rod', generation: 'exact', resource: 'browser-external:'+'b'.repeat(64), state: 'prepared' };
test('external browser declarations and exact generation controls never replay uncertain delivery', async () => {
 const requests = []; let lost = false;
 const client = await Client.connect(async request => {
  if (request.method === 'initialize') return { jsonrpc:'2.0', id:request.id, result:initial };
  requests.push(request); if (lost) throw new DeliveryError('lost acknowledgement');
  const result = request.method === 'browser.external_sessions' ? { items:[entry] } : request.method.startsWith('browser.') ? entry : status;
  return { jsonrpc:'2.0', id:request.id, result };
 }, {clientID:'browser-settings'});
 assert.deepEqual(await client.hosts.externalBrowser(), status);
 await client.hosts.setExternalBrowser(status.revision, configuration);
 assert.deepEqual(requests.at(-1).params, {expected_revision:status.revision,configuration});
 assert.deepEqual(await client.hosts.externalBrowserSessions('root'), {items:[entry]});
 await client.hosts.reconnectExternalBrowser('root','default','exact');
 assert.deepEqual(requests.at(-1).params,{root_id:'root',name:'default',generation:'exact'});
 await assert.rejects(client.hosts.reconnectExternalBrowser('root','../other','exact'),TypeError);
 assert.equal(requests.length,4);
 lost=true;
 await assert.rejects(client.hosts.disconnectExternalBrowser('root','default','exact'),DeliveryError);
 assert.equal(requests.length,5);
});
