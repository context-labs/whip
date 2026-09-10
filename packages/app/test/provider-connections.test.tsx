import { type ReactNode } from 'react';
import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { WhipClient } from '@whip/sdk';
import type { ProviderList, ProviderLoginStatus } from '@whip/protocol';
import { ThemeProvider, UIProvider } from '@whip/ui';
import { RuntimeContext } from '../src/context';
import type { AppRuntime } from '../src/runtime';
import { ProvidersSettings } from '../src/settings/providers';
import { ProviderConnections, useProviderConnections } from '../src/settings/provider-connections';
import { ProviderSetup } from '../src/provider-setup';

beforeEach(() => vi.stubGlobal('matchMedia', vi.fn(() => ({ matches: false, addEventListener() {}, removeEventListener() {} }))));
afterEach(() => { vi.unstubAllGlobals(); vi.useRealTimers(); });
type Entry = NonNullable<ProviderList['providers']>[number];
const entry = (id: string, source: string, available: boolean, extra: Partial<Entry['status']> = {}): Entry => ({
  id, name: id === 'openrouter' ? 'OpenRouter' : id === 'openai-codex' ? 'OpenAI (ChatGPT subscription)' : id,
  custom: id === 'custom', methods: id === 'openai-codex' ? ['login'] : ['api_key'],
  status: { provider: id, key_source: source, available, configured: available, auth_state: available ? 'connected' : 'key_required', warnings: [], ...extra },
});

function Setup({ client, enabled }: { client: WhipClient; enabled: boolean }) {
  const connections = useProviderConnections(client, enabled);
  return <ProviderSetup client={client} enabled={enabled} hostName="Remote workstation" connections={connections} onReady={() => {}} />;
}

function fixture(entries: Entry[] = [entry('openrouter', 'environment', true, { environment_variable: 'OPENROUTER_API_KEY' }), entry('openai-codex', 'none', false)], options: { setup?: boolean; discovery?: boolean; unkeyed?: boolean } = {}) {
  const queries = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0, staleTime: Infinity } } });
  const config = { revision: '7', disabled_providers: ['unrelated'], default_model: 'fixture', default_provider: 'openrouter', default_effort: '', compact_model: '', compact_provider: '', compact_percent: 75, goal_max_rounds: 8, max_retries: 2, import_claude: true, import_codex: true };
  const client = {
    getSnapshot: () => ({ state: 'connected', info: { runtime_id: 'host' } }),
    supports: vi.fn(() => options.discovery !== false),
    providers: {
      catalogs: vi.fn(async () => ({ result: { models: {}, providers: {}, catalogs: {} } })),
      list: vi.fn(async (): Promise<ProviderList> => ({ revision: '7', default_provider: 'openrouter', providers: entries, selection: { ready: false, model: '', provider: '', reason: 'key_required' } })),
      discover: vi.fn(async (): Promise<ProviderList> => ({ revision: '7', default_provider: 'openrouter', providers: entries, selection: { ready: false, model: '', provider: '', reason: 'key_required' } })),
      setKey: vi.fn(async () => ({})),
      disconnect: vi.fn(async () => ({ warnings: [] })), rotateKey: vi.fn(async () => ({})),
      login: { list: vi.fn(async () => ({ flows: [] as ProviderLoginStatus[] })), begin: vi.fn(async () => ({})) },
    },
    configuration: { get: vi.fn(async () => config), update: vi.fn(async () => ({})) },
  };
  const runtime = { queries, platform: { copy: vi.fn(async () => {}), openExternal: vi.fn(async () => {}) }, report: vi.fn(), connections: { host: () => ({ name: 'Remote workstation' }) } } as unknown as AppRuntime;
  const wrap = (node: ReactNode) => <RuntimeContext.Provider value={runtime}><ThemeProvider initialTheme="light"><UIProvider><QueryClientProvider client={queries}>{node}</QueryClientProvider></UIProvider></ThemeProvider></RuntimeContext.Provider>;
  const content = (enabled = true) => options.setup ? <Setup key={client.getSnapshot().info.runtime_id} client={client as unknown as WhipClient} enabled={enabled} /> : options.unkeyed ? <ProviderConnections client={client as unknown as WhipClient} enabled={enabled} /> : <ProvidersSettings client={client as unknown as WhipClient} enabled={enabled} />;
  const mounted = render(wrap(content()));
  return { client, config, queries, runtime, ...mounted,
    offline() { mounted.rerender(wrap(content(false))); },
    reconnect() { mounted.rerender(wrap(content())); },
    changeHost(id: string) {
      client.getSnapshot = () => ({ state: 'connected', info: { runtime_id: id } });
      mounted.rerender(wrap(content()));
    },
  };
}

it('shows host-owned sources, bundled logos and source-appropriate controls', async () => {
  const f = fixture();
  expect(await screen.findByText('Connections belong to Remote workstation.')).toBeTruthy();
  expect(await screen.findByText('Environment')).toBeTruthy();
  expect(screen.getByRole('button', { name: 'Connect OpenAI (ChatGPT subscription)' })).toBeTruthy();
  expect(document.querySelector('use')?.getAttribute('href')).toMatch(/providers\.svg#openrouter$/);
  fireEvent.click(screen.getByRole('button', { name: 'Manage OpenRouter' }));
  const dialog = await screen.findByRole('dialog', { name: 'OpenRouter' });
  expect(within(dialog).getByText('OPENROUTER_API_KEY')).toBeTruthy();
  expect(within(dialog).queryByRole('button', { name: 'Disconnect provider' })).toBeNull();
  fireEvent.click(within(dialog).getByRole('button', { name: 'Disable on this host' }));
  await waitFor(() => expect(f.client.configuration.update).toHaveBeenCalledExactlyOnceWith({ revision: '7', disabled_providers: ['unrelated', 'openrouter'] }, { signal: expect.any(AbortSignal) }));
  expect(f.client.providers.disconnect).not.toHaveBeenCalled();
});

it.each([['env_file', 'Environment file'], ['key_file', 'Key file']])('identifies %s credentials without offering to delete the source file', async (source, label) => {
  const f = fixture([entry('openrouter', source, true, { environment_variable: 'OPENROUTER_API_KEY', credential_path: '/fixtures/provider-keys/openrouter' })]);
  expect(await screen.findByText(label)).toBeTruthy();
  fireEvent.click(screen.getByRole('button', { name: 'Manage OpenRouter' }));
  const dialog = await screen.findByRole('dialog', { name: 'OpenRouter' });
  expect(within(dialog).getByText(label)).toBeTruthy();
  expect(within(dialog).getByText('OPENROUTER_API_KEY')).toBeTruthy();
  expect(within(dialog).getByText('/fixtures/provider-keys/openrouter')).toBeTruthy();
  expect(within(dialog).queryByRole('button', { name: 'Disconnect provider' })).toBeNull();
  fireEvent.click(within(dialog).getByRole('button', { name: 'Disable on this host' }));
  await waitFor(() => expect(f.client.configuration.update).toHaveBeenCalledExactlyOnceWith({ revision: '7', disabled_providers: ['unrelated', 'openrouter'] }, { signal: expect.any(AbortSignal) }));
  expect(f.client.providers.disconnect).not.toHaveBeenCalled();
});

it('connects with a key once, clears the draft, and keeps secrets out of queries', async () => {
  const f = fixture([entry('openrouter', 'none', false)]);
  fireEvent.click(await screen.findByRole('button', { name: 'Connect OpenRouter' }));
  const dialog = await screen.findByRole('dialog', { name: 'OpenRouter' });
  fireEvent.change(within(dialog).getByLabelText('API key'), { target: { value: 'private-test-key' } });
  fireEvent.click(within(dialog).getByRole('button', { name: 'Connect', exact: true }));
  await screen.findByText('OpenRouter connected.');
  expect(f.client.providers.setKey).toHaveBeenCalledExactlyOnceWith({ revision: '7', provider: 'openrouter', key: 'private-test-key', environment: false }, { signal: expect.any(AbortSignal) });
  expect(f.client.configuration.update).not.toHaveBeenCalled();
  expect(JSON.stringify(f.queries.getQueryCache().getAll().map(query => query.state.data))).not.toContain('private-test-key');
  fireEvent.click(screen.getByRole('button', { name: 'Connect OpenRouter' }));
  expect((await screen.findByLabelText('API key') as HTMLInputElement).value).toBe('');
});

it('keeps default repair explicit and allows removing a disabled saved key', async () => {
  const f = fixture([entry('openrouter', 'literal', false, { disabled: true, configured: true, auth_state: 'connected' })]);
  expect(await screen.findByText(/Your default model is unchanged/)).toBeTruthy();
  fireEvent.click(screen.getByRole('button', { name: 'Manage default provider' }));
  fireEvent.click(await screen.findByRole('button', { name: 'Disconnect provider' }));
  await waitFor(() => expect(f.client.providers.disconnect).toHaveBeenCalledExactlyOnceWith({ provider: 'openrouter', revision: '7' }, { signal: expect.any(AbortSignal) }));
  expect(f.client.configuration.update).not.toHaveBeenCalled();
});

it('rejects stale disable without mutating and refreshes the inventory', async () => {
  const f = fixture();
  f.config.revision = '8';
  fireEvent.click(await screen.findByRole('button', { name: 'Manage OpenRouter' }));
  fireEvent.click(await screen.findByRole('button', { name: 'Disable on this host' }));
  await screen.findByText(/Provider settings changed/);
  expect(f.client.configuration.update).not.toHaveBeenCalled();
  expect(f.client.providers.list.mock.calls.length).toBeGreaterThan(1);
});

it('uses the detected host key only after an explicit connection choice', async () => {
  const provider = entry('openrouter', 'literal', true);
  provider.methods = ['api_key', 'environment'];
  const f = fixture([provider]);
  fireEvent.click(await screen.findByRole('button', { name: 'Manage OpenRouter' }));
  fireEvent.click(await screen.findByRole('button', { name: 'Use detected key' }));
  await screen.findByText('OpenRouter now uses the detected host key.');
  expect(f.client.providers.setKey).toHaveBeenCalledExactlyOnceWith({ provider: 'openrouter', revision: '7', key: '', environment: true }, { signal: expect.any(AbortSignal) });
});

it('offers the existing subscription flow and disables mutations when the host disconnects', async () => {
  const f = fixture();
  fireEvent.click(await screen.findByRole('button', { name: 'Connect OpenAI (ChatGPT subscription)' }));
  fireEvent.click(await screen.findByRole('button', { name: 'Sign in', exact: true }));
  await waitFor(() => expect(f.client.providers.login.begin).toHaveBeenCalledExactlyOnceWith({ provider: 'openai-codex', signal: expect.any(AbortSignal) }));
  f.offline();
  await waitFor(() => expect((screen.getByRole('button', { name: 'Sign in', exact: true }) as HTMLButtonElement).disabled).toBe(true));
});

it('aborts an in-flight key request on unmount without replaying it', async () => {
  const f = fixture([entry('custom', 'none', false)]);
  f.client.providers.setKey.mockImplementationOnce(() => new Promise(() => {}));
  fireEvent.click(await screen.findByRole('button', { name: 'Connect custom' }));
  fireEvent.change(await screen.findByLabelText('API key'), { target: { value: 'private-test-key' } });
  fireEvent.click(screen.getByRole('button', { name: 'Connect', exact: true }));
  await waitFor(() => expect(f.client.providers.setKey).toHaveBeenCalledTimes(1));
  const options = f.client.providers.setKey.mock.calls[0]?.[1] as unknown as { signal: AbortSignal };
  f.unmount();
  expect(options.signal.aborted).toBe(true);
  expect(f.client.providers.setKey).toHaveBeenCalledTimes(1);
});

it('discards secret drafts and aborts pending requests when the execution host changes', async () => {
  const f = fixture([entry('openrouter', 'none', false)]);
  fireEvent.click(await screen.findByRole('button', { name: 'Connect OpenRouter' }));
  fireEvent.change(await screen.findByLabelText('API key'), { target: { value: 'first-host-key' } });
  f.changeHost('other-host');
  expect(screen.queryByRole('dialog')).toBeNull();
  fireEvent.click(await screen.findByRole('button', { name: 'Connect OpenRouter' }));
  expect((await screen.findByLabelText('API key') as HTMLInputElement).value).toBe('');
  expect(f.client.providers.setKey).not.toHaveBeenCalled();
  f.client.providers.setKey.mockImplementationOnce(() => new Promise(() => {}));
  fireEvent.change(screen.getByLabelText('API key'), { target: { value: 'second-host-key' } });
  fireEvent.click(screen.getByRole('button', { name: 'Connect', exact: true }));
  await waitFor(() => expect(f.client.providers.setKey).toHaveBeenCalledOnce());
  const options = f.client.providers.setKey.mock.calls[0]?.[1] as unknown as { signal: AbortSignal };
  f.changeHost('host');
  expect(options.signal.aborted).toBe(true);
  expect(screen.queryByRole('dialog')).toBeNull();
});

it('polls only active sign-in flows and stops when they finish', async () => {
  vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval'] });
  const f = fixture();
  await waitFor(() => expect(f.client.providers.login.list).toHaveBeenCalledOnce());
  await act(async () => { await vi.advanceTimersByTimeAsync(2200); });
  expect(f.client.providers.login.list).toHaveBeenCalledOnce();
  const flow: ProviderLoginStatus = { flow_id: 'sign-in', provider: 'openai-codex', state: 'pending', teams: [], projects: [], expires_at: '2099-01-01T00:00:00Z' };
  f.client.providers.login.list.mockResolvedValue({ flows: [flow] });
  await act(async () => { f.queries.setQueryData(['provider-login-flows', 'host'], { flows: [flow] }); });
  await act(async () => { await vi.advanceTimersByTimeAsync(2200); });
  expect(f.client.providers.login.list.mock.calls.length).toBeGreaterThan(1);
  await act(async () => { f.queries.setQueryData(['provider-login-flows', 'host'], { flows: [{ ...flow, state: 'succeeded' }] }); });
  const calls = f.client.providers.login.list.mock.calls.length;
  await act(async () => { await vi.advanceTimersByTimeAsync(4400); });
  expect(f.client.providers.login.list).toHaveBeenCalledTimes(calls);
});

it('refreshes only the owning host after a recovered login succeeds', async () => {
  const f = fixture();
  await waitFor(() => expect(f.client.providers.list).toHaveBeenCalledOnce());
  const otherHost = vi.fn(async () => ({}));
  await f.queries.fetchQuery({ queryKey: ['provider-list', 'other-host'], gcTime: Infinity, queryFn: otherHost });
  const flow: ProviderLoginStatus = { flow_id: 'sign-in', provider: 'openai-codex', state: 'succeeded', teams: [], projects: [], expires_at: '2099-01-01T00:00:00Z' };
  await act(async () => { f.queries.setQueryData(['provider-login-flows', 'host'], { flows: [flow] }); });
  await waitFor(() => expect(f.client.providers.list.mock.calls.length).toBeGreaterThan(1));
  expect(f.queries.getQueryState(['provider-list', 'other-host'])?.isInvalidated).toBe(false);
  expect(otherHost).toHaveBeenCalledOnce();
});

it.each([false, true])('refreshes machine-account status without replaying rotation (uncertain: %s)', async uncertain => {
  const f = fixture([entry('inference-net', 'machine', true, { email: 'fixture@example.com' })]);
  if (uncertain) f.client.providers.rotateKey.mockRejectedValueOnce(new Error('acknowledgement lost'));
  fireEvent.click(await screen.findByRole('button', { name: 'Manage inference-net' }));
  fireEvent.click(await screen.findByRole('button', { name: 'Rotate machine key' }));
  await screen.findByText(uncertain ? 'acknowledgement lost' : 'Machine key rotated.');
  await waitFor(() => expect(f.client.providers.list.mock.calls.length).toBeGreaterThan(1));
  expect(f.client.providers.rotateKey).toHaveBeenCalledExactlyOnceWith('inference-net', { signal: expect.any(AbortSignal) });
  expect(f.client.providers.disconnect).not.toHaveBeenCalled();
});

it('disconnects subscriptions with the inventory revision and never offers API-key setup', async () => {
  const f = fixture([entry('openai-codex', 'subscription', true, { email: 'fixture@example.com', plan: 'pro' })]);
  fireEvent.click(await screen.findByRole('button', { name: 'Manage OpenAI (ChatGPT subscription)' }));
  expect(screen.queryByLabelText('API key')).toBeNull();
  expect(screen.queryByRole('button', { name: 'Rotate machine key' })).toBeNull();
  fireEvent.click(screen.getByRole('button', { name: 'Disconnect provider' }));
  await screen.findByText('OpenAI (ChatGPT subscription) disconnected.');
  expect(f.client.providers.disconnect).toHaveBeenCalledExactlyOnceWith({ provider: 'openai-codex', revision: '7' }, { signal: expect.any(AbortSignal) });
  expect(f.client.providers.rotateKey).not.toHaveBeenCalled();
});

it('does not describe a signed-out subscription as connected', async () => {
  fixture([entry('openai-codex', 'subscription', false, { configured: true, auth_state: 'signed_out' })]);
  fireEvent.click(await screen.findByRole('button', { name: 'Manage OpenAI (ChatGPT subscription)' }));
  expect(await screen.findAllByText('Not signed in')).not.toHaveLength(0);
  expect(screen.queryByRole('button', { name: 'Disconnect provider' })).toBeNull();
  expect(screen.queryByLabelText('API key')).toBeNull();
});

it('aborts the local sign-in wait when the dialog unmounts', async () => {
  const f = fixture();
  f.client.providers.login.begin.mockImplementationOnce(() => new Promise(() => {}));
  fireEvent.click(await screen.findByRole('button', { name: 'Connect OpenAI (ChatGPT subscription)' }));
  fireEvent.click(await screen.findByRole('button', { name: 'Sign in', exact: true }));
  await waitFor(() => expect(f.client.providers.login.begin).toHaveBeenCalledOnce());
  const [options] = f.client.providers.login.begin.mock.calls[0] as unknown as [{ signal: AbortSignal }];
  f.unmount();
  expect(options.signal.aborted).toBe(true);
  expect(f.client.providers.login.begin).toHaveBeenCalledOnce();
});

it('offers an explicit new-session default after connecting without overwriting it during authentication', async () => {
  const connected = entry('openrouter', 'none', false); connected.suggested_model = 'coding-model';
  const f = fixture([connected]);
  fireEvent.click(await screen.findByRole('button', { name: 'Connect OpenRouter' }));
  fireEvent.change(await screen.findByLabelText('API key'), { target: { value: 'test-key' } });
  fireEvent.click(screen.getByRole('button', { name: 'Connect', exact: true }));
  fireEvent.click(await screen.findByRole('button', { name: 'Use coding-model for new sessions' }));
  await waitFor(() => expect(f.client.configuration.update).toHaveBeenCalledExactlyOnceWith({ revision: '7', default_model: 'coding-model', default_provider: 'openrouter', default_effort: '' }, { signal: expect.any(AbortSignal) }));
});

it('keeps a failed API key ephemeral while preserving the connection form for retry', async () => {
  const f = fixture([entry('openrouter', 'none', false)]);
  f.client.providers.setKey.mockRejectedValueOnce(new Error('The API key was rejected. Enter a new key.'));
  fireEvent.click(await screen.findByRole('button', { name: 'Connect OpenRouter' }));
  fireEvent.change(await screen.findByLabelText('API key'), { target: { value: 'rejected-private-key' } });
  fireEvent.click(screen.getByRole('button', { name: 'Connect', exact: true }));
  await screen.findByText('The API key was rejected. Enter a new key.');
  expect((screen.getByLabelText('API key') as HTMLInputElement).value).toBe('');
  expect(JSON.stringify(f.queries.getQueryCache().getAll().map(query => query.state.data))).not.toContain('rejected-private-key');
  expect(f.client.providers.setKey).toHaveBeenCalledOnce();
});

it('observes an existing login, presents a human state, and copies its code without beginning another', async () => {
  const f = fixture();
  const flow: ProviderLoginStatus = { flow_id: 'existing', provider: 'openai-codex', state: 'pending', user_code: 'CODE-123', verification_url: 'https://auth.openai.com/device', teams: [], projects: [], expires_at: '2099-01-01T00:00:00Z' };
  await waitFor(() => expect(f.client.providers.list).toHaveBeenCalled());
  await act(async () => { f.queries.setQueryData(['provider-login-flows', 'host'], { flows: [flow] }); });
  fireEvent.click(await screen.findByRole('button', { name: 'Continue sign-in' }));
  expect(await screen.findByText('Waiting for sign-in')).toBeTruthy();
  fireEvent.click(screen.getByRole('button', { name: 'Copy code' }));
  await screen.findByRole('button', { name: 'Code copied' });
  expect(f.runtime.platform.copy).toHaveBeenCalledExactlyOnceWith('CODE-123');
  expect(f.client.providers.login.begin).not.toHaveBeenCalled();
});

it('reconciles current host flows before beginning another login from a stale dialog', async () => {
  const f = fixture();
  await waitFor(() => expect(f.client.providers.login.list).toHaveBeenCalledOnce());
  fireEvent.click(await screen.findByRole('button', { name: 'Connect OpenAI (ChatGPT subscription)' }));
  const flow: ProviderLoginStatus = { flow_id: 'began-while-detached', provider: 'openai-codex', state: 'authorizing', teams: [], projects: [], expires_at: '2099-01-01T00:00:00Z' };
  f.client.providers.login.list.mockResolvedValue({ flows: [flow] });
  fireEvent.click(screen.getByRole('button', { name: 'Sign in', exact: true }));
  await screen.findByText('Waiting for sign-in');
  expect(f.client.providers.login.begin).not.toHaveBeenCalled();
});

it('opens a delayed verification URL once for an explicitly initiated login, retaining a fallback button', async () => {
  const f = fixture();
  fireEvent.click(await screen.findByRole('button', { name: 'Connect OpenAI (ChatGPT subscription)' }));
  fireEvent.click(screen.getByRole('button', { name: 'Sign in', exact: true }));
  await waitFor(() => expect(f.client.providers.login.begin).toHaveBeenCalledOnce());
  const flow: ProviderLoginStatus = { flow_id: 'new-login', provider: 'openai-codex', state: 'pending', user_code: 'CODE', verification_url: 'https://auth.openai.com/device', teams: [], projects: [], expires_at: '2099-01-01T00:00:00Z' };
  await act(async () => { f.queries.setQueryData(['provider-login-flows', 'host'], { flows: [flow] }); });
  expect(await screen.findByRole('button', { name: 'Open verification page' })).toBeTruthy();
  expect(f.runtime.platform.openExternal).toHaveBeenCalledExactlyOnceWith('https://auth.openai.com/device');
  await act(async () => { f.queries.setQueryData(['provider-login-flows', 'host'], { flows: [{ ...flow }] }); });
  expect(f.runtime.platform.openExternal).toHaveBeenCalledOnce();
});

it('discovers on explicit settings refresh and invalidates only this host', async () => {
  const f = fixture();
  await screen.findByRole('button', { name: 'Manage OpenRouter' });
  expect(f.client.providers.discover).not.toHaveBeenCalled();
  f.queries.setQueryDefaults(['configuration'], { gcTime: Infinity });
  f.queries.setQueryDefaults(['provider-catalogs'], { gcTime: Infinity });
  f.queries.setQueryData(['configuration', 'host'], { revision: '7' });
  f.queries.setQueryData(['provider-catalogs', 'host', 'unobserved-provider'], { result: {} });
  f.queries.setQueryData(['configuration', 'other-host'], { revision: '20' });
  fireEvent.click(screen.getByRole('button', { name: 'Refresh', exact: true }));
  await waitFor(() => expect(f.client.providers.discover).toHaveBeenCalledExactlyOnceWith({ signal: expect.any(AbortSignal) }));
  await waitFor(() => expect(f.queries.getQueryState(['configuration', 'host'])?.isInvalidated).toBe(true));
  expect(f.queries.getQueryState(['provider-catalogs', 'host', 'unobserved-provider'])?.isInvalidated).toBe(true);
  expect(f.queries.getQueryState(['configuration', 'other-host'])?.isInvalidated).toBe(false);
  expect(f.client.configuration.update).not.toHaveBeenCalled();
});

it('opens setup with discovery but does not repeat discovery during inventory refresh', async () => {
  const f = fixture([entry('openrouter', 'none', false)], { setup: true });
  await screen.findByRole('button', { name: 'Connect OpenRouter' });
  expect(f.client.providers.discover).toHaveBeenCalledOnce();
  await act(async () => { await f.queries.invalidateQueries({ queryKey: ['provider-list', 'host'] }); });
  expect(f.client.providers.discover).toHaveBeenCalledOnce();
  expect(f.client.configuration.update).not.toHaveBeenCalled();
});

it('falls back to read-only inventory for a daemon without provider discovery', async () => {
  const f = fixture([entry('openrouter', 'none', false)], { setup: true, discovery: false });
  await screen.findByRole('button', { name: 'Connect OpenRouter' });
  expect(f.client.supports).toHaveBeenCalledWith('rpc', 'provider.discover');
  expect(f.client.providers.discover).not.toHaveBeenCalled();
  expect(f.client.providers.list.mock.calls.length).toBeGreaterThan(1);
  expect(f.client.configuration.update).not.toHaveBeenCalled();
  expect(screen.queryByText(/saved|configured automatically/i)).toBeNull();
});

it('keeps a key draft and focus when discovery refreshes the same provider', async () => {
  const provider = entry('openrouter', 'none', false);
  const f = fixture([provider]);
  let finish!: (value: ProviderList) => void;
  f.client.providers.discover.mockImplementationOnce(() => new Promise(resolve => { finish = resolve; }));
  await screen.findByRole('button', { name: 'Connect OpenRouter' });
  fireEvent.click(screen.getByRole('button', { name: 'Refresh', exact: true }));
  await waitFor(() => expect(f.client.providers.discover).toHaveBeenCalledOnce());
  fireEvent.click(screen.getByRole('button', { name: 'Connect OpenRouter' }));
  const input = await screen.findByLabelText('API key') as HTMLInputElement;
  input.focus();
  fireEvent.change(input, { target: { value: 'unsaved-fixture-key' } });
  const result = { revision: '8', providers: [provider] };
  f.client.providers.list.mockResolvedValue(result);
  await act(async () => { finish(result); });
  expect(screen.getByLabelText('API key')).toBe(input);
  expect(input.value).toBe('unsaved-fixture-key');
  expect(document.activeElement).toBe(input);
  expect(JSON.stringify(f.queries.getQueryCache().getAll().map(query => query.state.data))).not.toContain('unsaved-fixture-key');
});

it('shows failed discovery persistence in settings while retaining usable routes', async () => {
  const f = fixture();
  const result: ProviderList = { revision: '7', providers: [entry('openrouter', 'environment', true, { configured: false })], discovery_error: 'Provider definitions could not be saved.' };
  f.client.providers.discover.mockResolvedValueOnce(result);
  await screen.findByRole('button', { name: 'Manage OpenRouter' });
  fireEvent.click(screen.getByRole('button', { name: 'Refresh', exact: true }));
  expect(await screen.findByText('Provider definitions could not be saved.')).toBeTruthy();
  expect(screen.getByRole('button', { name: 'Manage OpenRouter' })).toBeTruthy();
  expect(f.client.providers.discover).toHaveBeenCalledOnce();
  expect(f.client.providers.setKey).not.toHaveBeenCalled();
});

it('retires pending discovery on host changes and ignores a late result', async () => {
  const f = fixture();
  let finish!: (value: ProviderList) => void;
  f.client.providers.discover.mockImplementationOnce(() => new Promise(resolve => { finish = resolve; }));
  await screen.findByRole('button', { name: 'Manage OpenRouter' });
  fireEvent.click(screen.getByRole('button', { name: 'Refresh', exact: true }));
  await waitFor(() => expect(f.client.providers.discover).toHaveBeenCalledOnce());
  const [options] = f.client.providers.discover.mock.calls[0] as unknown as [{ signal: AbortSignal }];
  f.changeHost('other-host');
  expect(options.signal.aborted).toBe(true);
  await act(async () => { finish({ revision: 'old-host-late-result', providers: [] }); });
  expect(f.queries.getQueryData<ProviderList>(['provider-list', 'other-host'])?.revision).toBe('7');
  expect(f.queries.getQueryData<ProviderList>(['provider-list', 'host'])?.revision).not.toBe('old-host-late-result');
  expect(screen.queryByRole('alert')).toBeNull();
});

it('retires discovery when disconnected and rediscovers on setup reconnect', async () => {
  const f = fixture([entry('openrouter', 'none', false)], { setup: true });
  await screen.findByRole('button', { name: 'Connect OpenRouter' });
  expect(f.client.providers.discover).toHaveBeenCalledOnce();
  f.client.providers.discover.mockImplementationOnce(() => new Promise(() => {}));
  fireEvent.click(screen.getByRole('button', { name: 'Refresh', exact: true }));
  await waitFor(() => expect(f.client.providers.discover).toHaveBeenCalledTimes(2));
  const [options] = f.client.providers.discover.mock.calls[1] as unknown as [{ signal: AbortSignal }];
  f.offline();
  expect(options.signal.aborted).toBe(true);
  expect((screen.getByRole('button', { name: 'Refresh', exact: true }) as HTMLButtonElement).disabled).toBe(true);
  f.reconnect();
  await waitFor(() => expect(f.client.providers.discover).toHaveBeenCalledTimes(3));
  await waitFor(() => expect((screen.getByRole('button', { name: 'Refresh', exact: true }) as HTMLButtonElement).disabled).toBe(false));
});

it('keeps the setup refresh disabled while its initial discovery is pending', async () => {
  const f = fixture([entry('openrouter', 'none', false)], { setup: true });
  f.client.providers.discover.mockImplementationOnce(() => new Promise(() => {}));
  await waitFor(() => expect(f.client.providers.discover).toHaveBeenCalledOnce());
  expect((screen.getByRole('button', { name: 'Refresh', exact: true }) as HTMLButtonElement).disabled).toBe(true);
});

it('reenables refresh when the hook changes hosts without remounting during discovery', async () => {
  const f = fixture(undefined, { unkeyed: true });
  f.client.providers.discover.mockImplementationOnce(() => new Promise(() => {}));
  await screen.findByRole('button', { name: 'Manage OpenRouter' });
  fireEvent.click(screen.getByRole('button', { name: 'Refresh', exact: true }));
  await waitFor(() => expect(f.client.providers.discover).toHaveBeenCalledOnce());
  const [options] = f.client.providers.discover.mock.calls[0] as unknown as [{ signal: AbortSignal }];
  f.changeHost('other-host');
  expect(options.signal.aborted).toBe(true);
  await waitFor(() => expect((screen.getByRole('button', { name: 'Refresh', exact: true }) as HTMLButtonElement).disabled).toBe(false));
  fireEvent.click(screen.getByRole('button', { name: 'Refresh', exact: true }));
  await waitFor(() => expect(f.client.providers.discover).toHaveBeenCalledTimes(2));
});
