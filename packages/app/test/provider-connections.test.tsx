import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { DeliveryError, type ChangeProviderParams, type ProviderDefaultsParams } from '@whip/sdk';
import { ProvidersSettings } from '../src/settings/providers';
import { ProviderSetup } from '../src/provider-setup';
import { useProviderConnections } from '../src/settings/provider-connections';
import { LoginFlow } from '../src/settings/provider-login';
import { providerFixture, route, preset, revision, nextRevision, inferenceFlow } from './provider-fixture';

beforeEach(() => vi.stubGlobal('matchMedia', () => ({ matches: false, addEventListener() {}, removeEventListener() {} })));
afterEach(() => vi.unstubAllGlobals());

async function advanced() {
  const user = userEvent.setup();
  await user.click(await screen.findByRole('button', { name: /Connection options/ }));
  await user.click(await screen.findByRole('menuitem', { name: /Advanced configuration/ }));
}

it('reads explicit local sources without credential discovery, model refresh, or account effects', async () => {
  const f = await providerFixture(); f.data.inventory.routes = [route(), { ...route('custom'), credential: { source: 'command', state: 'unchecked', environment: '', file: '' } }];
  f.mount(<ProvidersSettings client={f.client} />);
  expect(await screen.findByText('Environment')).toBeTruthy(); expect(await screen.findByText(/Credential command not checked/)).toBeTruthy();
  expect(screen.getByText('Connected providers')).toBeTruthy();
  expect(f.calls.every(call => ['initialize', 'providers.list', 'providers.candidates', 'providers.presets', 'providers.readiness', 'providers.bundled', 'providers.catalog', 'accounts.inference.list', 'accounts.openai.list', 'host.permission_default'].includes(call.method))).toBe(true);
});
it('publishes a pasted key once, clears only after acknowledgement, and never journals secrets', async () => {
  const f = await providerFixture(); f.data.inventory.routes = []; f.data.inventory.defaults = null;
  f.data.handlers['providers.create'] = request => { const p = request.params as ChangeProviderParams; f.data.inventory = { ...f.data.inventory, revision: nextRevision, routes: [{ ...route(), credential: { source: 'file', state: 'available', environment: '', file: `/private/${p.key?.id}` } }] }; return f.data.inventory; };
  f.mount(<ProvidersSettings client={f.client} />);
  fireEvent.click(await screen.findByRole('button', { name: 'Connect OpenRouter' }));
  fireEvent.change(await screen.findByLabelText('API key'), { target: { value: 'private-test-key' } });
  fireEvent.click(screen.getByRole('button', { name: 'Connect', exact: true }));
  await screen.findByText('OpenRouter connected.');
  const writes = f.calls.filter(call => call.method === 'providers.create'); expect(writes).toHaveLength(1);
  expect(writes[0]?.params).toMatchObject({ revision, key: { key: 'private-test-key', id: expect.any(String) }, keep_credential: false, declaration: { credential: { source: 'file', environment: '', file: '', command: null } } });
  expect(screen.queryByLabelText('API key')).toBeNull();
  expect(JSON.stringify(f.queries.getQueryCache().getAll().map(query => query.state.data))).not.toContain('private-test-key');
  expect(f.count('providers.defaults')).toBe(0);
});
it('keeps the same key publication identity on explicit retry after uncertain storage', async () => {
  const f = await providerFixture(); f.data.inventory.routes = []; f.data.inventory.defaults = null;
  f.data.handlers['providers.create'] = () => { throw new DeliveryError('Published key durability is unconfirmed'); };
  f.mount(<ProvidersSettings client={f.client} />);
  fireEvent.click(await screen.findByRole('button', { name: 'Connect OpenRouter' }));
  fireEvent.change(await screen.findByLabelText('API key'), { target: { value: 'private-fixture-key' } });
  fireEvent.click(screen.getByRole('button', { name: 'Connect', exact: true }));
  await screen.findByText(/Published key durability is unconfirmed/);
  expect(f.count('providers.create')).toBe(1);
  fireEvent.click(screen.getByRole('button', { name: 'Connect', exact: true }));
  await waitFor(() => expect(f.count('providers.create')).toBe(2));
  const calls = f.calls.filter(call => call.method === 'providers.create'); expect(calls[0]?.params).toEqual(calls[1]?.params);
});
it('retains secret drafts offline and asks before discarding them', async () => {
  const f = await providerFixture(); f.data.inventory.routes = []; f.data.inventory.defaults = null;
  const mounted = f.mount(<ProvidersSettings client={f.client} />);
  fireEvent.click(await screen.findByRole('button', { name: 'Connect OpenRouter' }));
  fireEvent.change(await screen.findByLabelText('API key'), { target: { value: 'kept-key' } });
  mounted.rerender(f.wrap(<ProvidersSettings client={f.client} enabled={false} />));
  expect((screen.getByLabelText('API key') as HTMLInputElement).value).toBe('kept-key'); expect(screen.getByRole('button', { name: 'Connect', exact: true }).hasAttribute('disabled')).toBe(true);
  fireEvent.click(screen.getByRole('button', { name: 'Back' })); expect(await screen.findByRole('dialog', { name: 'Discard this credential?' })).toBeTruthy();
  expect(f.count('providers.create')).toBe(0);
});
it('a failed provider CAS refreshes evidence and requires explicit revision review', async () => {
  const f = await providerFixture(); f.data.handlers['providers.update'] = () => { f.data.inventory.revision = nextRevision; throw new DeliveryError('Revision conflict'); };
  f.mount(<ProvidersSettings client={f.client} />);
  fireEvent.click(await screen.findByRole('button', { name: 'Manage OpenRouter' }));
  await advanced();
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
  const f = await providerFixture(); f.data.handlers['providers.remove'] = () => { f.data.inventory = { ...f.data.inventory, revision: nextRevision, routes: [] }; return f.data.inventory; };
  f.mount(<ProvidersSettings client={f.client} />); fireEvent.click(await screen.findByRole('button', { name: 'Manage OpenRouter' }));
  expect(await screen.findByText('OPENROUTER_API_KEY')).toBeTruthy(); await advanced(); fireEvent.click(screen.getByRole('button', { name: 'Remove configured route' }));
  const dialog = await screen.findByRole('dialog', { name: 'Remove this provider route?' }); expect(within(dialog).getByText(/Saved key files and remote accounts are preserved/)).toBeTruthy();
  fireEvent.click(within(dialog).getByRole('button', { name: 'Remove route' })); await screen.findByText('Provider route removed. Credential files are unchanged.');
  expect(f.calls.find(call => call.method === 'providers.remove')?.params).toEqual({ revision, provider: 'openrouter', replacement: null }); expect(f.count('accounts.inference.logout')).toBe(0);
});

it('validates canonical key discovery before publication and keeps rejected keys for explicit retry', async () => {
  const f = await providerFixture(); f.data.inventory.routes = []; f.data.inventory.defaults = null;
  f.data.presets.items = [{ ...preset(), base_url: 'https://openrouter.ai/api/v1' }];
  f.data.handlers['providers.setup_key'] = () => { throw new DeliveryError('Credential discovery rejected'); };
  f.mount(<ProvidersSettings client={f.client} />);
  fireEvent.click(await screen.findByRole('button', { name: 'Connect OpenRouter' }));
  const dialog = await screen.findByRole('dialog', { name: 'OpenRouter' });
  fireEvent.change(within(dialog).getByLabelText('API key'), { target: { value: 'private-key' } });
  fireEvent.click(within(dialog).getByRole('button', { name: 'Connect', exact: true }));
  await screen.findByText('Credential discovery rejected');
  expect(f.count('providers.setup_key')).toBe(1); expect(f.count('providers.create')).toBe(0);
  expect((screen.getByLabelText('API key') as HTMLInputElement).value).toBe('private-key');
  expect(f.data.inventory.routes).toEqual([]); expect(f.data.inventory.defaults).toBeNull();
  f.data.handlers['providers.setup_key'] = () => { f.data.inventory = { ...f.data.inventory, revision: nextRevision, routes: [{ ...route(), base_url: 'https://openrouter.ai/api/v1' }] }; return f.data.inventory; };
  fireEvent.click(within(dialog).getByRole('button', { name: 'Connect', exact: true }));
  await screen.findByText('OpenRouter connected.');
  const requests = f.calls.filter(call => call.method === 'providers.setup_key');
  expect(requests).toHaveLength(2); expect(requests[0]!.params).toEqual(requests[1]!.params);
  expect(requests[0]!.params).toMatchObject({ revision, provider: 'openrouter', key: { id: expect.any(String), key: 'private-key' }, environment: false });
  expect(f.count('providers.defaults')).toBe(0);
  expect(JSON.stringify(f.queries.getQueryCache().getAll().map(query => query.state.data))).not.toContain('private-key');
});
it('offers Connect for configured missing credentials and limits the recommendation to the canonical preset', async () => {
  const f = await providerFixture(); f.data.inventory.defaults = null;
  f.data.inventory.routes = [{ ...route(), credential: { ...route().credential, state: 'missing' } }];
  f.data.presets.items = [preset(), { ...preset('inference-net'), base_url: 'https://api.inference.net/v1' }];
  function Setup() { const connections = useProviderConnections(f.client, true); return <ProviderSetup client={f.client} enabled hostName="Remote" connections={connections} onReady={() => {}} />; }
  f.mount(<Setup />);
  expect(await screen.findAllByText('Recommended', { exact: true })).toHaveLength(1);
  fireEvent.click(await screen.findByRole('button', { name: 'Connect OpenRouter' }));
  expect(await screen.findByRole('dialog', { name: 'OpenRouter' })).toBeTruthy();
  expect(f.count('providers.defaults')).toBe(0);
});

it('keeps failed removal feedback in its active dialog and preserves the provider draft without replay', async () => {
  const f = await providerFixture();
  f.data.handlers['providers.remove'] = () => { throw new DeliveryError('This provider is the model default'); };
  f.mount(<ProvidersSettings client={f.client} />);
  fireEvent.click(await screen.findByRole('button', { name: 'Manage OpenRouter' }));
  await advanced();
  fireEvent.change(await screen.findByLabelText('Endpoint'), { target: { value: 'https://draft.test/v1' } });
  fireEvent.click(screen.getByRole('button', { name: 'Remove configured route' }));
  const dialog = await screen.findByRole('dialog', { name: 'Remove this provider route?' });
  fireEvent.click(within(dialog).getByRole('button', { name: 'Remove route' }));
  expect(await within(dialog).findByText('Could not remove provider route')).toBeTruthy();
  expect(within(dialog).getByText('This provider is the model default')).toBeTruthy();
  expect(f.count('providers.remove')).toBe(1);
  expect(f.data.inventory.routes).toHaveLength(1);
  fireEvent.click(within(dialog).getByRole('button', { name: 'Cancel' }));
  await waitFor(() => expect(screen.queryByRole('dialog', { name: 'Remove this provider route?' })).toBeNull());
  expect((screen.getByLabelText('Endpoint') as HTMLInputElement).value).toBe('https://draft.test/v1');
  expect(f.count('providers.remove')).toBe(1);
});

it('previews detected credentials without publishing and applies Use in explicit route/default steps', async () => {
  const f = await providerFixture(); f.data.inventory.routes = []; f.data.inventory.defaults = null;
  f.data.handlers['providers.candidates'] = () => ({ revision: f.data.inventory.revision, items: [{ provider: 'openrouter', source: 'env', environment: 'OPENROUTER_API_KEY', credential_state: 'available' }] });
  f.data.handlers['providers.use_candidate'] = () => { f.data.inventory = { ...f.data.inventory, revision: nextRevision, routes: [route()] }; return f.data.inventory; };
  f.data.handlers['providers.defaults'] = request => { f.data.inventory.defaults = (request.params as ProviderDefaultsParams).defaults.selection; return f.data.inventory; };
  const ready = vi.fn();
  function Setup() { const connections = useProviderConnections(f.client, true); return <ProviderSetup client={f.client} enabled hostName="Remote workstation" connections={connections} onReady={ready} />; }
  f.mount(<Setup />);
  await screen.findByText('Already available'); await screen.findByRole('button', { name: 'Use fixture' });
  expect(f.count('providers.use_candidate')).toBe(0); expect(f.count('providers.defaults')).toBe(0); expect(f.count('providers.refresh')).toBe(0);
  fireEvent.click(screen.getByRole('button', { name: 'Use fixture' })); await waitFor(() => expect(ready).toHaveBeenCalledOnce());
  expect(f.calls.find(call => call.method === 'providers.use_candidate')?.params).toEqual({ revision, provider: 'openrouter', source: 'env', environment: 'OPENROUTER_API_KEY' });
  expect(f.calls.find(call => call.method === 'providers.defaults')?.params).toMatchObject({ revision: nextRevision, defaults: { selection: { provider: 'openrouter', name: 'fixture', effort: '' }, settings: { max_attempts: 0, context_window_tokens: null, max_output_tokens: '0' } } });
  expect(f.count('sessions.submit')).toBe(0);
});

it('disable and enable keep the default and credential visible while moving the provider between groups', async () => {
  const f = await providerFixture();
  f.data.handlers['providers.set_enabled'] = request => { f.data.inventory = { ...f.data.inventory, revision: nextRevision, routes: [{ ...f.data.inventory.routes[0]!, disabled: !(request.params as { enabled: boolean }).enabled }] }; return f.data.inventory; };
  f.mount(<ProvidersSettings client={f.client} />); const user = userEvent.setup();
  await user.click(await screen.findByRole('button', { name: 'Manage OpenRouter' })); await user.click(screen.getByRole('button', { name: /Connection options/ })); await user.click(await screen.findByRole('menuitem', { name: 'Disable on this host' }));
  await screen.findByText('Disabled providers'); expect(f.data.inventory.defaults?.name).toBe('fixture'); expect(f.data.inventory.routes[0]?.credential.state).toBe('available');
  await user.click(screen.getByRole('button', { name: 'Manage OpenRouter' })); await user.click(screen.getByRole('button', { name: /Connection options/ })); await user.click(await screen.findByRole('menuitem', { name: 'Enable on this host' }));
  await screen.findByText('Connected providers'); expect(screen.queryByText('Disabled providers')).toBeNull(); expect(f.count('providers.defaults')).toBe(0); expect(f.count('providers.set_enabled')).toBe(2);
});

it.each([
  ['cleared', null, 'OpenRouter disconnected.'],
  ['preserved_shared', null, 'OpenRouter disabled. Its credentials are shared with another provider and were kept.'],
  ['preserved_external', null, 'These credentials are managed outside Whip.'],
  ['pending', 'Key cleanup still pending', 'Key cleanup still pending'],
] as const)('Disconnect presents %s without removing the route or silently retrying', async (state, failure, expected) => {
  const f = await providerFixture(); f.data.handlers['providers.disconnect'] = () => ({ inventory: f.data.inventory, credential_state: state, local_failure: failure, cleanup_failure: null });
  f.mount(<ProvidersSettings client={f.client} />); const user = userEvent.setup();
  await user.click(await screen.findByRole('button', { name: 'Manage OpenRouter' })); await user.click(screen.getByRole('button', { name: /Connection options/ })); await user.click(await screen.findByRole('menuitem', { name: 'Disconnect provider' }));
  await screen.findByText(text => text.includes(expected));
  expect(f.count('providers.disconnect')).toBe(1); expect(f.count('providers.remove')).toBe(0); expect(f.count('accounts.inference.logout')).toBe(0); expect(f.data.inventory.defaults?.name).toBe('fixture');
});


it('keeps existing custom route lifecycle actions in the everyday connection view', async () => {
  const f = await providerFixture(); f.data.inventory.routes.push(route('custom'));
  f.mount(<ProvidersSettings client={f.client} />); const user = userEvent.setup();
  await user.click(await screen.findByRole('button', { name: 'Manage custom', exact: true }));
  expect(screen.queryByLabelText('Endpoint')).toBeNull();
  await user.click(screen.getByRole('button', { name: /Connection options/ }));
  expect(await screen.findByRole('menuitem', { name: 'Disable on this host' })).toBeTruthy();
  expect(screen.getByRole('menuitem', { name: 'Disconnect provider' })).toBeTruthy();
  await user.click(screen.getByRole('menuitem', { name: /Advanced configuration/ }));
  expect(await screen.findByLabelText('Endpoint')).toBeTruthy();
});
