import { StrictMode } from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { NativeSurfaceProvider, UIProvider } from '@whip/ui';
import { BrowserWorkspace } from '../src/browser-workspace';
import { BrowserPreviewControls } from '../src/browser-preview-controls';
import { browserPreviewAddress } from '../src/browser-address';
import type { BrowserEvent, BrowserInventory, BrowserPlatform, BrowserTabState } from '../src/browser-types';
import { RuntimeContext } from '../src/context';
import type { AppRuntime } from '../src/runtime';
import { SessionTabs } from '../src/session-tabs';
import type { HostConnection } from '../src/hosts';

const { navigate } = vi.hoisted(() => {
  globalThis.ResizeObserver = class { observe() {} unobserve() {} disconnect() {} };
  return { navigate: vi.fn(async () => undefined) };
});
vi.mock('@tanstack/react-router', async importOriginal => ({ ...await importOriginal<typeof import('@tanstack/react-router')>(), useNavigate: () => navigate }));
const cleanups: (() => void)[] = [];
afterEach(() => { cleanups.splice(0).forEach(cleanup => cleanup()); navigate.mockClear(); });
function deferred<T>() { let resolve!: (value: T) => void; const promise = new Promise<T>(done => { resolve = done; }); return { promise, resolve }; }
const request = { connectionId: 'ssh_saved', runtimeId: 'runtime_verified', projectId: 'cwd:/project/one', url: 'http://127.0.0.1:3000/' };
function pageFor(id = 'preview_page'): BrowserTabState { return { id, generation: `generation_${id}`, documentGeneration: 1, status: 'suspended', url: request.url, title: 'Preview', environmentId: 'preview_environment', loading: false, canGoBack: false, canGoForward: false, zoomFactor: 1 }; }
function fixture(enabled = true) {
  const tabs = new SessionTabs();
  let snapshot: BrowserInventory = { epoch: 'epoch_1', revision: 0, tabs: [] };
  let listener = (_event: BrowserEvent) => {};
  const native = {
    version: 1 as const, snapshot: vi.fn(async () => snapshot), restore: vi.fn(async () => snapshot),
    create: vi.fn(async () => pageFor()),
    createPreview: vi.fn(async (_input: { epoch: string } & typeof request): Promise<BrowserTabState | undefined> => {
      const page = pageFor(); snapshot = { ...snapshot, revision: snapshot.revision + 1, tabs: [...snapshot.tabs, page] };
      listener({ kind: 'snapshot', snapshot }); return page;
    }),
    admitted: vi.fn(async () => {}), present: vi.fn(async (_input: Parameters<BrowserPlatform['present']>[0]) => {}),
    act: vi.fn(async () => {}), close: vi.fn(async () => ({ status: 'closed' as const })),
    onEvent: (callback: (event: BrowserEvent) => void) => { listener = callback; return () => { listener = () => {}; }; },
  };
  const report = vi.fn(), browser = new BrowserWorkspace(enabled ? native : undefined, tabs, report);
  const hostListeners = new Set<() => void>();
  const catalog = { status: 'ready', page: { items: [
    { id: 'historic_root', cwd: '/project/one', title: 'No Browser module required' },
    { id: 'other_root', cwd: '/project/one', title: 'Duplicate project' },
    { id: 'bad_root', cwd: 'relative/path', title: 'Not an absolute project' },
  ] } };
  const host = (id: string, kind: 'ssh' | 'url' | 'local') => ({ id, name: id === 'ssh_saved' ? 'Build server' : kind, state: 'connected', runtimeId: request.runtimeId,
    profile: { id, target: { kind } }, client: {}, list: { getSnapshot: () => catalog, subscribe: () => () => {} } }) as unknown as HostConnection;
  let state = { hosts: [host('ssh_saved', 'ssh'), host('url_saved', 'url'), host('local_saved', 'local')] };
  const runtime = { browser, tabs, platform: { browser: enabled ? native : undefined }, getSnapshot: () => state,
    subscribe: (fn: () => void) => { hostListeners.add(fn); return () => { hostListeners.delete(fn); }; },
  } as unknown as AppRuntime;
  const connected = (value: boolean) => { state = { hosts: state.hosts.map(host => host.id === 'ssh_saved' ? { ...host, state: value ? 'connected' : 'disconnected' } : host) }; hostListeners.forEach(fn => fn()); };
  cleanups.push(() => browser.dispose());
  return { tabs, browser, native, runtime, connected, report };
}
function mount(f: ReturnType<typeof fixture>, acquire?: () => { ready: Promise<void>; release(): void }) {
  const queries = new QueryClient({ defaultOptions: { queries: { retry: false } } }); cleanups.push(() => queries.clear());
  return render(<StrictMode><RuntimeContext.Provider value={f.runtime}><QueryClientProvider client={queries}><NativeSurfaceProvider acquire={acquire}><UIProvider><BrowserPreviewControls/></UIProvider></NativeSurfaceProvider></QueryClientProvider></RuntimeContext.Provider></StrictMode>);
}
async function chooseProject() {
  fireEvent.click(screen.getByRole('button', { name: 'Open SSH preview…' }));
  fireEvent.click(await screen.findByRole('combobox', { name: 'SSH host' }));
  expect(screen.queryByRole('option', { name: 'url' })).toBeNull();
  expect(screen.queryByRole('option', { name: 'local' })).toBeNull();
  fireEvent.click(await screen.findByRole('option', { name: 'Build server' }));
  fireEvent.click(screen.getByRole('combobox', { name: 'Project' }));
  expect(screen.queryByRole('option', { name: 'relative/path' })).toBeNull();
  expect(screen.getAllByRole('option', { name: '/project/one' })).toHaveLength(1);
  fireEvent.click(screen.getByRole('option', { name: '/project/one' }));
}

describe('literal human preview addresses', () => {
  it('preserves scheme and accepts only exact literal loopback', () => {
    expect(browserPreviewAddress('http://127.0.0.1:3000/a')).toBe('http://127.0.0.1:3000/a');
    expect(browserPreviewAddress('https://[::1]:444')).toBe('https://[::1]:444/');
    for (const url of ['http://localhost:3000', 'http://127.1:3000', 'http://2130706433:3000', 'http://127.0.0.2:3000', 'http://example.com', 'http://127.0.0.1.evil:3000', 'http://user@127.0.0.1:3000', 'file:///etc/passwd', '127.0.0.1:3000', 'http://127.0.0.1:0', 'http://[::1]:65536', 'http://127.0.0.1/\n']) expect(() => browserPreviewAddress(url)).toThrow();
  });
});
describe('human preview admission', () => {
  it('does not present a provisional page before admission ACK', async () => {
    const f = fixture(), admitted = deferred<void>(); f.native.admitted.mockReturnValue(admitted.promise);
    const opening = f.browser.createPreview(request, 'main');
    await waitFor(() => expect(f.native.admitted).toHaveBeenCalled());
    expect(f.tabs.workspace().tabs).toMatchObject([{ id: 'preview_page', environmentId: 'preview_environment' }]);
    const element = document.createElement('div'); document.body.append(element); cleanups.push(() => element.remove());
    vi.spyOn(element, 'getClientRects').mockReturnValue([{}] as unknown as DOMRectList);
    vi.spyOn(element, 'getBoundingClientRect').mockReturnValue({ x: 1, y: 2, width: 300, height: 200 } as DOMRect);
    const stop = f.browser.register('preview_page', element); cleanups.push(stop);
    const hold = f.browser.acquireOverlay(); await hold.ready;
    expect(f.native.present.mock.calls.at(-1)![0].slots).toEqual([]);
    admitted.resolve(); await opening; hold.release();
    await waitFor(() => expect(f.native.present.mock.calls.at(-1)![0].slots).toMatchObject([{ tabId: 'preview_page' }]));
  });
  it('leaves no workspace tab or admission on native cancellation', async () => {
    const f = fixture(); f.native.createPreview.mockResolvedValue(undefined);
    expect(await f.browser.createPreview(request, 'main')).toBeUndefined();
    expect(f.tabs.workspace().tabs).toEqual([]); expect(f.native.admitted).not.toHaveBeenCalled(); expect(f.native.close).not.toHaveBeenCalled();
  });
  it('rejects full capacity before confirmation and cleans up if capacity fills while confirmation waits', async () => {
    const full = fixture(); for (let i = 0; i < 8; i++) full.tabs.openBrowser({ id: `page_${i}`, url: 'about:blank' });
    await expect(full.browser.createPreview(request, 'main')).rejects.toThrow('8 Browser'); expect(full.native.createPreview).not.toHaveBeenCalled();
    const f = fixture(), confirmed = deferred<void>(), create = f.native.createPreview.getMockImplementation()!;
    f.native.createPreview.mockImplementation(async input => { await confirmed.promise; return create(input); });
    const opening = f.browser.createPreview(request, 'main');
    await waitFor(() => expect(f.native.createPreview).toHaveBeenCalled());
    for (let i = 0; i < 8; i++) f.tabs.openBrowser({ id: `page_${i}`, url: 'about:blank' });
    confirmed.resolve(); await expect(opening).rejects.toThrow('8 Browser');
    expect(f.native.admitted).not.toHaveBeenCalled(); expect(f.native.close).toHaveBeenCalledWith({ epoch: 'epoch_1', tabId: 'preview_page', generation: 'generation_preview_page' });
    expect(f.tabs.workspace().tabs).toHaveLength(8);
  });
  it('discards provisional workspace metadata and closes after native route-commit failure', async () => {
    const f = fixture(); f.native.admitted.mockRejectedValue(new Error('SSH connection changed'));
    await expect(f.browser.createPreview(request, 'main')).rejects.toThrow('SSH connection changed');
    expect(f.tabs.workspace().tabs).toEqual([]); expect(f.native.close).toHaveBeenCalledOnce();
  });
});
describe('human SSH preview controls', () => {
  it('chooses only a connected SSH project without a root, unmounts the dialog before confirmation, and opens the admitted tab', async () => {
    const f = fixture(), create = f.native.createPreview.getMockImplementation()!;
    f.native.createPreview.mockImplementation(async input => { expect(screen.queryByRole('dialog', { name: 'Open SSH preview' })).toBeNull(); return create(input); });
    mount(f); await chooseProject(); expect(f.native.createPreview).not.toHaveBeenCalled();
    expect(screen.queryByRole('combobox', { name: 'Conversation' })).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'Continue to native confirmation' }));
    await waitFor(() => expect(navigate).toHaveBeenCalledWith(expect.objectContaining({ to: '/browser/$viewId', params: { viewId: 'preview_page' } })));
    expect(f.native.createPreview).toHaveBeenCalledExactlyOnceWith({ ...request, epoch: 'epoch_1' });
    expect(f.native.admitted).toHaveBeenCalledOnce();
  });
  it('rejects nonliteral hosts inline and treats native cancel as a non-error outcome', async () => {
    const f = fixture(); f.native.createPreview.mockResolvedValue(undefined); mount(f); await chooseProject();
    fireEvent.change(screen.getByRole('textbox', { name: 'Preview URL' }), { target: { value: 'http://localhost:3000' } });
    fireEvent.click(screen.getByRole('button', { name: 'Continue to native confirmation' }));
    expect((await screen.findByRole('alert')).textContent).toContain('literal'); expect(f.native.createPreview).not.toHaveBeenCalled();
    fireEvent.change(screen.getByRole('textbox', { name: 'Preview URL' }), { target: { value: 'https://[::1]:444' } });
    fireEvent.click(screen.getByRole('button', { name: 'Continue to native confirmation' }));
    expect(await screen.findByText('SSH preview was not opened.')).not.toBeNull(); expect(screen.queryByRole('alert')).toBeNull(); expect(navigate).not.toHaveBeenCalled();
  });
  it('requires native hide ACK before exposing the picker and balances its hold on close', async () => {
    const f = fixture(), hidden = deferred<void>(), release = vi.fn(); mount(f, () => ({ ready: hidden.promise, release }));
    fireEvent.click(screen.getByRole('button', { name: 'Open SSH preview…' }));
    expect(screen.queryByRole('dialog')).toBeNull(); expect(f.native.createPreview).not.toHaveBeenCalled();
    await act(async () => hidden.resolve()); expect(await screen.findByRole('dialog')).not.toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'Close' })); await waitFor(() => expect(release).toHaveBeenCalled());
    expect(f.native.createPreview).not.toHaveBeenCalled();
  });
  it('clears project selection on disconnect and never begins a preview automatically after reconnect', async () => {
    const f = fixture(); mount(f); await chooseProject(); act(() => f.connected(false));
    expect(screen.queryByRole('button', { name: 'Continue to native confirmation' })).toBeNull();
    act(() => f.connected(true));
    expect((screen.getByRole('button', { name: 'Continue to native confirmation' }) as HTMLButtonElement).disabled).toBe(true);
    expect(f.native.createPreview).not.toHaveBeenCalled();
  });
  it('keeps unsupported web controls inert', () => {
    const f = fixture(false); mount(f); expect((screen.getByRole('button', { name: 'Open SSH preview…' }) as HTMLButtonElement).disabled).toBe(true);
    fireEvent.click(screen.getByRole('button', { name: 'Open SSH preview…' })); expect(screen.queryByRole('dialog')).toBeNull();
  });
});
