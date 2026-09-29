import assert from 'node:assert/strict';
import { test } from 'node:test';
import { Client, DeliveryError, RemoteError } from '../dist/index.js';

const initial = { major: 4, minor: 0, runtime_id: 'runtime', process_epoch: 'boot_test', network_client: false, builtins: [] };
const success = (request, result) => ({ jsonrpc: '2.0', id: request.id, result });
const inventory = { revision: 'a'.repeat(64), routes: [], defaults: null, compaction_model: null };
const model = { id: 'model', name: 'Exact model', prices: { input: '9007199254740993', output: '0', reasoning: null, cached_input: null, cached_output: null }, context_window_tokens: '0', advertised_context_tokens: null, effective_context_percent: null, max_output_tokens: null, reasoning_efforts: [], input_modalities: null, output_modalities: ['text'], supports_tools: false, metadata_source: 'advertised' };
const catalog = { provider: 'custom', state: 'cached', scope_state: 'unverified', discovery: 'catalog_response', fetched_at: '2026-09-28T12:00:00.123456789Z', stale: false, failure: null, models: [model] };
const selection = { provider: 'custom', name: 'explicit-unknown', effort: '', temperature: 0 };
const change = { revision: inventory.revision, provider: 'custom', declaration: { kind: 'openai-chat', base_url: 'https://example.test/v1', credential: { source: 'file', environment: '', file: '', command: null }, models: {} }, keep_credential: false, key: { id: 'stable-key', key: 'private-pasted-key' } };

test('provider helpers preserve exact nullable metadata and perform only requested operations', async () => {
  const calls = [];
  const client = await Client.connect(async request => {
    if (request.method === 'initialize') return success(request, initial);
    calls.push(structuredClone(request));
    switch (request.method) {
      case 'providers.presets': return success(request, { items: [] });
      case 'providers.bundled': return success(request, { items: [{ ...model, metadata_source: 'bundled' }] });
      case 'providers.catalog': return success(request, structuredClone(catalog));
      case 'providers.refresh': return success(request, { ...structuredClone(catalog), failure: 'Discovery failed; previous observations retained' });
      case 'providers.readiness': return success(request, { configured: true, credential_state: 'unchecked', catalog_state: 'cached', model_state: 'unknown', inference_state: 'not_tested' });
      default: return success(request, structuredClone(inventory));
    }
  }, { clientID: 'provider-setup' });
  await client.providerPresets();
  assert.equal((await client.bundledProviderModels('custom')).items[0].prices.input, '9007199254740993');
  await client.listProviders();
  await client.createProvider(change);
  await client.updateProvider({ ...change, key: null, keep_credential: true, declaration: { ...change.declaration, credential: null } });
  await client.setProviderDefaults({ revision: inventory.revision, defaults: { selection, settings: null } });
  await client.setProviderCompactionModel({ revision: inventory.revision, defaults: { selection: null, settings: null } });
  assert.deepEqual(await client.providerCatalog('custom'), catalog);
  assert.ok((await client.refreshProviderCatalog('custom')).failure);
  assert.equal((await client.providerReadiness(selection)).inference_state, 'not_tested');
  await client.removeProvider({ revision: inventory.revision, provider: 'custom', replacement: { selection: null, settings: null } });
  assert.deepEqual(calls.map(value => value.method), ['presets', 'bundled', 'list', 'create', 'update', 'defaults', 'compaction', 'catalog', 'refresh', 'readiness', 'remove'].map(value => `providers.${value}`));
  const before = calls.length;
  await assert.rejects(client.createProvider({ ...change, key: { id: 'stable-key', key: 'control\nsecret' } }), TypeError);
  await assert.rejects(client.listProviders({ signal: AbortSignal.abort() }), error => error.name === 'AbortError');
  assert.equal(calls.length, before);
});

test('provider mutation uncertainty and key durability do not trigger automatic replay', async () => {
  const calls = [];
  const client = await Client.connect(async request => {
    if (request.method === 'initialize') return success(request, initial);
    calls.push(request.method);
    if (request.method === 'providers.update') return { jsonrpc: '2.0', id: request.id, error: { code: -32033, kind: 'PROVIDER_KEY_PENDING', message: 'Published key durability is unconfirmed; retry the same key identity' } };
    if (request.method === 'providers.list') return success(request, inventory);
    throw new DeliveryError('acknowledgement lost');
  }, { clientID: 'provider-recovery' });
  await assert.rejects(client.createProvider(change), DeliveryError);
  await assert.rejects(client.updateProvider(change), error => error instanceof RemoteError && error.kind === 'PROVIDER_KEY_PENDING');
  assert.deepEqual(calls, ['providers.create', 'providers.update']);
  assert.deepEqual(await client.listProviders(), inventory);
});

test('provider response validation rejects lossy numeric prices and secret projections', async () => {
  for (const invalid of [{ ...catalog, models: [{ ...model, prices: { ...model.prices, input: 9007199254740993 } }] }, { ...catalog, key: 'private-key' }]) {
    const client = await Client.connect(async request => success(request, request.method === 'initialize' ? initial : invalid), { clientID: 'provider-invalid' });
    await assert.rejects(client.providerCatalog('custom'), TypeError);
  }
});
