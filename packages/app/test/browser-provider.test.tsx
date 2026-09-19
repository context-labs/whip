import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { NativeSurfaceProvider, UIProvider } from '@whip/ui';
import type { BrowserSelection, BrowserSelectionOptions, WhipClient } from '@whip/sdk';
import type { BrowserAgentBridge, BrowserAgentEvent } from '../src/browser-agent-types';
import type { BrowserWorkspace } from '../src/browser-workspace';
import { BrowserAssociations, browserProjectId } from '../src/browser-provider';
import { BrowserProviderControls } from '../src/browser-provider-controls';
import { RuntimeContext } from '../src/context';
import type { AppRuntime } from '../src/runtime';
import type { HostConnection, HostConnections } from '../src/hosts';
import { SessionTabs } from '../src/session-tabs';

vi.hoisted(() => { globalThis.ResizeObserver = class { observe() {} unobserve() {} disconnect() {} }; });
const cleanups: (() => void)[] = [];
afterEach(() => { cleanups.splice(0).forEach(cleanup => cleanup()); });
function deferred<T>() { let resolve!: (value: T) => void; const promise = new Promise<T>(done => { resolve = done; }); return { promise, resolve }; }
const roots = [{ id: 'root_older', title: 'Older explicit root', cwd: '/project/one' }, { id: 'root_latest', title: 'Newest root', cwd: '/project/two' }];
const choose = { hostId: 'local', rootId: roots[0]!.id, title: roots[0]!.title, tabId: 'human_tab' };
function fixture(kind: 'local' | 'ssh' | 'url' = 'local', enabled = true, discovery = false) {
  const tabs = new SessionTabs(); if (!discovery) tabs.openBrowser({ id: 'human_tab', url: 'https://example.com', titleHint: 'Human page' });
  const events = new Set<(event: BrowserAgentEvent) => void>(), hostListeners = new Set<() => void>();
  const preview = { host_id: 'ssh:host', host_identity: 'verified_runtime', connection_generation: 'connection_1', environment_id: 'environment_1', loopback: '127.0.0.1', ports: [] };
  const identity = { desktopId: 'desktop_1', windowId: 'window_1', createProfileId: 'profile_create', tabs: [
    { tab_id: 'human_tab', tab_generation: 'generation_human', profile_id: 'profile_human' },
    { tab_id: 'not_offered', tab_generation: 'generation_other', profile_id: 'profile_other' },
  ] };
  const bridge: BrowserAgentBridge = { identity: vi.fn(async () => identity), preview: vi.fn(async () => preview), select: vi.fn(async () => {}),
    ...(discovery ? { inventory: vi.fn() } : {}),
    dispatch: vi.fn(), cancel: vi.fn(), release: vi.fn(async () => {}), onEvent: listener => { events.add(listener); return () => { events.delete(listener); }; } };
  let options: BrowserSelectionOptions | undefined;
  const releases: ReturnType<typeof vi.fn>[] = [];
  const selection = () => {
    let active = true;
    const release = vi.fn(async () => { active = false; }); releases.push(release);
    return { provider: { provider_epoch: 'provider_1' }, get active() { return active; }, release } as BrowserSelection;
  };
  const select = vi.fn(async (_offer, _bridge, input) => { options = input; return selection(); });
  const client = { browser: { select }, sessions: { list: vi.fn(async () => ({ revision: '1', items: roots, next_cursor: null })) } } as unknown as WhipClient;
  const catalog = { status: 'ready' as const, page: { revision: '1', items: roots, next_cursor: null }, truncated: false };
  let host = { id: 'local', name: 'Selected host', runtimeId: 'verified_runtime', state: 'connected', client,
    profile: { id: kind === 'ssh' ? 'ssh:host' : 'local', target: kind === 'ssh' ? { kind, host: 'host' } : kind === 'url' ? { kind, endpoint: 'https://host/' } : { kind } },
    list: { subscribe: () => () => {}, getSnapshot: () => catalog },
  } as unknown as HostConnection;
  let state = { hosts: [host] };
  const abort = new AbortController();
  const hosts = { getSnapshot: () => state, subscribe: (listener: () => void) => { hostListeners.add(listener); return () => { hostListeners.delete(listener); }; },
    signal: () => abort.signal } as unknown as HostConnections;
  const close = vi.fn(async () => ({ status: 'closed' as const })), admit = vi.fn(async () => undefined);
  const browser = { platform: { close }, admit } as unknown as BrowserWorkspace;
  const report = vi.fn();
  const associations = new BrowserAssociations(enabled ? bridge : undefined, hosts, tabs, browser, report);
  const runtime = { browserAssociations: associations, connections: hosts, tabs, platform: { browserAgent: enabled ? bridge : undefined }, getSnapshot: () => state,
    subscribe: hosts.subscribe } as unknown as AppRuntime;
  const disconnect = () => { host = { ...host, state: 'disconnected' }; state = { hosts: [host] }; for (const listener of hostListeners) listener(); };
  const reconnect = () => { host = { ...host, state: 'connected' }; state = { hosts: [host] }; for (const listener of hostListeners) listener(); };
  const emit = (event: BrowserAgentEvent) => { for (const listener of events) listener(event); };
  cleanups.push(() => associations.dispose());
  return { tabs, associations, bridge, identity, preview, runtime, select, releases, selection, disconnect, reconnect, emit, close, admit, report, options: () => options };
}

describe('explicit Browser provider associations', () => {
  it('advertises a zero-tab destination without sharing pages or preview network, then admits the approved create', async () => {
    const f = fixture('local', true, true);
    const session = f.tabs.open('verified_runtime', 'root_older', 'Conversation');
    await waitFor(() => expect(f.associations.getSnapshot()[0]?.status).toBe('available'));
    expect(f.select).toHaveBeenCalledWith(expect.objectContaining({ version: 2, availability: true, root_id: 'root_older', offered_tabs: [], offered_preview_hosts: [] }), f.bridge, expect.any(Object));
    expect(f.tabs.workspace().tabs.some(tab => tab.kind === 'browser')).toBe(false);
    expect(f.admit).not.toHaveBeenCalled(); expect(f.bridge.preview).not.toHaveBeenCalled();
    f.emit({ kind: 'admission', commandId: 'approved', rootId: 'root_older', providerEpoch: 'provider_1', epoch: 'epoch', tab: { id: 'created', generation: 'generation', url: 'about:blank' } } as BrowserAgentEvent);
    await waitFor(() => expect(f.admit).toHaveBeenCalledOnce());
    expect(f.admit.mock.calls[0]?.[2]).toBe('main'); expect(f.close).not.toHaveBeenCalled();
    expect(f.associations.getSnapshot()[0]?.status).toBe('selected');
    await f.associations.release(f.associations.getSnapshot()[0]!.key);
    f.tabs.openNew({}); await Promise.resolve();
    expect(f.select).toHaveBeenCalledOnce(); expect(f.tabs.workspace().tabs.some(tab => tab.id === session)).toBe(true);
  });
  it('re-advertises only inert availability after reconnect, without replaying a create', async () => {
    const f = fixture('local', true, true); f.tabs.open('verified_runtime', 'root_older');
    await waitFor(() => expect(f.associations.getSnapshot()[0]?.status).toBe('available'));
    f.disconnect(); f.reconnect();
    await waitFor(() => expect(f.select).toHaveBeenCalledTimes(2));
    expect(f.select.mock.calls[1]?.[0]).toMatchObject({ availability: true, offered_tabs: [], offered_preview_hosts: [] });
    expect(f.admit).not.toHaveBeenCalled(); expect(f.bridge.dispatch).not.toHaveBeenCalled();
  });
  it('offers exactly the chosen root and current tab, never newest/all tabs; release leaves human browsing intact', async () => {
    const f = fixture(); expect(f.select).not.toHaveBeenCalled();
    await f.associations.select(choose);
    expect(f.select).toHaveBeenCalledWith(expect.objectContaining({ root_id: 'root_older', version: 1, desktop_id: 'desktop_1', window_id: 'window_1',
      create_profile_id: 'profile_create', offered_tabs: [f.identity.tabs[0]], offered_preview_hosts: [] }), f.bridge, expect.any(Object));
    expect(f.associations.getSnapshot()[0]).toMatchObject({ status: 'selected', rootId: 'root_older', paneId: 'main' });
    expect(f.bridge.preview).not.toHaveBeenCalled();
    await f.associations.release(f.associations.getSnapshot()[0]!.key);
    expect(f.releases[0]).toHaveBeenCalledOnce(); expect(f.close).not.toHaveBeenCalled(); expect(f.tabs.workspace().tabs[0]?.id).toBe('human_tab');
  });
  it('disconnect releases the exact association and reconnect never implicitly selects it again', async () => {
    const f = fixture(); await f.associations.select(choose); f.disconnect();
    expect(f.associations.getSnapshot()[0]?.status).toBe('unavailable'); expect(f.releases[0]).toHaveBeenCalledOnce();
    f.reconnect(); expect(f.select).toHaveBeenCalledOnce(); expect(f.associations.getSnapshot()[0]?.status).toBe('unavailable');
    await f.associations.select(choose); expect(f.select).toHaveBeenCalledTimes(2); expect(f.associations.getSnapshot()[0]?.status).toBe('selected');
  });
  it('fences a selection whose native identity lookup races disconnect', async () => {
    const f = fixture(), pending = deferred<typeof f.identity>(); vi.mocked(f.bridge.identity).mockReturnValueOnce(pending.promise);
    const selecting = f.associations.select(choose); f.disconnect(); pending.resolve(f.identity);
    await expect(selecting).rejects.toThrow(); expect(f.select).not.toHaveBeenCalled(); expect(f.associations.getSnapshot()[0]?.status).toBe('unavailable');
    f.reconnect(); expect(f.select).not.toHaveBeenCalled();
  });
  it('releases a late bind result after explicit release instead of resurrecting selection', async () => {
    const f = fixture(), pending = deferred<BrowserSelection>(); f.select.mockReturnValueOnce(pending.promise);
    const selecting = f.associations.select(choose); await waitFor(() => expect(f.select).toHaveBeenCalledOnce());
    await f.associations.release(f.associations.getSnapshot()[0]!.key); const result = f.selection(); pending.resolve(result);
    await expect(selecting).rejects.toThrow('interrupted'); expect(result.active).toBe(false); expect(f.associations.getSnapshot()).toEqual([]);
  });
  it('routes admission to the pane captured at explicit selection and ignores duplicate delivery', async () => {
    const f = fixture(); await f.associations.select(choose); f.tabs.openNew({}); f.tabs.split('human_tab', 'right');
    const event = { kind: 'admission', commandId: 'cmd_1', rootId: 'root_older', providerEpoch: 'provider_1', epoch: 'epoch_1', tab: { id: 'created_tab', generation: 'tab_generation', url: 'about:blank' } } as BrowserAgentEvent;
    f.emit(event); await waitFor(() => expect(f.admit).toHaveBeenCalledOnce());
    expect(f.admit).toHaveBeenCalledWith(event.tab, { epoch: 'epoch_1', tabId: 'created_tab', generation: 'tab_generation' }, 'main');
    f.emit(event); await Promise.resolve(); expect(f.admit).toHaveBeenCalledOnce();
    f.emit({ ...event, commandId: 'cmd_old', providerEpoch: 'retired' }); await waitFor(() => expect(f.close).toHaveBeenCalledOnce());
    f.tabs.openBrowser({ id: 'created_tab', url: 'about:blank', titleHint: '' });
    await f.associations.release(f.associations.getSnapshot()[0]!.key);
    f.emit(event); await Promise.resolve(); expect(f.close).toHaveBeenCalledOnce();
  });
  it('reflects SDK revocation immediately through onError without silently reselecting', async () => {
    const f = fixture(); await f.associations.select(choose); f.options()?.onError?.(new Error('Provider revoked'));
    expect(f.associations.getSnapshot()[0]).toMatchObject({ status: 'error', error: 'Provider revoked' }); expect(f.select).toHaveBeenCalledOnce();
  });
  it('offers only explicitly chosen verified SSH metadata, never a URL-host substitute', async () => {
    const f = fixture('ssh'); await f.associations.select({ ...choose, preview: true, projectId: browserProjectId('/project/one') });
    expect(f.bridge.preview).toHaveBeenCalledWith({ connectionId: 'ssh:host', runtimeId: 'verified_runtime', projectId: 'cwd:/project/one', loopback: '127.0.0.1' });
    expect(f.select).toHaveBeenCalledWith(expect.objectContaining({ offered_preview_hosts: [f.preview] }), f.bridge, expect.objectContaining({ connectionId: 'ssh:host', projectId: 'cwd:/project/one' }));
    const url = fixture('url'); await expect(url.associations.select({ ...choose, preview: true, projectId: 'cwd:/project/one' })).rejects.toThrow('not a URL host');
    expect(url.bridge.preview).not.toHaveBeenCalled(); expect(url.select).not.toHaveBeenCalled();
  });
  it('rejects missing capability, absent tabs and unsafe project metadata', async () => {
    const f = fixture('local', false); await expect(f.associations.select(choose)).rejects.toThrow('desktop app');
    expect(browserProjectId('relative')).toBeUndefined(); expect(browserProjectId('/path\nwrong')).toBeUndefined(); expect(browserProjectId('/' + 'x'.repeat(508))).toBeUndefined();
    expect(browserProjectId('/project/one')).toBe('cwd:/project/one');
    const normal = fixture(); await expect(normal.associations.select({ ...choose, tabId: 'not-open' })).rejects.toThrow('no longer open'); expect(normal.select).not.toHaveBeenCalled();
  });
});

function mount(f: ReturnType<typeof fixture>, acquire?: () => { ready: Promise<void>; release(): void }) {
  const queries = new QueryClient({ defaultOptions: { queries: { retry: false } } }); cleanups.push(() => queries.clear());
  return render(<RuntimeContext.Provider value={f.runtime}><QueryClientProvider client={queries}><NativeSurfaceProvider acquire={acquire}><UIProvider><BrowserProviderControls tabId="human_tab"/></UIProvider></NativeSurfaceProvider></QueryClientProvider></RuntimeContext.Provider>);
}
async function option(label: string, name: RegExp) {
  fireEvent.click(screen.getByRole('combobox', { name: label }));
  fireEvent.click(await screen.findByRole('option', { name }));
}
describe('Browser access controls', () => {
  it('requires an explicit host and root, does not bind on dialog open, and exposes release', async () => {
    const f = fixture(); mount(f); fireEvent.click(screen.getByRole('button', { name: 'Conversation access…' }));
    await screen.findByRole('dialog', { name: 'Conversation Browser access' }); expect(f.select).not.toHaveBeenCalled();
    expect((screen.getByRole('button', { name: 'Offer Browser to conversation' }) as HTMLButtonElement).disabled).toBe(true);
    await option('Execution host', /Selected host/); await option('Conversation', /Older explicit root/);
    fireEvent.click(screen.getByRole('button', { name: 'Offer Browser to conversation' }));
    await waitFor(() => expect(f.select).toHaveBeenCalledOnce()); expect(f.select.mock.calls[0]![0].root_id).toBe('root_older');
    expect(await screen.findByText('Selected — permission requests may use this window')).not.toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'Release Browser access for Older explicit root' }));
    await waitFor(() => expect(f.associations.getSnapshot()).toEqual([])); expect(f.close).not.toHaveBeenCalled();
  });
  it('gates real selection controls behind the native-hide acknowledgement', async () => {
    const f = fixture(), hidden = deferred<void>(), release = vi.fn(); mount(f, () => ({ ready: hidden.promise, release }));
    fireEvent.click(screen.getByRole('button', { name: 'Conversation access…' })); expect(screen.queryByRole('dialog')).toBeNull();
    expect(f.bridge.identity).not.toHaveBeenCalled(); await act(async () => hidden.resolve());
    expect(await screen.findByRole('dialog')).not.toBeNull(); expect(f.select).not.toHaveBeenCalled();
  });
  it('shows fresh-session help on a historical-root rejection and never upgrades authority', async () => {
    const f = fixture(); f.select.mockRejectedValueOnce(new Error('Browser module unavailable on historical root')); mount(f);
    fireEvent.click(screen.getByRole('button', { name: 'Conversation access…' })); await option('Execution host', /Selected host/); await option('Conversation', /Older explicit root/);
    fireEvent.click(screen.getByRole('button', { name: 'Offer Browser to conversation' }));
    expect((await screen.findByRole('alert')).textContent).toContain('historical root'); expect(screen.getByText(/Start a fresh conversation/)).not.toBeNull();
    expect(f.select).toHaveBeenCalledOnce(); expect(f.associations.getSnapshot()[0]?.status).toBe('error');
  });
  it('keeps web capability safely disabled without attempting native selection', () => {
    const f = fixture('local', false); mount(f); expect((screen.getByRole('button', { name: 'Conversation access…' }) as HTMLButtonElement).disabled).toBe(true); expect(f.bridge.identity).not.toHaveBeenCalled(); expect(f.select).not.toHaveBeenCalled();
  });
});
