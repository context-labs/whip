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

beforeEach(() => vi.stubGlobal('matchMedia', vi.fn(() => ({ matches: false, addEventListener() {}, removeEventListener() {} }))));
afterEach(() => { vi.unstubAllGlobals(); vi.useRealTimers(); });
type Entry = NonNullable<ProviderList['providers']>[number];
const entry = (id: string, source: string, available: boolean, extra: Partial<Entry['status']> = {}): Entry => ({
  id, name: id === 'openrouter' ? 'OpenRouter' : id === 'openai-codex' ? 'OpenAI (ChatGPT subscription)' : id,
  custom: id === 'custom', methods: id === 'openai-codex' ? ['login'] : ['api_key'],
  status: { provider: id, key_source: source, available, configured: available, auth_state: available ? 'connected' : 'key_required', warnings: [], ...extra },
});

function fixture(entries: Entry[] = [entry('openrouter', 'environment', true, { environment_variable: 'OPENROUTER_API_KEY' }), entry('openai-codex', 'none', false)]) {
  const queries = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0, staleTime: Infinity } } });
  const config = { revision: '7', disabled_providers: ['unrelated'], default_model: 'fixture', default_provider: 'openrouter', default_effort: '', compact_model: '', compact_provider: '', compact_percent: 75, goal_max_rounds: 8, max_retries: 2, import_claude: true, import_codex: true };
  const client = {
    getSnapshot: () => ({ state: 'connected', info: { runtime_id: 'host' } }),
    providers: {
      catalogs: vi.fn(async () => ({ result: { models: {}, providers: {}, catalogs: {} } })),
      list: vi.fn(async () => ({ revision: '7', default_provider: 'openrouter', providers: entries })), setKey: vi.fn(async () => ({})),
      disconnect: vi.fn(async () => ({ warnings: [] })), rotateKey: vi.fn(async () => ({})),
      login: { list: vi.fn(async () => ({ flows: [] as ProviderLoginStatus[] })), begin: vi.fn(async () => ({})) },
    },
    configuration: { get: vi.fn(async () => config), update: vi.fn(async () => ({})) },
  };
  const runtime = { queries, report: vi.fn(), connections: { host: () => ({ name: 'Remote workstation' }) } } as unknown as AppRuntime;
  const wrap = (node: ReactNode) => <RuntimeContext.Provider value={runtime}><ThemeProvider initialTheme="light"><UIProvider><QueryClientProvider client={queries}>{node}</QueryClientProvider></UIProvider></ThemeProvider></RuntimeContext.Provider>;
  const mounted = render(wrap(<ProvidersSettings client={client as unknown as WhipClient} enabled />));
  return { client, config, queries, ...mounted,
    offline() { mounted.rerender(wrap(<ProvidersSettings client={client as unknown as WhipClient} enabled={false} />)); },
    changeHost(id: string) {
      client.getSnapshot = () => ({ state: 'connected', info: { runtime_id: id } });
      mounted.rerender(wrap(<ProvidersSettings client={client as unknown as WhipClient} enabled />));
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

it('uses the detected host environment only after an explicit connection choice', async () => {
  const provider = entry('openrouter', 'literal', true);
  provider.methods = ['api_key', 'environment'];
  const f = fixture([provider]);
  fireEvent.click(await screen.findByRole('button', { name: 'Manage OpenRouter' }));
  fireEvent.click(await screen.findByRole('button', { name: 'Use host environment' }));
  await screen.findByText('OpenRouter now uses the host environment.');
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
