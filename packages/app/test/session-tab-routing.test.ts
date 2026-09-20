import { describe, expect, it, vi } from 'vitest';
import type { AnyRouter } from '@tanstack/react-router';
import type { AppRuntime } from '../src/runtime';
import { bindSessionTabs, openNewChat, openSessionView, openChatView, openChildChat, canSplitSessionPane } from '../src/session-tab-routing';
import { SessionTabs, selectedSessionTab, sessionViewPane } from '../src/session-tabs';
import { newSessionSearch } from '../src/sidebar-state';
import type { AppStorage } from '../src/platform';

function fixture(path = '/', key = 'initial', storage?: AppStorage) {
  const runtimeEvents = new Set<() => void>();
  const tabs = new SessionTabs(storage, () => runtimeEvents.forEach(fn => fn()));
  const connectionEvents = new Set<() => void>(), routeEvents = new Set<() => void>();
  let connection = { state: 'connecting', info: undefined as { runtime_id: string } | undefined };
  const client = { getSnapshot: () => connection, subscribe: (fn: () => void) => { connectionEvents.add(fn); return () => connectionEvents.delete(fn); } };
  const runtime = { tabs, connections: { home: () => ({ client }), host: (id: string) => connection.info?.runtime_id === id ? { client, state: connection.state } : undefined }, rememberSession: vi.fn(), getSnapshot: () => ({ client, hosts: [], selectedHostId: undefined }), subscribe: (fn: () => void) => { runtimeEvents.add(fn); return () => runtimeEvents.delete(fn); }, report: vi.fn(() => runtimeEvents.forEach(fn => fn())), reportWorkspace: vi.fn(() => runtimeEvents.forEach(fn => fn())), clearWorkspaceError: vi.fn() } as unknown as AppRuntime;
  const router = {
    state: { location: { pathname: path, href: path, search: {} as Record<string, unknown>, state: { __TSR_key: key, whipViewId: undefined as string | undefined } } },
    subscribe: (_kind: string, fn: () => void) => { routeEvents.add(fn); return () => routeEvents.delete(fn); },
    navigate: vi.fn(async () => {}),
  };
  return { tabs, runtime, router,
    start: () => bindSessionTabs(runtime, router as unknown as AnyRouter),
    connect(runtimeId = 'mac') { connection = { state: 'connected', info: { runtime_id: runtimeId } }; runtimeEvents.forEach(fn => fn()); },
    disconnect() { connection = { ...connection, state: 'disconnected' }; runtimeEvents.forEach(fn => fn()); },
    route(pathname: string, search: Record<string, unknown> = {}, whipViewId?: string) { router.state.location = { pathname, href: pathname, search, state: { __TSR_key: crypto.randomUUID(), whipViewId } }; routeEvents.forEach(fn => fn()); },
  };
}
describe('child chat routing and shared split geometry', () => {
  function frame(paneId: string, width = 641, height = 481, compact = false) {
    const layout = document.createElement('div');
    layout.dataset.workspaceCompact = String(compact);
    const element = document.createElement('div'); element.dataset.workspaceFrame = paneId;
    Object.defineProperties(element, { clientWidth: { value: width }, clientHeight: { value: height } });
    layout.append(element); document.body.append(layout);
    return () => layout.remove();
  }
  it.each([
    [640, 481, false, 'right', false], [641, 481, false, 'right', true],
    [641, 481, true, 'right', false], [641, 480, false, 'bottom', false],
    [641, 481, false, 'bottom', true],
  ] as const)('guards width %s height %s compact %s edge %s', (width, height, compact, edge, expected) => {
    const tabs = new SessionTabs(), paneId = tabs.workspace().focusedPaneId;
    const remove = frame(paneId, width, height, compact);
    expect(canSplitSessionPane(tabs.workspace(), paneId, edge)).toBe(expected);
    expect(canSplitSessionPane(tabs.workspace(), paneId, edge, true)).toBe(false);
    remove(); expect(canSplitSessionPane(tabs.workspace(), paneId, edge)).toBe(false);
  });
  it('routes the source host/root and target view identity, preserving source state through Back/Forward', () => {
    const f = fixture('/h/mac/s/root'); f.connect();
    f.tabs.visit('mac', 'root', { agent: 'parent', panel: 'context' });
    f.router.state.location.search = { agent: 'parent', panel: 'context' };
    const dispose = f.start();
    const source = f.tabs.workspace().tabs.find(tab => tab.id === 'root');
    const remove = frame(sessionViewPane(f.tabs.workspace(), 'root')!.id);
    const result = openChildChat(f.runtime, f.router.navigate, 'root', 'A');
    expect(result).toBeDefined();
    expect(f.router.navigate).toHaveBeenLastCalledWith({ to: '/h/$runtimeId/s/$rootId', params: { runtimeId: 'mac', rootId: 'root' }, search: { agent: 'A' }, state: { whipViewId: result!.tab.id } });
    f.route('/h/mac/s/root', { agent: 'A' }, result!.tab.id);
    f.route('/h/mac/s/root', { agent: 'parent', panel: 'context' }, 'root');
    f.route('/h/mac/s/root', { agent: 'A' }, result!.tab.id);
    expect(f.tabs.workspace().tabs.find(tab => tab.id === 'root')).toEqual(source);
    expect(f.tabs.workspace().tabs).toHaveLength(2);
    remove(); dispose();
  });
  it('offers explicit tab fallback on compact layouts without any hidden split', () => {
    const f = fixture(); f.connect(); f.tabs.visit('mac', 'root', {});
    const before = f.tabs.getSnapshot(), onUnavailable = vi.fn();
    const remove = frame(sessionViewPane(f.tabs.workspace(), 'root')!.id, 900, 900, true);
    expect(openChildChat(f.runtime, f.router.navigate, 'root', 'A', { onUnavailable })).toBeUndefined();
    expect(onUnavailable).toHaveBeenCalledWith(expect.stringContaining('Open in tab'));
    expect(f.tabs.getSnapshot()).toBe(before); expect(f.router.navigate).not.toHaveBeenCalled();
    const result = openChildChat(f.runtime, f.router.navigate, 'root', 'A', { openInTab: true, onUnavailable });
    expect(result!.tab.location).toEqual({ agent: 'A' }); expect(result!.companion).toBeUndefined();
    expect(f.tabs.workspace().layout.type).toBe('pane');
    expect(f.tabs.workspace().tabs.find(tab => tab.id === 'root')).toMatchObject({ location: {} });
    remove();
  });
  it('focuses the target workspace view after navigation without scrolling or stealing later focus', async () => {
    const frames: FrameRequestCallback[] = [];
    vi.spyOn(window, 'requestAnimationFrame').mockImplementation(callback => { frames.push(callback); return frames.length; });
    const f = fixture(); f.connect(); f.tabs.visit('mac', 'root', {});
    const result = openChildChat(f.runtime, f.router.navigate, 'root', 'A', { openInTab: true })!;
    const panel = document.createElement('div'); panel.tabIndex = -1; panel.dataset.workspaceView = result.tab.id;
    document.body.append(panel); const focus = vi.spyOn(panel, 'focus');
    await Promise.resolve(); frames.shift()!(0);
    expect(document.activeElement).toBe(panel); expect(focus).toHaveBeenCalledWith({ preventScroll: true });
    focus.mockClear();
    const other = openChildChat(f.runtime, f.router.navigate, 'root', 'B', { openInTab: true })!;
    panel.dataset.workspaceView = other.tab.id;
    await Promise.resolve(); f.tabs.activate('root'); frames.shift()!(0);
    expect(focus).not.toHaveBeenCalled();
    panel.remove();
  });
  it('can focus a previously created companion after compact resize without allocating a hidden split', () => {
    const f = fixture(); f.connect(); f.tabs.visit('mac', 'root', {});
    const paneId = sessionViewPane(f.tabs.workspace(), 'root')!.id;
    const remove = frame(paneId);
    const first = openChildChat(f.runtime, f.router.navigate, 'root', 'A')!; remove();
    const removeCompact = frame(paneId, 900, 900, true);
    const second = openChildChat(f.runtime, f.router.navigate, 'root', 'B', { companion: first.companion })!;
    expect(second.tab.id).toBe(first.tab.id); expect(f.tabs.workspace().tabs).toHaveLength(2);
    expect(selectedSessionTab(f.tabs.workspace())?.id).toBe(first.tab.id);
    removeCompact();
  });
  it('reports removed hosts without mutation and navigational failure without reallocating', async () => {
    const f = fixture(); f.tabs.visit('mac', 'root', {});
    const before = f.tabs.getSnapshot(), onUnavailable = vi.fn();
    openChildChat(f.runtime, f.router.navigate, 'root', 'A', { openInTab: true, onUnavailable });
    expect(onUnavailable).toHaveBeenCalledWith(expect.stringContaining('host'));
    expect(f.tabs.getSnapshot()).toBe(before);
    f.connect(); f.disconnect();
    const error = new Error('Navigation failed'); f.router.navigate.mockRejectedValueOnce(error);
    const result = openChildChat(f.runtime, f.router.navigate, 'root', 'A', { openInTab: true, onUnavailable });
    await Promise.resolve(); await Promise.resolve();
    expect(result).toBeDefined(); expect(f.tabs.workspace().tabs).toHaveLength(2);
    expect(f.router.navigate).toHaveBeenCalledOnce();
    expect(f.runtime.reportWorkspace).toHaveBeenCalledWith(error); expect(onUnavailable).toHaveBeenCalledOnce();
  });
});

describe('always-new chat routing', () => {
  it('routes identical session URLs with fresh view identities and restores Back/Forward selection', async () => {
    const f = fixture('/h/mac/s/root'); f.connect();
    f.tabs.visit('mac', 'root', {});
    const original = f.tabs.workspace().tabs[0]!;
    const dispose = f.start();
    const tab = openChatView(f.runtime, f.router.navigate, 'mac', 'root', 'Root');
    expect(tab!.id).not.toBe(original.id);
    expect(f.router.navigate).toHaveBeenLastCalledWith({ to: '/h/$runtimeId/s/$rootId', params: { runtimeId: 'mac', rootId: 'root' }, search: {}, state: { whipViewId: tab!.id } });
    f.route('/h/mac/s/root', {}, tab!.id);
    f.route('/h/mac/s/root', {}, original.id);
    expect(selectedSessionTab(f.tabs.workspace())?.id).toBe(original.id);
    f.route('/h/mac/s/root', {}, tab!.id);
    expect(selectedSessionTab(f.tabs.workspace())?.id).toBe(tab!.id);
    expect(f.tabs.workspace().tabs).toHaveLength(2);
    dispose();
  });

  it('opens a local view for a known disconnected host without session work', () => {
    const f = fixture(); f.connect(); f.disconnect();
    const tab = openChatView(f.runtime, f.router.navigate, 'mac', 'root');
    expect(tab).toMatchObject({ runtimeId: 'mac', rootId: 'root', kind: 'chat' });
    expect(selectedSessionTab(f.tabs.workspace())?.id).toBe(tab!.id);
    expect(f.router.navigate).toHaveBeenCalledExactlyOnceWith(expect.objectContaining({ state: { whipViewId: tab!.id } }));
    expect(f.runtime.reportWorkspace).not.toHaveBeenCalled();
  });

  it('reports removed-host, stale-pane and capacity errors without navigation or mutation', async () => {
    const f = fixture();
    const empty = f.tabs.getSnapshot();
    await openChatView(f.runtime, f.router.navigate, 'mac', 'root');
    expect(f.runtime.reportWorkspace).toHaveBeenLastCalledWith(expect.objectContaining({ message: expect.stringContaining('no longer available') }));
    expect(f.tabs.getSnapshot()).toBe(empty);
    f.connect();
    await openChatView(f.runtime, f.router.navigate, 'mac', 'root', '', { paneId: 'removed' });
    expect(f.tabs.getSnapshot()).toBe(empty);
    for (let i = 0; i < 32; i++) f.tabs.openChatView('mac', 'root');
    const full = f.tabs.getSnapshot();
    await openChatView(f.runtime, f.router.navigate, 'mac', 'root');
    expect(f.tabs.getSnapshot()).toBe(full);
    expect(f.runtime.reportWorkspace).toHaveBeenCalledTimes(3);
    expect(f.router.navigate).not.toHaveBeenCalled();
  });

  it('reports navigation failure without retrying the already-created view', async () => {
    const f = fixture(); f.connect();
    const failure = new Error('Navigation unavailable');
    f.router.navigate.mockRejectedValueOnce(failure);
    await openChatView(f.runtime, f.router.navigate, 'mac', 'root');
    expect(f.tabs.workspace().tabs).toHaveLength(1);
    expect(f.router.navigate).toHaveBeenCalledOnce();
    expect(f.runtime.reportWorkspace).toHaveBeenCalledExactlyOnceWith(failure);
  });
});

describe('tab route authority', () => {
  it('opens REPL with a new history identity and Back/Forward selects without converting the chat', async () => {
    const f = fixture('/h/mac/s/root'); f.tabs.visit('mac', 'root', { agent: 'child' });
    f.router.state.location.search = { agent: 'child' };
    const dispose = f.start();
    const repl = await openSessionView(f.runtime, f.router.navigate, 'root', 'repl');
    expect(repl).toBeDefined();
    expect(f.router.navigate).toHaveBeenLastCalledWith({ to: '/h/$runtimeId/s/$rootId', params: { runtimeId: 'mac', rootId: 'root' }, search: { agent: 'child', view: 'repl' }, state: { whipViewId: repl!.id } });
    f.route('/h/mac/s/root', { agent: 'child', view: 'repl' }, repl!.id);
    f.route('/h/mac/s/root', { agent: 'child' }, 'root');
    f.route('/h/mac/s/root', { agent: 'child', view: 'repl' }, repl!.id);
    expect(f.tabs.workspace().tabs.map(tab => tab.kind)).toEqual(['chat', 'repl']);
    f.tabs.closeViews([repl!.id]);
    f.route('/h/mac/s/root', { agent: 'child', view: 'repl' }, repl!.id);
    expect(f.tabs.workspace().tabs.map(tab => tab.kind)).toEqual(['chat', 'repl']);
    expect(selectedSessionTab(f.tabs.workspace())?.id).toBe(repl!.id);
    dispose();
  });
  it('reports stale-source and navigation failures without replacing the chat', async () => {
    const f = fixture(); f.tabs.visit('mac', 'root', {});
    await openSessionView(f.runtime, f.router.navigate, 'missing', 'repl');
    expect(f.router.navigate).not.toHaveBeenCalled();
    expect(f.runtime.reportWorkspace).toHaveBeenCalled();
    f.router.navigate.mockRejectedValueOnce(new Error('Navigation failed'));
    await openSessionView(f.runtime, f.router.navigate, 'root', 'repl');
    expect(f.tabs.workspace().tabs[0]).toMatchObject({ id: 'root', kind: 'chat' });
    expect(f.runtime.reportWorkspace).toHaveBeenLastCalledWith(new Error('Navigation failed'));
  });
  it('switches the source tab in place at capacity, preserving its pane, agent and inspector', async () => {
    const f = fixture('/h/mac/s/root');
    const location = { agent: 'child', panel: 'context' as const };
    f.tabs.visit('mac', 'root', location);
    f.tabs.openRelated('root', 'repl');
    f.tabs.openRelated('root', 'trace');
    f.tabs.split('root', 'right');
    while (f.tabs.workspace().tabs.length < 32) f.tabs.open('mac', `filler-${f.tabs.workspace().tabs.length}`);
    f.route('/h/mac/s/root', location, 'root');
    const dispose = f.start();
    const original = f.tabs.workspace();
    for (const kind of ['repl', 'trace', 'chat'] as const) {
      const before = f.tabs.workspace();
      const result = await openSessionView(f.runtime, f.router.navigate, 'root', kind, true);
      expect(result).toMatchObject({ id: 'root', kind, location });
      expect(f.tabs.workspace()).toEqual(before); // Commit only when the route resolves.
      const search = { ...location, ...(kind !== 'chat' ? { view: kind } : {}) };
      expect(f.router.navigate).toHaveBeenLastCalledWith(expect.objectContaining({ search, state: { whipViewId: 'root' } }));
      f.route('/h/mac/s/root', search, 'root');
      expect(selectedSessionTab(f.tabs.workspace())).toMatchObject({ id: 'root', kind, location });
      expect(f.tabs.workspace().focusedPaneId).toBe(original.focusedPaneId);
      expect(f.tabs.workspace().tabs.map(tab => tab.id)).toEqual(original.tabs.map(tab => tab.id));
      expect(f.tabs.workspace().tabs.filter(tab => tab.id !== 'root')).toEqual(original.tabs.filter(tab => tab.id !== 'root'));
    }
    // Back/Forward change the mode of the same tab, not the selected tab identity.
    f.route('/h/mac/s/root', { ...location, view: 'trace' }, 'root');
    expect(selectedSessionTab(f.tabs.workspace())).toMatchObject({ id: 'root', kind: 'trace' });
    f.route('/h/mac/s/root', location, 'root');
    expect(selectedSessionTab(f.tabs.workspace())).toMatchObject({ id: 'root', kind: 'chat' });
    expect(f.tabs.workspace().tabs).toHaveLength(32);
    expect(f.runtime.reportWorkspace).not.toHaveBeenCalled();
    dispose();
  });

  it('does not mutate tabs when an in-place navigation fails or its source is invalid', async () => {
    const f = fixture();
    f.tabs.visit('mac', 'root', { agent: 'child' });
    const draft = f.tabs.openNew();
    const before = f.tabs.workspace();
    await openSessionView(f.runtime, f.router.navigate, 'missing', 'repl', true);
    await openSessionView(f.runtime, f.router.navigate, draft.id, 'trace', true);
    expect(f.router.navigate).not.toHaveBeenCalled();
    f.router.navigate.mockRejectedValueOnce(new Error('Navigation failed'));
    await openSessionView(f.runtime, f.router.navigate, 'root', 'repl', true);
    expect(f.tabs.workspace()).toEqual(before);
    expect(f.runtime.reportWorkspace).toHaveBeenLastCalledWith(new Error('Navigation failed'));
  });

  it('restores bare-home immediately even before connection and preserves child/inspector', () => {
    const f = fixture(); f.tabs.visit('mac', 'root', { agent: 'child', panel: 'execution' }); const dispose = f.start();
    f.connect();
    expect(f.router.navigate).toHaveBeenCalledExactlyOnceWith({ to: '/h/$runtimeId/s/$rootId', params: { runtimeId: 'mac', rootId: 'root' }, search: { agent: 'child', panel: 'execution' }, state: { whipViewId: 'root' }, replace: true });
    f.route('/h/mac/s/root', { agent: 'child' }); f.route('/'); f.connect();
    expect(f.router.navigate).toHaveBeenCalledTimes(2); expect(f.tabs.workspace().restoreSelection).toBe(true); dispose();
  });
  it('does not restore a saved tab over an explicit directory creation URL', () => {
    const f = fixture();
    f.router.state.location.search = { cwd: '/repo', runtimeId: 'mac' };
    f.tabs.visit('mac', 'saved', {});
    const dispose = f.start(); f.connect();
    expect(f.router.navigate).toHaveBeenCalledOnce();
    expect(selectedSessionTab(f.tabs.workspace())).toMatchObject({ kind: 'new', runtimeId: 'mac', cwd: '/repo' });
    expect(f.tabs.workspace().tabs).toHaveLength(2);
    dispose();
  });
  it('deep links beat saved selection and browser back reopens a closed session', () => {
    const f = fixture('/h/mac/s/linked'); f.tabs.visit('mac', 'saved', {}); const dispose = f.start(); f.connect();
    expect(f.router.navigate).not.toHaveBeenCalled(); expect(selectedSessionTab(f.tabs.workspace())?.rootId).toBe('linked');
    f.route('/h/mac/s/saved'); f.tabs.close('mac', ['linked'], 'saved');
    f.route('/h/mac/s/linked'); expect(f.tabs.workspace().tabs.filter(tab => tab.rootId === 'linked')).toHaveLength(1); dispose();
  });
  it('does not immediately reopen the active tab during its close transition', () => {
    const f = fixture('/h/mac/s/a'); f.tabs.open('mac', 'a'); f.tabs.open('mac', 'b'); const dispose = f.start(); f.connect();
    const next = f.tabs.close('mac', ['a'], 'a');
    expect(next).toBe('b'); expect(f.tabs.workspace().tabs.map(tab => tab.rootId)).toEqual(['b']);
    f.route('/h/mac/s/b'); expect(f.tabs.workspace().closed[0]?.tab.rootId).toBe('a'); dispose();
  });
  it('does not re-admit a closing URL when denied storage synchronously reports through runtime', () => {
    let denied = false;
    const disk: AppStorage = { keys: () => [], getItem: () => null, removeItem: () => {}, setItem: () => { if (denied) throw Error('storage denied'); } };
    const f = fixture('/h/mac/s/a', 'initial', disk);
    f.tabs.open('mac', 'a'); f.tabs.open('mac', 'b'); const dispose = f.start(); f.connect(); denied = true;
    expect(f.tabs.close('mac', ['a'], 'a')).toBe('b');
    f.runtime.report('A pending command also completed');
    expect(f.tabs.workspace().tabs.map(tab => tab.rootId)).toEqual(['b']);
    expect(f.tabs.workspace().closed.map(item => item.tab.rootId)).toEqual(['a']);
    f.route('/h/mac/s/b'); f.route('/h/mac/s/a');
    expect(selectedSessionTab(f.tabs.workspace())?.rootId).toBe('a'); dispose();
  });
  it('admits an overflow deep link only after space is explicitly made', () => {
    const f = fixture('/h/mac/s/extra'); for (let i = 0; i < 32; i++) f.tabs.open('mac', `root${i}`); const dispose = f.start(); f.connect();
    expect(f.runtime.reportWorkspace).toHaveBeenCalledTimes(1); expect(f.tabs.workspace().tabs.some(tab => tab.rootId === 'extra')).toBe(false);
    f.tabs.close('mac', ['root0']); expect(f.tabs.workspace().tabs).toHaveLength(32); expect(selectedSessionTab(f.tabs.workspace())?.rootId).toBe('extra'); dispose();
  });
  it('does not replace a navigation made while initial connection was pending', () => {
    const f = fixture('/settings'); f.tabs.visit('mac', 'saved', {}); const dispose = f.start(); f.connect(); expect(f.router.navigate).not.toHaveBeenCalled(); dispose();
  });
});

it('history view IDs target the intended duplicate without overwriting sibling locations', () => {
  const f = fixture('/h/mac/s/root'); f.tabs.visit('mac', 'root', { agent: 'first' });
  const duplicate = f.tabs.split('root', 'right');
  const dispose = f.start(); f.connect();
  f.route('/h/mac/s/root', { agent: 'second', panel: 'execution' }, duplicate);
  f.route('/h/mac/s/root', { agent: 'first', panel: 'context' }, 'root');
  expect(f.tabs.workspace().tabs.find(tab => tab.id === duplicate)?.location).toEqual({ agent: 'second', panel: 'execution' });
  expect(f.tabs.workspace().tabs.find(tab => tab.id === 'root')?.location).toEqual({ agent: 'first', panel: 'context' });
  f.route('/h/mac/s/other', {}, duplicate);
  expect(f.tabs.workspace().tabs.find(tab => tab.id === duplicate)?.rootId).toBe('root');
  expect(selectedSessionTab(f.tabs.workspace())?.rootId).toBe('other');
  dispose();
});

it('restores REPL mode at startup with the saved view and agent', () => {
  const f = fixture();
  f.tabs.visit('mac', 'root', { view: 'repl', agent: 'child', panel: 'execution' });
  const dispose = f.start(); f.connect();
  expect(f.router.navigate).toHaveBeenCalledExactlyOnceWith({ to: '/h/$runtimeId/s/$rootId', params: { runtimeId: 'mac', rootId: 'root' }, search: { view: 'repl', agent: 'child', panel: 'execution' }, state: { whipViewId: 'root' }, replace: true });
  dispose();
});

it('browser history switches modes on the intended view without adding tabs or changing duplicates', () => {
  const f = fixture('/h/mac/s/root');
  f.tabs.visit('mac', 'root', {});
  const duplicate = f.tabs.split('root', 'right');
  const dispose = f.start(); f.connect();
  const chat = { agent: 'first' }, repl = { view: 'repl', agent: 'second' };
  f.route('/h/mac/s/root', chat, 'root');
  f.route('/h/mac/s/root', repl, duplicate);
  expect(selectedSessionTab(f.tabs.workspace())).toMatchObject({ id: duplicate, kind: 'repl' });
  // Back to the first view, then forward to its REPL sibling.
  f.route('/h/mac/s/root', chat, 'root');
  expect(selectedSessionTab(f.tabs.workspace())).toMatchObject({ id: 'root', kind: 'chat' });
  expect(f.tabs.workspace().tabs.find(tab => tab.id === duplicate)).toMatchObject({ kind: 'repl', location: { agent: 'second' } });
  f.route('/h/mac/s/root', repl, duplicate);
  // Mode changes also participate in history when the view ID stays the same.
  f.route('/h/mac/s/root', { agent: 'second' }, duplicate);
  expect(selectedSessionTab(f.tabs.workspace())?.kind).toBe('chat');
  f.route('/h/mac/s/root', repl, duplicate);
  expect(selectedSessionTab(f.tabs.workspace())?.kind).toBe('repl');
  expect(f.tabs.workspace().tabs).toHaveLength(2);
  dispose();
});

it('an explicit URL controls mode over saved preferences and malformed modes default to chat', () => {
  const f = fixture('/h/mac/s/root');
  f.tabs.visit('mac', 'root', { view: 'repl', agent: 'child' });
  const dispose = f.start(); f.connect();
  expect(selectedSessionTab(f.tabs.workspace())).toMatchObject({ kind: 'chat', location: {} });
  f.route('/h/mac/s/root', { view: 'repl' });
  expect(selectedSessionTab(f.tabs.workspace())?.kind).toBe('repl');
  f.route('/h/mac/s/root', { view: ['repl'], agent: '\n', panel: 'invalid' });
  expect(selectedSessionTab(f.tabs.workspace())).toMatchObject({ kind: 'chat', location: {} });
  f.route('/h/mac/s/direct-repl', { view: 'repl', agent: 'child' });
  expect(selectedSessionTab(f.tabs.workspace())).toMatchObject({ rootId: 'direct-repl', kind: 'repl', location: { agent: 'child' } });
  dispose();
});

it('restores a remote tab while Local is offline and routes back to a local tab independently', () => {
  const f = fixture();
  f.tabs.visit('mac', 'same', { agent: 'local' });
  const remote = f.tabs.visit('remote', 'same', { agent: 'remote', view: 'repl' });
  const dispose = f.start();
  expect(f.router.navigate).toHaveBeenCalledExactlyOnceWith(expect.objectContaining({ params: { runtimeId: 'remote', rootId: 'same' }, state: { whipViewId: remote } }));
  f.route('/h/mac/s/same', { agent: 'local' }, 'same');
  expect(selectedSessionTab(f.tabs.workspace())).toMatchObject({ runtimeId: 'mac', location: { agent: 'local' } });
  f.route('/h/remote/s/same', { agent: 'remote', view: 'repl' }, remote);
  f.connect();
  expect(selectedSessionTab(f.tabs.workspace())).toMatchObject({ runtimeId: 'remote', kind: 'repl' });
  expect(f.tabs.workspace().tabs).toHaveLength(2);
  dispose();
});

describe('New Chat route ownership', () => {
  it('creates a distinct draft for every explicit action and never remembers a daemon session', () => {
    const f = fixture(); const dispose = f.start();
    const first = openNewChat(f.runtime, f.router.navigate);
    const second = openNewChat(f.runtime, f.router.navigate, { cwd: '/repo' });
    expect(first?.id).not.toBe(second?.id);
    expect(f.tabs.workspace().tabs).toHaveLength(2);
    expect(f.router.navigate).toHaveBeenLastCalledWith(expect.objectContaining({ to: '/new/$draftId', params: { draftId: second?.id } }));
    expect(f.runtime.rememberSession).not.toHaveBeenCalled(); dispose();
  });
  it('restores a saved draft on home and observes the draft route idempotently', () => {
    const f = fixture(); const draft = f.tabs.openNew({ cwd: '/repo' }); const dispose = f.start();
    expect(f.router.navigate).toHaveBeenCalledExactlyOnceWith({ to: '/new/$draftId', params: { draftId: draft.id }, search: {}, state: { whipViewId: draft.id }, replace: true });
    f.route(`/new/${draft.id}`); f.connect(); f.connect(); f.route(`/new/${draft.id}`);
    expect(f.tabs.workspace().tabs).toHaveLength(1);
    expect(selectedSessionTab(f.tabs.workspace())?.id).toBe(draft.id);
    expect(f.runtime.rememberSession).not.toHaveBeenCalled(); dispose();
  });
  it('does not allocate for unknown drafts or revive a closing URL until explicit history navigation', () => {
    const f = fixture('/new/missing'); const dispose = f.start(); f.connect();
    expect(f.tabs.workspace().tabs).toHaveLength(0); expect(f.router.navigate).not.toHaveBeenCalled();
    const draft = f.tabs.openNew(); f.route(`/new/${draft.id}`);
    f.tabs.closeViews([draft.id], draft.id); f.connect();
    expect(f.tabs.workspace().tabs).toHaveLength(0);
    f.route('/'); expect(f.tabs.workspace().tabs).toHaveLength(0);
    f.route(`/new/${draft.id}`); expect(selectedSessionTab(f.tabs.workspace())?.id).toBe(draft.id);
    expect(f.tabs.workspace().tabs).toHaveLength(1); dispose();
  });
  it('replaces only a still-focused draft URL on promotion', () => {
    const f = fixture('/settings'); const first = f.tabs.openNew(), second = f.tabs.openNew(); const dispose = f.start();
    f.route(`/new/${first.id}`); f.tabs.promoteNew(first.id, 'mac', 'accepted');
    expect(f.router.navigate).toHaveBeenCalledExactlyOnceWith({ to: '/h/$runtimeId/s/$rootId', params: { runtimeId: 'mac', rootId: 'accepted' }, search: {}, state: { whipViewId: first.id }, replace: true });
    f.route('/h/mac/s/accepted', {}, first.id); f.router.navigate.mockClear();
    f.tabs.promoteNew(second.id, 'mac', 'background');
    expect(f.router.navigate).not.toHaveBeenCalled(); expect(selectedSessionTab(f.tabs.workspace())?.id).toBe(first.id);
    f.route(`/new/${second.id}`);
    expect(f.router.navigate).toHaveBeenCalledWith(expect.objectContaining({ params: { runtimeId: 'mac', rootId: 'background' }, replace: true })); dispose();
  });
  it('does not navigate for promotion from Settings or a closed draft', () => {
    const f = fixture('/settings'); const draft = f.tabs.openNew(); const dispose = f.start();
    f.tabs.promoteNew(draft.id, 'mac', 'accepted'); expect(f.router.navigate).not.toHaveBeenCalled();
    const closed = f.tabs.openNew(); f.route(`/new/${closed.id}`); f.tabs.closeViews([closed.id], closed.id);
    f.tabs.promoteNew(closed.id, 'mac', 'closed'); expect(f.router.navigate).not.toHaveBeenCalled(); dispose();
  });
  it.each([1, '1'])('creates exactly one draft for validated hostless new=%s intent', value => {
    const f = fixture();
    f.tabs.visit('mac', 'saved', {});
    f.router.state.location.search = newSessionSearch({ new: value });
    expect(f.router.state.location.search).toEqual({ new: 1 });
    const dispose = f.start(); f.connect(); f.connect();
    const selected = selectedSessionTab(f.tabs.workspace());
    expect(selected).toMatchObject({ kind: 'new' });
    expect(f.tabs.workspace().tabs).toHaveLength(2);
    expect(f.router.navigate).toHaveBeenCalledExactlyOnceWith({ to: '/new/$draftId', params: { draftId: selected?.id }, search: {}, state: { whipViewId: selected?.id }, replace: true });
    expect(f.runtime.rememberSession).not.toHaveBeenCalled();
    dispose();
  });
  it('consumes a legacy creation intent once despite runtime notifications and respects capacity', () => {
    const f = fixture(); f.router.state.location.search = { new: '1', runtimeId: 'mac' }; const dispose = f.start();
    f.connect(); f.connect(); expect(f.tabs.workspace().tabs).toHaveLength(1);
    for (let i = 1; i < 32; i++) f.tabs.openNew();
    const selected = selectedSessionTab(f.tabs.workspace()); f.router.navigate.mockClear();
    openNewChat(f.runtime, f.router.navigate); expect(selectedSessionTab(f.tabs.workspace())).toBe(selected);
    expect(f.router.navigate).not.toHaveBeenCalled(); expect(f.runtime.reportWorkspace).toHaveBeenCalled(); dispose();
  });
});

describe('terminal tab routes', () => {
  it('parses and builds terminal destinations without touching session URLs', async () => {
    const { tabDestination, terminalDestination, openTerminalTab } = await import('../src/session-tab-routing');
    expect(terminalDestination('/h/mac/t/term-1')).toEqual({ runtimeId: 'mac', terminalId: 'term-1' });
    expect(terminalDestination('/h/mac/s/root')).toBeUndefined();
    expect(terminalDestination('/h/mac/t/%ZZ')).toBeUndefined();
    const tabs = new SessionTabs();
    const id = tabs.openTerminal('mac', 'term-1', '/work');
    const tab = tabs.workspace().tabs.find(item => item.id === id)!;
    expect(tabDestination(tab)).toEqual({ to: '/h/$runtimeId/t/$terminalId', params: { runtimeId: 'mac', terminalId: 'term-1' }, search: {}, state: { whipViewId: id } });
    expect(typeof openTerminalTab).toBe('function');
  });
  it('selects an open terminal tab from its URL and shows the missing state for an unknown one', () => {
    const f = fixture('/h/mac/s/root');
    f.tabs.open('mac', 'root');
    const id = f.tabs.openTerminal('mac', 'term-1', '/work');
    f.tabs.visit('mac', 'root', {});
    expect(selectedSessionTab(f.tabs.workspace())?.id).not.toBe(id);
    const dispose = f.start();
    f.route('/h/mac/t/term-1');
    expect(selectedSessionTab(f.tabs.workspace())?.id).toBe(id);
    const before = f.tabs.workspace().tabs.length;
    f.route('/h/mac/t/term-unknown');
    expect(f.tabs.workspace().tabs).toHaveLength(before);
    expect(f.router.navigate).not.toHaveBeenCalled();
    dispose();
  });
  it('opens a shell on the host before adding a tab, and reports hosts without terminals', async () => {
    const { openTerminalTab } = await import('../src/session-tab-routing');
    const tabs = new SessionTabs();
    const open = vi.fn(async () => ({ id: 'term-7', shell: '/bin/zsh', cwd: '/resolved' }));
    const snapshot = { state: 'connected', info: { runtime_id: 'mac', capabilities: ['terminals'] } };
    const client = { getSnapshot: () => snapshot, terminals: { open } };
    const runtime = { tabs, connections: { host: (id: string) => id === 'mac' ? { client } : undefined }, reportWorkspace: vi.fn() } as unknown as AppRuntime;
    const navigate = vi.fn(async () => {});
    const id = await openTerminalTab(runtime, navigate as unknown as AnyRouter['navigate'], { runtimeId: 'mac', cwd: '/work', rootId: 'root' });
    expect(open).toHaveBeenCalledWith({ cwd: '/work', rootId: 'root', cols: 80, rows: 24 });
    expect(tabs.workspace().tabs.find(tab => tab.id === id)).toMatchObject({ kind: 'terminal', terminalId: 'term-7', cwd: '/resolved' });
    expect(navigate).toHaveBeenCalledWith(expect.objectContaining({ to: '/h/$runtimeId/t/$terminalId', params: { runtimeId: 'mac', terminalId: 'term-7' } }));
    snapshot.info.capabilities = [];
    expect(await openTerminalTab(runtime, navigate as unknown as AnyRouter['navigate'], { runtimeId: 'mac' })).toBeUndefined();
    expect((runtime.reportWorkspace as ReturnType<typeof vi.fn>).mock.calls.at(-1)?.[0]).toMatchObject({ message: expect.stringContaining('does not offer terminals') });
    expect(await openTerminalTab(runtime, navigate as unknown as AnyRouter['navigate'], { runtimeId: 'other' })).toBeUndefined();
    expect(open).toHaveBeenCalledTimes(1);
  });
});
