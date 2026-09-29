import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { DeliveryError, type ChangeProviderParams, type ProviderDefaultsParams } from '@whip/sdk';
import { ProvidersSettings } from '../src/settings/providers';
import { ProviderSetup } from '../src/provider-setup';
import { useProviderConnections } from '../src/settings/provider-connections';
import { LoginFlow } from '../src/settings/provider-login';
import { providerFixture, route, preset, revision, nextRevision, inferenceFlow } from './provider-fixture';

beforeEach(() => vi.stubGlobal('matchMedia', () => ({ matches: false, addEventListener() {}, removeEventListener() {} })));
afterEach(() => vi.unstubAllGlobals());

it('reads explicit local sources without credential discovery, model refresh, or account effects', async () => {
  const f = await providerFixture(); f.data.inventory.routes = [route(), { ...route('custom'), credential: { source: 'command', state: 'unchecked', environment: '', file: '' } }];
  f.mount(<ProvidersSettings client={f.client} />);
  expect(await screen.findByText('Environment')).toBeTruthy(); expect(await screen.findByText(/Credential command not checked/)).toBeTruthy();
  expect(screen.getByText(/Model access and inference have not been tested/)).toBeTruthy();
  expect(f.calls.every(call => ['initialize', 'providers.list', 'providers.presets', 'providers.readiness', 'providers.bundled', 'providers.catalog', 'accounts.inference.list', 'accounts.openai.list', 'host.permission_default'].includes(call.method))).toBe(true);
});
it('publishes a pasted key once, clears only after acknowledgement, and never journals secrets', async () => {
  const f = await providerFixture(); f.data.inventory.routes = []; f.data.inventory.defaults = null;
  f.data.handlers['providers.create'] = request => { const p = request.params as ChangeProviderParams; f.data.inventory = { ...f.data.inventory, revision: nextRevision, routes: [{ ...route(), credential: { source: 'file', state: 'available', environment: '', file: `/private/${p.key?.id}` } }] }; return f.data.inventory; };
  f.mount(<ProvidersSettings client={f.client} />);
  fireEvent.click(await screen.findByRole('button', { name: 'Connect OpenRouter' }));
  fireEvent.change(await screen.findByLabelText('API key'), { target: { value: 'private-test-key' } });
  fireEvent.click(screen.getByRole('button', { name: 'Save provider' }));
  await screen.findByText('Provider saved. Your default model is unchanged.');
  const writes = f.calls.filter(call => call.method === 'providers.create'); expect(writes).toHaveLength(1);
  expect(writes[0]?.params).toMatchObject({ revision, key: { key: 'private-test-key', id: expect.any(String) }, keep_credential: false, declaration: { credential: { source: 'file', environment: '', file: '', command: null } } });
  expect((screen.getByLabelText('API key') as HTMLInputElement).value).toBe('');
  expect(JSON.stringify(f.queries.getQueryCache().getAll().map(query => query.state.data))).not.toContain('private-test-key');
  expect(f.count('providers.defaults')).toBe(0);
});
it('keeps the same key publication identity on explicit retry after uncertain storage', async () => {
  const f = await providerFixture(); f.data.inventory.routes = []; f.data.inventory.defaults = null;
  f.data.handlers['providers.create'] = () => { throw new DeliveryError('Published key durability is unconfirmed'); };
  f.mount(<ProvidersSettings client={f.client} />);
  fireEvent.click(await screen.findByRole('button', { name: 'Connect OpenRouter' }));
  fireEvent.change(await screen.findByLabelText('API key'), { target: { value: 'private-fixture-key' } });
  fireEvent.click(screen.getByRole('button', { name: 'Save provider' }));
  await screen.findByText(/Published key durability is unconfirmed/);
  expect(f.count('providers.create')).toBe(1);
  fireEvent.click(screen.getByRole('button', { name: 'Save provider' }));
  await waitFor(() => expect(f.count('providers.create')).toBe(2));
  const calls = f.calls.filter(call => call.method === 'providers.create'); expect(calls[0]?.params).toEqual(calls[1]?.params);
});
it('retains secret drafts offline and asks before discarding them', async () => {
  const f = await providerFixture(); f.data.inventory.routes = []; f.data.inventory.defaults = null;
  const mounted = f.mount(<ProvidersSettings client={f.client} />);
  fireEvent.click(await screen.findByRole('button', { name: 'Connect OpenRouter' }));
  fireEvent.change(await screen.findByLabelText('API key'), { target: { value: 'kept-key' } });
  mounted.rerender(f.wrap(<ProvidersSettings client={f.client} enabled={false} />));
  expect((screen.getByLabelText('API key') as HTMLInputElement).value).toBe('kept-key'); expect(screen.getByRole('button', { name: 'Save provider' }).hasAttribute('disabled')).toBe(true);
  fireEvent.click(screen.getByRole('button', { name: 'Done' })); expect(await screen.findByRole('dialog', { name: 'Discard this credential?' })).toBeTruthy();
  expect(f.count('providers.create')).toBe(0);
});
it('a failed provider CAS refreshes evidence and requires explicit revision review', async () => {
  const f = await providerFixture(); f.data.handlers['providers.update'] = () => { f.data.inventory.revision = nextRevision; throw new DeliveryError('Revision conflict'); };
  f.mount(<ProvidersSettings client={f.client} />);
  fireEvent.click(await screen.findByRole('button', { name: 'Manage OpenRouter' }));
  fireEvent.change(await screen.findByLabelText('Endpoint'), { target: { value: 'https://changed.test/v1' } });
  fireEvent.click(screen.getByRole('button', { name: 'Save provider' })); await screen.findByText(/Revision conflict/);
  expect(f.count('providers.update')).toBe(1); expect(screen.getByRole('button', { name: 'Save provider' }).hasAttribute('disabled')).toBe(true);
  expect((screen.getByLabelText('Endpoint') as HTMLInputElement).value).toBe('https://changed.test/v1');
  expect(await screen.findByRole('button', { name: 'Review current revision and keep these edits' })).toBeTruthy();
});
it('lost begin acknowledgement is recovered by list without another begin or browser open', async () => {
  const f = await providerFixture(); f.data.handlers['accounts.inference.begin'] = () => { f.data.inference = [inferenceFlow()]; throw new DeliveryError('Sign-in acknowledgement lost'); };
  f.mount(<ProvidersSettings client={f.client} />);
  fireEvent.click(await screen.findByRole('button', { name: 'Connect inference-net' }));
  fireEvent.click(await screen.findByRole('button', { name: 'Sign in with Inference.net' }));
  await screen.findByText('Finish signing in in your browser');
  expect(f.count('accounts.inference.begin')).toBe(1); expect(f.count('accounts.inference.list')).toBeGreaterThan(1); expect(f.runtime.platform.openExternal).not.toHaveBeenCalled();
});
it('reuses an active login and opens its fixed verification page only after user sign-in', async () => {
  const f = await providerFixture(); const flow = inferenceFlow(); let reads = 0;
  f.data.handlers['accounts.inference.list'] = () => ({ items: ++reads === 1 ? [] : [flow] });
  f.mount(<ProvidersSettings client={f.client} />);
  fireEvent.click(await screen.findByRole('button', { name: 'Connect inference-net' })); fireEvent.click(await screen.findByRole('button', { name: 'Sign in with Inference.net' }));
  await waitFor(() => expect(f.runtime.platform.openExternal).toHaveBeenCalledExactlyOnceWith(flow.verification_url)); expect(f.count('accounts.inference.begin')).toBe(0);
});
it('uncertain project creation exposes read-only progress inspection and never repeats creation', async () => {
  const f = await providerFixture(); const flow = inferenceFlow('uncertain'); f.data.handlers['accounts.inference.get'] = () => flow;
  const update = vi.fn(async () => {}), refresh = vi.fn(async () => {});
  f.mount(<LoginFlow flow={{ provider: 'inference-net', value: flow }} client={f.client} enabled hostName="Remote" update={update} refresh={refresh} leave={() => {}} />);
  expect(screen.getByText(/last account change may have completed/)).toBeTruthy();
  expect(screen.queryByRole('button', { name: 'Create and connect' })).toBeNull(); expect(screen.queryByRole('button', { name: 'Continue setup' })).toBeNull();
  fireEvent.click(screen.getByRole('button', { name: 'Check progress' })); await waitFor(() => expect(update).toHaveBeenCalled()); expect(f.count('accounts.inference.get')).toBe(1); expect(f.count('accounts.inference.create_project')).toBe(0);
});
it.each(['persistence_required', 'setup_required', 'cleanup_required'] as const)('retries the known %s step by exact flow identity', async state => {
  const f = await providerFixture(); const flow = inferenceFlow(state); f.data.handlers['accounts.inference.retry'] = () => inferenceFlow('succeeded');
  const update = vi.fn(async () => {});
  f.mount(<LoginFlow flow={{ provider: 'inference-net', value: flow }} client={f.client} enabled hostName="Remote" update={update} refresh={async () => {}} leave={() => {}} />);
  fireEvent.click(screen.getByRole('button', { name: 'Continue setup' })); await waitFor(() => expect(update).toHaveBeenCalled());
  expect(f.calls.find(call => call.method === 'accounts.inference.retry')?.params).toEqual({ flow_id: flow.id }); expect(f.count('accounts.inference.begin')).toBe(0); expect(f.count('accounts.inference.rotate')).toBe(0);
});
it('cached catalogs never change defaults until an explicit setup choice', async () => {
  const f = await providerFixture(); f.data.inventory.defaults = null; f.data.presets.items = [preset()];
  f.data.handlers['providers.defaults'] = request => { const p = request.params as ProviderDefaultsParams; f.data.inventory = { ...f.data.inventory, revision: nextRevision, defaults: p.defaults.selection }; return f.data.inventory; };
  const ready = vi.fn();
  function Setup() { const connections = useProviderConnections(f.client, true); return <ProviderSetup client={f.client} enabled hostName="Remote" connections={connections} onReady={ready} />; }
  f.mount(<Setup />); await screen.findByRole('button', { name: 'Use fixture' }); expect(f.count('providers.defaults')).toBe(0); expect(f.count('providers.refresh')).toBe(0);
  fireEvent.click(screen.getByRole('button', { name: 'Use fixture' })); await waitFor(() => expect(ready).toHaveBeenCalledOnce());
  expect(f.calls.find(call => call.method === 'providers.defaults')?.params).toMatchObject({ revision, defaults: { selection: { name: 'fixture', provider: 'openrouter', effort: '' }, settings: { prices: { input: null, output: null } } } });
});
it('route removal is explicit, preserves credential ownership, and does not disconnect an account', async () => {
  const f = await providerFixture(); f.data.handlers['providers.remove'] = () => ({ ...f.data.inventory, revision: nextRevision, routes: [] });
  f.mount(<ProvidersSettings client={f.client} />); fireEvent.click(await screen.findByRole('button', { name: 'Manage OpenRouter' }));
  expect(await screen.findByText('OPENROUTER_API_KEY')).toBeTruthy(); fireEvent.click(screen.getByRole('button', { name: 'Remove configured route' }));
  const dialog = await screen.findByRole('dialog', { name: 'Remove this provider route?' }); expect(within(dialog).getByText(/Saved key files and remote accounts are preserved/)).toBeTruthy();
  fireEvent.click(within(dialog).getByRole('button', { name: 'Remove route' })); await screen.findByText('Provider route removed. Credential files are unchanged.');
  expect(f.calls.find(call => call.method === 'providers.remove')?.params).toEqual({ revision, provider: 'openrouter', replacement: null }); expect(f.count('accounts.inference.logout')).toBe(0);
});
