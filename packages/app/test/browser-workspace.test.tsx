import { createMemoryHistory } from '@tanstack/react-router';
import { createWhipApplication } from '../src';
import { StrictMode } from 'react';
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { NativeSurfaceProvider, useNativeOverlay, useNativeSurfacePresence, Dialog, Button } from '@whip/ui';
import { BrowserWorkspace } from '../src/browser-workspace';
import { browserAddress, browserURL } from '../src/browser-address';
import { SessionTabs, TAB_STORAGE_KEY, PRE_BROWSER_TAB_STORAGE_KEY, BROWSER_TAB_RECOVERY_KEY, sessionPanes, isBrowserTab } from '../src/session-tabs';
import { tabDestination, browserDestination } from '../src/session-tab-routing';
import type { BrowserEvent, BrowserInventory, BrowserPlatform, BrowserTabState } from '../src/browser-types';

vi.hoisted(() => { window.scrollTo = () => {}; globalThis.ResizeObserver = class { observe() {} unobserve() {} disconnect() {} }; });

const descriptor = { id: 'browser_one', url: 'https://example.com/', titleHint: 'Example' };
const stateFor = (id = descriptor.id): BrowserTabState => ({ id, generation: `generation_${id}`, documentGeneration: 1, status: 'ready', url: descriptor.url, title: descriptor.titleHint, loading: false, canGoBack: false, canGoForward: false, zoomFactor: 1 });
function deferred() { let resolve!: () => void; const promise = new Promise<void>(done => { resolve = done; }); return { promise, resolve }; }
function storage() { const data = new Map<string, string>(); return { keys: () => [...data.keys()], getItem: (key: string) => data.get(key) ?? null, setItem: (key: string, value: string) => { data.set(key, value); }, removeItem: (key: string) => { data.delete(key); } }; }
function host(tabs: SessionTabs) {
  let snapshot: BrowserInventory = { epoch: 'epoch_a', revision: 0, tabs: [] };
  let listener: (event: BrowserEvent) => void = () => {};
  const emit = (event: BrowserEvent) => { if (event.kind === 'snapshot') snapshot = event.snapshot; listener(event); };
  const platform: BrowserPlatform = {
    version: 1, snapshot: vi.fn(async () => snapshot),
    restore: vi.fn(async input => {
      if (Object.keys(input).some(key => !['epoch', 'tabs'].includes(key)) || input.tabs.some(tab => Object.keys(tab).some(key => !['id', 'url', 'titleHint', 'environmentId'].includes(key)))) throw new Error('Invalid browser request');
      snapshot = { ...snapshot, revision: snapshot.revision + 1, tabs: [...snapshot.tabs, ...input.tabs.filter(tab => !snapshot.tabs.some(item => item.id === tab.id)).map(tab => ({ ...stateFor(tab.id), url: tab.url, title: tab.titleHint ?? '' }))] }; return snapshot; }),
    create: vi.fn(async input => { const page = { ...stateFor('browser_created'), url: input.url }; emit({ kind: 'snapshot', snapshot: { ...snapshot, revision: snapshot.revision + 1, tabs: [...snapshot.tabs, page] } }); return page; }),
    admitted: vi.fn(async () => {}), present: vi.fn(async () => {}), act: vi.fn(async () => {}), close: vi.fn(async () => ({ status: 'closed' as const })),
    onEvent: callback => { listener = callback; return () => { listener = () => {}; }; },
  };
  const report = vi.fn(), browser = new BrowserWorkspace(platform, tabs, report);
  return { browser, platform, report, emit };
}
function slot() {
  const element = document.createElement('div'); document.body.append(element);
  vi.spyOn(element, 'getBoundingClientRect').mockReturnValue({ x: 10.5, y: 90.25, width: 400.75, height: 300.5 } as DOMRect);
  vi.spyOn(element, 'getClientRects').mockReturnValue([{}] as unknown as DOMRectList);
  return element;
}

describe('production Browser metadata and routing', () => {
  it('normalizes addresses without search, credential leakage or scheme downgrade', () => {
    expect(browserAddress('example.com')).toBe('https://example.com/');
    expect(browserAddress('localhost:5173/a')).toBe('http://localhost:5173/a');
    expect(browserAddress('[::1]:8080')).toBe('http://[::1]:8080/');
    expect(browserAddress('https://localhost:444')).toBe('https://localhost:444/');
    expect(browserAddress('')).toBe('about:blank');
    for (const address of ['find cats', 'javascript:alert(1)', 'file:///tmp/a', 'https://user:secret@example.com', 'data:text/html,x']) expect(() => browserAddress(address)).toThrow();
    expect(() => browserURL(`https://example.com/${'x'.repeat(8192)}`)).toThrow();
    expect(() => browserURL('https://example.com/\n')).toThrow();
  });
  it('preserves metadata in v3 without a capability and strips live authority', () => {
    const disk = storage(), tabs = new SessionTabs(disk);
    tabs.openBrowser({ ...descriptor, environmentId: 'ssh_hint', partition: 'privileged', attachmentId: 'old', runtimeId: 'host' } as typeof descriptor);
    const restored = new SessionTabs(disk).workspace().tabs[0]!;
    expect(restored).toEqual({ ...descriptor, kind: 'browser', environmentId: 'ssh_hint' });
    expect(JSON.parse(disk.getItem(TAB_STORAGE_KEY)!).version).toBe(3);
    expect(tabDestination(restored)).toMatchObject({ to: '/browser/$viewId', params: { viewId: descriptor.id } });
    expect(browserDestination(`/browser/${descriptor.id}`)).toBe(descriptor.id);
    expect(browserDestination('/browser/%')).toBeUndefined();
  });
  it('keeps a non-destructive pre-browser backup and recovers after an older parser drops the kind', () => {
    const disk = storage(), tabs = new SessionTabs(disk); tabs.openNew({});
    const before = disk.getItem(TAB_STORAGE_KEY)!; tabs.openBrowser(descriptor);
    expect(disk.getItem(PRE_BROWSER_TAB_STORAGE_KEY)).toBe(before);
    tabs.updateBrowser(descriptor.id, { url: 'https://example.org/', titleHint: 'Updated' });
    expect(disk.getItem(PRE_BROWSER_TAB_STORAGE_KEY)).toBe(before);
    disk.setItem(TAB_STORAGE_KEY, before);
    const upgraded = new SessionTabs(disk);
    expect(upgraded.browserRecovery()).toMatchObject([{ id: descriptor.id, url: 'https://example.org/' }]);
    upgraded.recoverBrowserTabs(); expect(upgraded.workspace().tabs.filter(isBrowserTab)).toHaveLength(1);
    upgraded.closeViews([descriptor.id]); upgraded.forgetBrowserHistory();
    expect(upgraded.browserRecovery()).toEqual([]); expect(disk.getItem(BROWSER_TAB_RECOVERY_KEY)).toBeNull();
    expect(disk.getItem(PRE_BROWSER_TAB_STORAGE_KEY)).toBe(before);
  });
  it('does not overwrite downgrade-orphan addresses when a different Browser is opened before recovery', () => {
    const disk = storage(), original = new SessionTabs(disk); original.openBrowser(descriptor);
    const oldClient = JSON.parse(disk.getItem(TAB_STORAGE_KEY)!); oldClient.workspace.layout.tabs = []; delete oldClient.workspace.layout.selected;
    disk.setItem(TAB_STORAGE_KEY, JSON.stringify(oldClient));
    const upgraded = new SessionTabs(disk); upgraded.openBrowser({ ...descriptor, id: 'browser_new', url: 'https://new.example/' });
    expect(upgraded.browserRecovery()).toEqual([expect.objectContaining({ id: descriptor.id, url: descriptor.url })]);
    expect(new SessionTabs(disk).browserRecovery()).toEqual([expect.objectContaining({ id: descriptor.id })]);
    upgraded.recoverBrowserTabs(); expect(upgraded.workspace().tabs.filter(isBrowserTab)).toHaveLength(2);
  });
  it('caps new creation at eight without dropping up to32 restored metadata entries', () => {
    const disk = storage(), tabs = new SessionTabs(disk);
    for (let index = 0; index < 8; index++) tabs.openBrowser({ ...descriptor, id: `browser_${index}` });
    expect(tabs.canOpenBrowser()).toBe(false); expect(() => tabs.openBrowser({ ...descriptor, id: 'extra' })).toThrow('8 Browser');
    const saved = JSON.parse(disk.getItem(TAB_STORAGE_KEY)!);
    saved.workspace.layout.tabs = Array.from({ length: 32 }, (_, index) => ({ ...descriptor, kind: 'browser', id: `browser_${index}` }));
    disk.setItem(TAB_STORAGE_KEY, JSON.stringify(saved));
    expect(new SessionTabs(disk).workspace().tabs).toHaveLength(32);
  });
  it('moves and splits one page identity, but reopens with a fresh ID', () => {
    const tabs = new SessionTabs(); tabs.openBrowser(descriptor); tabs.openNew({});
    expect(tabs.split(descriptor.id, 'right')).toBe(descriptor.id);
    expect(sessionPanes(tabs.workspace().layout)).toHaveLength(2);
    expect(tabs.workspace().tabs.filter(isBrowserTab)).toHaveLength(1);
    tabs.closeViews([descriptor.id]); const reopened = tabs.reopenView();
    expect(reopened).not.toBe(descriptor.id); expect(tabs.workspace().tabs.find(tab => tab.id === reopened)).toMatchObject({ kind: 'browser', url: descriptor.url });
  });
  it('does not write metadata again for unchanged native observation', () => {
    const tabs = new SessionTabs(); tabs.openBrowser(descriptor); const change = vi.fn(); tabs.subscribe(change);
    tabs.updateBrowser(descriptor.id, { url: descriptor.url, titleHint: descriptor.titleHint }); expect(change).not.toHaveBeenCalled();
  });
});

describe('production native workspace coordinator', () => {
  it('projects only restore wire metadata after New Browser creation and reaches presentation after admission', async () => {
    const tabs = new SessionTabs(), { browser, platform, report } = host(tabs);
    const element = slot(); let unregister: (() => void) | undefined;
    try {
      const created = await browser.create();
      expect(created).toMatchObject({ kind: 'browser', id: 'browser_created' });
      await waitFor(() => expect(platform.restore).toHaveBeenCalledWith({ epoch: 'epoch_a', tabs: [{ id: created.id, url: 'about:blank', titleHint: 'Example' }] }));
      expect(platform.admitted).toHaveBeenCalledExactlyOnceWith({ epoch: 'epoch_a', tabId: created.id, generation: `generation_${created.id}` });
      unregister = browser.register(created.id, element);
      await waitFor(() => expect(vi.mocked(platform.present).mock.calls.at(-1)?.[0].slots).toMatchObject([{ tabId: created.id }]));
      expect(report).not.toHaveBeenCalled();
    } finally { unregister?.(); browser.dispose(); element.remove(); }
  });
  it('projects only restore wire metadata when reloading persisted local and preview descriptors', async () => {
    const disk = storage(), before = new SessionTabs(disk);
    before.openBrowser(descriptor);
    before.openBrowser({ ...descriptor, id: 'browser_preview', environmentId: 'environment_one' });
    const restored = new SessionTabs(disk), { browser, platform, report } = host(restored);
    try {
      expect(restored.workspace().tabs.every(tab => tab.kind === 'browser')).toBe(true);
      await browser.start();
      expect(platform.restore).toHaveBeenCalledExactlyOnceWith({ epoch: 'epoch_a', tabs: [descriptor, { ...descriptor, id: 'browser_preview', environmentId: 'environment_one' }] });
      expect(vi.mocked(platform.restore).mock.calls[0]![0].tabs.map(tab => Object.keys(tab).sort())).toEqual([
        ['id', 'titleHint', 'url'], ['environmentId', 'id', 'titleHint', 'url'],
      ]);
      expect(platform.create).not.toHaveBeenCalled(); expect(platform.admitted).not.toHaveBeenCalled();
      expect(report).not.toHaveBeenCalled();
    } finally { browser.dispose(); }
  });
  it('acknowledges creation only after workspace admission and uses fresh target generations', async () => {
    const tabs = new SessionTabs(), { browser, platform } = host(tabs);
    vi.mocked(platform.admitted).mockImplementation(async target => { expect(tabs.workspace().tabs.some(tab => tab.id === target.tabId)).toBe(true); });
    const created = await browser.create('localhost:8080');
    expect(created.url).toBe('http://localhost:8080/'); expect(platform.admitted).toHaveBeenCalledOnce();
    await browser.act(created.id, { kind: 'reload' });
    expect(platform.act).toHaveBeenCalledWith({ epoch: 'epoch_a', tabId: created.id, generation: `generation_${created.id}`, action: { kind: 'reload' } }); browser.dispose();
  });
  it('removes provisional native resources if renderer admission fails', async () => {
    const disk = storage(), tabs = new SessionTabs(disk), { browser, platform } = host(tabs);
    vi.mocked(platform.admitted).mockRejectedValue(new Error('admission lost'));
    await expect(browser.create()).rejects.toThrow('admission lost');
    expect(platform.close).toHaveBeenCalledOnce(); expect(tabs.workspace().tabs).toHaveLength(0); expect(tabs.workspace().closed).toHaveLength(0); expect(new SessionTabs(disk).browserRecovery()).toHaveLength(0); browser.dispose();
  });
  it('admits provider-created pages in the originating pane without changing selection or focused pane', async () => {
    const tabs = new SessionTabs(); const first = tabs.openNew({}); tabs.openNew({}); tabs.split(first.id, 'right');
    const origin = sessionPanes(tabs.workspace().layout).find(pane => !pane.tabs.some(tab => tab.id === first.id))!;
    const focusedPaneId = tabs.workspace().focusedPaneId, selected = origin.selected;
    const { browser, platform } = host(tabs); await browser.start();
    const page = await platform.create({ epoch: 'epoch_a', url: 'https://example.com/' });
    await browser.admit(page, { epoch: 'epoch_a', tabId: page.id, generation: page.generation }, origin.id);
    expect(tabs.workspace().focusedPaneId).toBe(focusedPaneId);
    expect(sessionPanes(tabs.workspace().layout).find(pane => pane.id === origin.id)).toMatchObject({ selected, tabs: expect.arrayContaining([expect.objectContaining({ id: page.id, kind: 'browser' })]) });
    expect(platform.admitted).toHaveBeenCalledOnce(); browser.dispose();
  });
  it('rejects provider admission after its originating pane disappears, and never auto-admits snapshot pages', async () => {
    const tabs = new SessionTabs(), { browser, platform } = host(tabs); await browser.start();
    const page = await platform.create({ epoch: 'epoch_a', url: 'about:blank' });
    expect(tabs.workspace().tabs).toHaveLength(0);
    await expect(browser.admit(page, { epoch: 'epoch_a', tabId: page.id, generation: page.generation }, 'removed_pane')).rejects.toThrow('originating workspace pane');
    expect(platform.admitted).not.toHaveBeenCalled(); expect(platform.close).toHaveBeenCalledOnce(); browser.dispose();
  });
  it('rejects stale snapshot revisions, retired epochs and target events', async () => {
    const tabs = new SessionTabs(); tabs.openBrowser(descriptor); const { browser, emit } = host(tabs); await browser.start();
    const callback = vi.fn(); browser.onEvent(callback);
    emit({ kind: 'focused', epoch: 'epoch_a', tabId: descriptor.id, generation: 'stale' }); expect(callback).not.toHaveBeenCalled();
    emit({ kind: 'snapshot', snapshot: { epoch: 'epoch_a', revision: 0, tabs: [] } }); expect(browser.getSnapshot().tabs).toHaveLength(1);
    emit({ kind: 'snapshot', snapshot: { epoch: 'epoch_b', revision: 0, tabs: [] } });
    emit({ kind: 'snapshot', snapshot: { epoch: 'epoch_a', revision: 100, tabs: [] } }); expect(browser.getSnapshot().epoch).toBe('epoch_b'); browser.dispose();
  });
  it('balances overlapping hide holds and waits for the native ACK', async () => {
    const tabs = new SessionTabs(), { browser, platform } = host(tabs); await browser.start();
    const hidden = deferred(); vi.mocked(platform.present).mockImplementation(async value => { if (value.blocked) await hidden.promise; });
    const first = browser.acquireOverlay(), second = browser.acquireOverlay(), ready = vi.fn(); first.ready.then(ready);
    await Promise.resolve(); expect(ready).not.toHaveBeenCalled();
    hidden.resolve(); await first.ready; await second.ready;
    first.release(); first.release(); expect(vi.mocked(platform.present).mock.calls.at(-1)![0].blocked).toBe(true);
    second.release(); await Promise.resolve(); expect(vi.mocked(platform.present).mock.calls.at(-1)![0].blocked).toBe(false); browser.dispose();
  });
  it('publishes fractional CSS bounds at most four slots without applying DPR', async () => {
    const tabs = new SessionTabs(); for (let index = 0; index < 5; index++) tabs.openBrowser({ ...descriptor, id: `browser_${index}` });
    const { browser, platform } = host(tabs); await browser.start(); const elements = Array.from({ length: 5 }, slot);
    const stops = elements.map((element, index) => browser.register(`browser_${index}`, element));
    await waitFor(() => expect(vi.mocked(platform.present).mock.calls.at(-1)![0].slots).toHaveLength(4));
    expect(vi.mocked(platform.present).mock.calls.at(-1)![0].slots[0]!.bounds).toEqual({ x: 10.5, y: 90.25, width: 400.75, height: 300.5 });
    stops.forEach(stop => stop()); elements.forEach(element => element.remove()); browser.dispose();
  });
  it('restores native focus only to the same still-presented generation after the last hold', async () => {
    const tabs = new SessionTabs(); tabs.openBrowser(descriptor); const { browser, platform, emit } = host(tabs); await browser.start();
    const element = slot(), stop = browser.register(descriptor.id, element);
    emit({ kind: 'focused', ...browser.target(descriptor.id)! }); const hold = browser.acquireOverlay(); await hold.ready; hold.release();
    await waitFor(() => expect(platform.act).toHaveBeenCalledWith({ ...browser.target(descriptor.id), action: { kind: 'focus' } }));
    stop(); element.remove(); browser.dispose();
  });
  it('honors beforeunload cancellation and closes inert web metadata without native claims', async () => {
    const tabs = new SessionTabs(); tabs.openBrowser(descriptor); const { browser, platform } = host(tabs);
    vi.mocked(platform.close).mockResolvedValue({ status: 'cancelled' }); expect(await browser.mayClose(descriptor.id)).toBe(false);
    expect(tabs.workspace().tabs).toHaveLength(1); browser.dispose();
    const web = new BrowserWorkspace(undefined, tabs, vi.fn()); expect(await web.mayClose(descriptor.id)).toBe(true); web.dispose();
  });
});

describe('production native overlay gating', () => {
  it('gates actual default-open dialogs through StrictMode until hide ACK and releases on unmount', async () => {
    const hidden = deferred(), releases: ReturnType<typeof vi.fn>[] = [];
    const acquire = () => { const release = vi.fn(); releases.push(release); return { ready: hidden.promise, release }; };
    const view = render(<StrictMode><NativeSurfaceProvider acquire={acquire}><Dialog open onOpenChange={() => {}} title="Approval"><Button>Allow once</Button></Dialog></NativeSurfaceProvider></StrictMode>);
    expect(screen.queryByRole('button', { name: 'Allow once' })).toBeNull();
    await act(async () => hidden.resolve()); expect(await screen.findByRole('button', { name: 'Allow once' })).toBeTruthy();
    view.unmount(); expect(releases.length).toBeGreaterThan(0); for (const release of releases) expect(release).toHaveBeenCalledOnce();
  });
  it('holds through close transitions and releases only on completion, not a close request', async () => {
    const release = vi.fn(); const acquire = () => ({ ready: Promise.resolve(), release });
    function Probe() { const overlay = useNativeOverlay(); return <><button onClick={() => overlay.onOpenChange(true)}>Open</button><button onClick={() => overlay.onOpenChange(false)}>Close</button><button onClick={() => overlay.onOpenChangeComplete(false)}>Exit done</button>{overlay.open && <span>Visible</span>}</>; }
    render(<NativeSurfaceProvider acquire={acquire}><Probe/></NativeSurfaceProvider>); fireEvent.click(screen.getByText('Open')); await screen.findByText('Visible');
    fireEvent.click(screen.getByText('Close')); expect(release).not.toHaveBeenCalled(); fireEvent.click(screen.getByText('Exit done')); expect(release).toHaveBeenCalledOnce();
  });
  it('never mounts toast/drag-style portals before ACK and preserves synchronous web behavior', async () => {
    const hidden = deferred(), release = vi.fn(); const acquire = () => ({ ready: hidden.promise, release });
    function Portal({ present }: { present: boolean }) { const visible = useNativeSurfacePresence(present); return visible ? <button>Toast action</button> : null; }
    const view = render(<NativeSurfaceProvider acquire={acquire}><Portal present/></NativeSurfaceProvider>); expect(screen.queryByRole('button')).toBeNull();
    await act(async () => hidden.resolve()); expect(screen.getByRole('button')).toBeTruthy(); view.unmount(); expect(release).toHaveBeenCalledOnce();
    render(<Portal present/>); expect(screen.getByRole('button')).toBeTruthy();
  });
});


describe('production Browser shell integration without a daemon', () => {
  function application(native = true, design?: BrowserPlatform['design']) {
    vi.stubGlobal('matchMedia', () => ({ matches: false, addEventListener() {}, removeEventListener() {} }));
    vi.stubGlobal('ResizeObserver', class { observe() {} unobserve() {} disconnect() {} });
    const disk = storage(), bridge = host(new SessionTabs());
    if (design) Object.assign(bridge.platform, { design });
    if (!native) new SessionTabs(disk).openBrowser(descriptor);
    const app = createWhipApplication({ defaultEndpoint: 'http://127.0.0.1:8080', storage: disk, windowStorage: disk, browser: native ? bridge.platform : undefined,
      copy: vi.fn(async () => {}), openExternal: vi.fn(async () => {}), download: vi.fn(async () => {}) },
      createMemoryHistory({ initialEntries: [native ? '/' : `/browser/${descriptor.id}`] }));
    const view = render(<app.Application/>);
    return { ...app, ...bridge, cleanup() { view.unmount(); app.dispose(); bridge.browser.dispose(); vi.unstubAllGlobals(); } };
  }
  function designPlatform() {
    const gate = deferred();
    let held = false;
    const start = vi.fn(async (target: { epoch: string; tabId: string; generation: string }) => {
      if (held) await gate.promise;
      return { ...target, designId: 'design', documentRevision: 1, selectionRevision: 0, status: 'active' as const, viewport: { width: 1000, height: 650 }, elements: [] };
    });
    return { start, stop: vi.fn(async () => {}), update: vi.fn(async () => {}), capture: vi.fn(), onEvent: () => () => {}, hold() { held = true; }, release: gate.resolve };
  }
  it('toggles Design through the existing owner from browser/address focus, with exact nonrepeat modifiers and pending-start exclusion', async () => {
    const design = designPlatform(), app = application(true, design);
    const chord = { key: 'D', metaKey: true, shiftKey: true };
    try {
      fireEvent.click(await screen.findByRole('button', { name: 'New Browser tab' }));
      const address = await screen.findByRole('textbox', { name: 'Browser address' });
      await waitFor(() => expect(app.runtime.browser.canToggleDesign('browser_created')).toBe(true));
      for (const change of [{ metaKey: false }, { shiftKey: false }, { ctrlKey: true }, { altKey: true }, { repeat: true }, { isComposing: true }]) fireEvent.keyDown(address, { ...chord, ...change });
      expect(design.start).not.toHaveBeenCalled();
      fireEvent.keyDown(document.body, chord);
      expect(design.start).not.toHaveBeenCalled();
      design.hold();
      expect(fireEvent.keyDown(address, chord)).toBe(false);
      fireEvent.keyDown(address, chord);
      expect(design.start).toHaveBeenCalledTimes(1);
      await act(async () => design.release());
      await screen.findByRole('button', { name: 'Exit Design Mode' });
      fireEvent.keyDown(screen.getByRole('region', { name: /^Browser:/ }), chord);
      await screen.findByRole('button', { name: 'Enter Design Mode' });
      expect(design.stop).toHaveBeenCalledTimes(1);
      await waitFor(() => expect(app.platform.act).toHaveBeenLastCalledWith(expect.objectContaining({ tabId: 'browser_created', action: { kind: 'focus' } })));
    } finally { app.cleanup(); }
  });
  it('rejects inactive pane, native-surface modal holds and stale native shortcut identities', async () => {
    const design = designPlatform(), app = application(true, design);
    try {
      fireEvent.click(await screen.findByRole('button', { name: 'New Browser tab' }));
      const address = await screen.findByRole('textbox', { name: 'Browser address' });
      await waitFor(() => expect(app.runtime.browser.canToggleDesign('browser_created')).toBe(true));
      const event = { kind: 'shortcut' as const, shortcut: 'design-toggle' as const, ...app.runtime.browser.target('browser_created')! };
      await act(async () => { app.emit({ ...event, generation: 'stale' }); app.emit({ ...event, epoch: 'stale' }); });
      expect(design.start).not.toHaveBeenCalled();
      const hold = app.runtime.browser.acquireOverlay();
      await hold.ready;
      await act(async () => app.emit(event));
      expect(fireEvent.keyDown(address, { key: 'D', metaKey: true, shiftKey: true })).toBe(true);
      expect(design.start).not.toHaveBeenCalled();
      hold.release();
      await act(async () => { const other = app.runtime.tabs.openNew({}); app.runtime.tabs.split(other.id, 'right'); });
      await act(async () => app.emit(event));
      expect(app.runtime.browser.toggleDesign('browser_created')).toBe(false);
      expect(design.start).not.toHaveBeenCalled();
      await act(async () => app.runtime.tabs.activate('browser_created'));
      await waitFor(() => expect(app.runtime.browser.canToggleDesign('browser_created')).toBe(true));
      await act(async () => app.emit(event));
      await screen.findByRole('button', { name: 'Exit Design Mode' });
      expect(design.start).toHaveBeenCalledTimes(1);
      await act(async () => app.emit(event));
      await screen.findByRole('button', { name: 'Enter Design Mode' });
      expect(design.stop).toHaveBeenCalledTimes(1);
      await waitFor(() => expect(app.platform.act).toHaveBeenLastCalledWith(expect.objectContaining({ tabId: 'browser_created', action: { kind: 'focus' } })));
    } finally { app.cleanup(); }
  });
  it.each(['stop-pane', 'stop-generation', 'sync-pane', 'sync-generation'])('does not restore shortcut focus after delayed %s invalidation', async scenario => {
    const design = designPlatform(), app = application(true, design), gate = deferred(), began = deferred();
    const restoreFocus = vi.spyOn(app.runtime.browser, 'restoreDesignFocus');
    try {
      fireEvent.click(await screen.findByRole('button', { name: 'New Browser tab' }));
      await screen.findByRole('textbox', { name: 'Browser address' });
      await waitFor(() => expect(app.runtime.browser.canToggleDesign('browser_created')).toBe(true));
      const target = app.runtime.browser.target('browser_created')!;
      const event = { kind: 'shortcut' as const, shortcut: 'design-toggle' as const, ...target };
      await act(async () => app.emit(event));
      await screen.findByRole('button', { name: 'Exit Design Mode' });
      if (scenario.startsWith('stop')) design.stop.mockImplementationOnce(async () => { began.resolve(); await gate.promise; });
      else {
        const restore = app.platform.restore;
        vi.spyOn(app.platform, 'restore').mockImplementationOnce(async input => { began.resolve(); await gate.promise; return restore(input); });
        await act(async () => {
          app.runtime.tabs.openBrowser({ id: 'browser_sync', url: 'https://example.org/' });
          app.runtime.tabs.activate('browser_created');
        });
      }
      vi.mocked(app.platform.act).mockClear();
      await act(async () => app.emit(event));
      await began.promise;
      await act(async () => {
        if (scenario.endsWith('pane')) { const other = app.runtime.tabs.openNew({}); app.runtime.tabs.split(other.id, 'right'); }
        else {
          const inventory = app.runtime.browser.getSnapshot();
          app.emit({ kind: 'snapshot', snapshot: { ...inventory, revision: inventory.revision + 1, tabs: inventory.tabs.map(tab => tab.id === target.tabId ? { ...tab, generation: 'replacement' } : tab) } });
        }
        gate.resolve();
      });
      await waitFor(() => expect(restoreFocus).toHaveBeenCalledTimes(1));
      await act(async () => { await restoreFocus.mock.results[0]!.value; });
      await waitFor(() => expect(app.runtime.browser.canToggleDesign('browser_created') && app.runtime.browser.target('browser_created')?.generation === target.generation).toBe(false));
      expect(vi.mocked(app.platform.act).mock.calls.filter(([input]) => input.action.kind === 'focus')).toEqual([]);
    } finally { gate.resolve(); app.cleanup(); }
  });
  it('creates, routes and navigates a page without an execution host, preserving cancelled close', async () => {
    const app = application();
    try {
      fireEvent.click(await screen.findByRole('button', { name: 'New Browser tab' }));
      const address = await screen.findByRole('textbox', { name: 'Browser address' });
      await waitFor(() => expect(app.router.state.location.pathname).toBe('/browser/browser_created'));
      expect(app.runtime.tabs.workspace().tabs).toHaveLength(1);
      fireEvent.change(address, { target: { value: 'localhost:4567/test' } });
      fireEvent.submit(screen.getByRole('form', { name: 'Browser navigation' }));
      await waitFor(() => expect(app.platform.act).toHaveBeenCalledWith(expect.objectContaining({ action: { kind: 'navigate', url: 'http://localhost:4567/test' } })));
      vi.mocked(app.platform.close).mockResolvedValue({ status: 'cancelled' });
      act(() => app.emit({ kind: 'shortcut', shortcut: 'close', ...app.runtime.browser.target('browser_created')! }));
      await waitFor(() => expect(app.platform.close).toHaveBeenCalledOnce());
      expect(app.runtime.tabs.workspace().tabs).toHaveLength(1);
      vi.mocked(app.platform.close).mockResolvedValue({ status: 'closed' });
      act(() => app.emit({ kind: 'shortcut', shortcut: 'close', ...app.runtime.browser.target('browser_created')! }));
      await waitFor(() => expect(app.runtime.tabs.workspace().tabs).toHaveLength(0));
    } finally { app.cleanup(); }
  });
  it('preserves an unavailable web address and does not expose creation', async () => {
    const app = application(false);
    try {
      expect(await screen.findByText('Browser pages are available in the desktop app. This saved address is preserved.')).toBeTruthy();
      expect(screen.queryByRole('button', { name: 'New Browser tab' })).toBeNull();
      const address = screen.getByRole('textbox', { name: 'Browser address' }) as HTMLInputElement;
      expect(address.readOnly).toBe(true); expect(address.value).toBe(descriptor.url);
      fireEvent.click(screen.getByRole('button', { name: 'Copy address' }));
      expect(app.runtime.platform.copy).toHaveBeenCalledWith(descriptor.url);
      expect(app.runtime.tabs.workspace().tabs[0]).toMatchObject({ kind: 'browser', url: descriptor.url });
    } finally { app.cleanup(); }
  });
  it('waits for hide ACK before making settings interactive above a former native page', async () => {
    const app = application();
    try {
      fireEvent.click(await screen.findByRole('button', { name: 'New Browser tab' }));
      await screen.findByRole('textbox', { name: 'Browser address' });
      const hidden = deferred();
      vi.mocked(app.platform.present).mockImplementation(async value => { if (value.blocked) await hidden.promise; });
      await act(async () => { await app.router.navigate({ to: '/settings', search: { section: 'general' } }); });
      expect(screen.queryByRole('heading', { name: 'Keyboard shortcuts' })).toBeNull();
      await act(async () => hidden.resolve());
      expect(await screen.findByRole('heading', { name: 'Keyboard shortcuts' })).toBeTruthy();
      expect(app.runtime.tabs.workspace().tabs).toHaveLength(1);
    } finally { app.cleanup(); }
  });
});
