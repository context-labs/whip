import { describe, expect, it, vi } from 'vitest';
import type { AnyRouter } from '@tanstack/react-router';
import type { AppRuntime } from '../src/runtime';
import { bindSessionTabs, openNewChat } from '../src/session-tab-routing';
import { SessionTabs, selectedSessionTab } from '../src/session-tabs';
import { newSessionSearch } from '../src/sidebar-state';
import type { AppStorage } from '../src/platform';

function fixture(path = '/', key = 'initial', storage?: AppStorage) {
  const runtimeEvents = new Set<() => void>();
  const tabs = new SessionTabs(storage, () => runtimeEvents.forEach(fn => fn()));
  const connectionEvents = new Set<() => void>(), routeEvents = new Set<() => void>();
  let connection = { state: 'connecting', info: undefined as { runtime_id: string } | undefined };
  const client = { getSnapshot: () => connection, subscribe: (fn: () => void) => { connectionEvents.add(fn); return () => connectionEvents.delete(fn); } };
  const runtime = { tabs, connections: { home: () => ({ client }), host: (id: string) => connection.info?.runtime_id === id ? { client } : undefined }, rememberSession: vi.fn(), getSnapshot: () => ({ client, hosts: [], selectedHostId: undefined }), subscribe: (fn: () => void) => { runtimeEvents.add(fn); return () => runtimeEvents.delete(fn); }, report: vi.fn(() => runtimeEvents.forEach(fn => fn())) } as unknown as AppRuntime;
  const router = {
    state: { location: { pathname: path, href: path, search: {} as Record<string, unknown>, state: { __TSR_key: key, whipViewId: undefined as string | undefined } } },
    subscribe: (_kind: string, fn: () => void) => { routeEvents.add(fn); return () => routeEvents.delete(fn); },
    navigate: vi.fn(async () => {}),
  };
  return { tabs, runtime, router,
    start: () => bindSessionTabs(runtime, router as unknown as AnyRouter),
    connect(runtimeId = 'mac') { connection = { state: 'connected', info: { runtime_id: runtimeId } }; runtimeEvents.forEach(fn => fn()); },
    route(pathname: string, search: Record<string, unknown> = {}, whipViewId?: string) { router.state.location = { pathname, href: pathname, search, state: { __TSR_key: crypto.randomUUID(), whipViewId } }; routeEvents.forEach(fn => fn()); },
  };
}
describe('tab route authority', () => {
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
    expect(f.runtime.report).toHaveBeenCalledTimes(1); expect(f.tabs.workspace().tabs.some(tab => tab.rootId === 'extra')).toBe(false);
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
    expect(f.router.navigate).not.toHaveBeenCalled(); expect(f.runtime.report).toHaveBeenCalled(); dispose();
  });
});
