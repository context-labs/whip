import { describe, expect, it, vi } from 'vitest';
import { SessionTabs, TAB_STORAGE_KEY, PREVIOUS_TAB_STORAGE_KEY, LEGACY_TAB_STORAGE_KEY, sessionPanes, sessionViewPane, selectedSessionTab, sessionSearch, validateSessionSearch } from '../src/session-tabs';
import { sessionDestination } from '../src/session-tab-routing';
import type { AppStorage } from '../src/platform';

function storage(): AppStorage {
  const values = new Map<string, string>();
  return { keys: () => [...values.keys()], getItem: key => values.get(key) ?? null, setItem: (key, value) => { values.set(key, value); }, removeItem: key => { values.delete(key); } };
}
describe('window session tabs', () => {
  it('keeps a valid agent definition on new-chat tabs and rejects malformed ids', () => {
    const state = new SessionTabs();
    const tab = state.openNew({ definition: 'junior-developer' });
    expect(tab.definition).toBe('junior-developer');
    expect(state.updateNew(tab.id, { definition: 'support-triage' })).toBe(true);
    expect((state.workspace().tabs.find(item => item.id === tab.id) as { definition?: string }).definition).toBe('support-triage');
    expect(() => state.updateNew(tab.id, { definition: 'Not Valid' })).toThrow('Invalid New Chat options');
    expect(state.openNew({}).definition).toBeUndefined();
  });
  it('deduplicates roots while preserving separate hosts and selected child context', () => {
    const state = new SessionTabs();
    state.open('mac', 'root', 'Review'); state.visit('mac', 'root', { agent: 'child', panel: 'execution' }); state.open('mac', 'root', 'ignored');
    state.open('server', 'root', 'Review');
    expect(state.workspace().tabs).toHaveLength(2);
    expect(state.workspace().tabs[0]?.location).toEqual({ agent: 'child', panel: 'execution' });
    expect(state.workspace().tabs.map(tab => tab.runtimeId)).toEqual(['mac', 'server']);
    expect(new Set(state.workspace().tabs.map(tab => tab.id)).size).toBe(2);
    const snapshot = state.getSnapshot(); state.visit('mac', 'root', { agent: 'child', panel: 'execution' });
    expect(state.getSnapshot()).toBe(snapshot);
  });
  it('closes active tabs to the right then left, preserving background selection and reopen order', () => {
    const state = new SessionTabs();
    ['a', 'b', 'c'].forEach(id => state.open('mac', id));
    state.visit('mac', 'b', { panel: 'context' });
    expect(state.close('mac', ['a'], 'b')).toBeUndefined();
    expect(selectedSessionTab(state.workspace())?.rootId).toBe('b');
    expect(state.reopenView()).toBe('a');
    expect(state.workspace().tabs.map(x => x.rootId)).toEqual(['a', 'b', 'c']);
    expect(state.close('mac', ['b'], 'b')).toBe('c');
    expect(state.close('mac', ['c'], 'c')).toBe('a');
    expect(state.close('mac', ['a'], 'a')).toBeNull();
    state.reopenView(); state.reopenView(); state.reopenView();
    expect(state.workspace().tabs.find(x => x.rootId === 'b')?.location.panel).toBe('context');
  });
  it('closes a group atomically and does not replace the active background session', () => {
    const state = new SessionTabs(), update = vi.fn();
    ['a', 'b', 'c', 'd'].forEach(id => state.open('mac', id)); state.visit('mac', 'c', {});
    const off = state.subscribe(update);
    expect(state.close('mac', ['a', 'b', 'd'], 'c')).toBeUndefined();
    expect(update).toHaveBeenCalledTimes(1);
    expect(state.workspace().tabs.map(x => x.rootId)).toEqual(['c']); off();
  });
  it('purges every view and closed entry for a deleted root while preserving other hosts and focus', () => {
    const disk = storage(), state = new SessionTabs(disk);
    state.visit('mac', 'root', { agent: 'child' });
    const duplicate = state.split('root', 'right');
    state.closeViews([duplicate]);
    const remote = state.open('remote', 'root');
    state.visit('remote', 'root', {}, remote);
    const surviving = selectedSessionTab(state.workspace());
    const update = vi.fn(); state.subscribe(update);
    state.purge('mac', 'root');
    expect(update).toHaveBeenCalledTimes(1);
    expect(state.workspace().tabs).toEqual([surviving]);
    expect(selectedSessionTab(state.workspace())).toEqual(surviving);
    expect(state.workspace().closed).toEqual([]);
    expect(state.reopenView()).toBeUndefined();
    expect(new SessionTabs(disk).workspace()).toEqual(state.workspace());
    state.purge('remote', 'root');
    expect(state.workspace().tabs).toEqual([]);
    expect(state.workspace().restoreSelection).toBe(false);
  });
  it('purges closed-only and recoverable legacy roots without discarding unrelated layouts', () => {
    const disk = storage();
    disk.setItem(LEGACY_TAB_STORAGE_KEY, JSON.stringify({ version: 1, workspaces: [
      { runtimeId: 'mac', tabs: [{ rootId: 'root' }, { rootId: 'keep' }], closed: [{ tab: { rootId: 'root' } }] },
      { runtimeId: 'remote', tabs: [{ rootId: 'root' }] },
    ] }));
    const state = new SessionTabs(disk, undefined, 'remote');
    state.purge('mac', 'root');
    expect(state.getSnapshot().previous[0]?.workspace.tabs.map(tab => tab.rootId)).toEqual(['keep']);
    const restored = new SessionTabs(disk);
    expect(restored.getSnapshot().previous[0]?.workspace.tabs.map(tab => tab.rootId)).toEqual(['keep']);
    expect(restored.getSnapshot().previous[0]?.workspace.closed).toEqual([]);
    state.close('remote', ['root']);
    state.purge('remote', 'root');
    expect(state.reopenView()).toBeUndefined();
  });
  it('bounds open tabs without silently evicting work and bounds closed history', () => {
    const state = new SessionTabs();
    for (let i = 0; i < 32; i++) state.open('mac', `root-${i}`);
    expect(() => state.open('mac', 'extra')).toThrow('32 open');
    expect(state.workspace().tabs[0]?.rootId).toBe('root-0');
    expect(() => state.open('mac', 'root-0')).not.toThrow();
    state.close('mac', state.workspace().tabs.map(x => x.rootId));
    expect(state.workspace().closed).toHaveLength(20);
  });
  it('accepts exact reorder permutations only', () => {
    const state = new SessionTabs(); ['a', 'b', 'c'].forEach(id => state.open('mac', id));
    state.move('c', -1); expect(state.workspace().tabs.map(x => x.rootId)).toEqual(['a', 'c', 'b']);
    expect(() => state.reorderPane('main', ['a', 'c', 'c'])).toThrow();
    expect(() => state.reorderPane('main', ['a', 'c', 'unknown'])).toThrow();
    state.reorderPane('main', ['b', 'c', 'a']); expect(state.workspace().tabs.map(x => x.rootId)).toEqual(['b', 'c', 'a']);
  });
  it('restores a window independently, preserving valid entries from damaged records', () => {
    const disk = storage(); disk.setItem(TAB_STORAGE_KEY, JSON.stringify({ version: 1, workspaces: [{ runtimeId: 'mac', tabs: [null, { rootId: 'a', titleHint: 'A', location: { panel: 'fake', agent: 'child', secret: 'not copied' } }, { rootId: 'a' }, { rootId: 'b' }], lastActiveRootId: 'b' }] }));
    const state = new SessionTabs(disk), independent = new SessionTabs(disk);
    expect(state.workspace().tabs.map(x => x.rootId)).toEqual(['a', 'b']);
    expect(state.workspace().tabs[0]?.location).toEqual({ agent: 'child' });
    state.close('mac', ['a']); expect(independent.workspace().tabs).toHaveLength(2);
    const restored = new SessionTabs(disk); expect(restored.workspace().tabs).toHaveLength(1);
    expect(selectedSessionTab(restored.workspace())?.rootId).toBe('b');
    restored.home(); expect(new SessionTabs(disk).workspace().restoreSelection).toBe(false);
  });
  it('bounds bytes and host layouts and retains no prompt or operation payloads', () => {
    const disk = storage(), state = new SessionTabs(disk);
    for (let i = 0; i < 32; i++) state.open(`host-${i % 6}`, `root-${i}`, '界'.repeat(1000));
    expect(() => state.open('another-host', 'overflow')).toThrow('32 open');
    const saved = disk.getItem(TAB_STORAGE_KEY)!;
    expect(new TextEncoder().encode(saved).byteLength).toBeLessThanOrEqual(64 * 1024);
    expect(state.getSnapshot().version).toBe(3);
    expect(state.workspace().tabs).toHaveLength(32);
    expect(Object.keys(state.workspace().tabs[0]!)).toEqual(['id', 'kind', 'runtimeId', 'rootId', 'titleHint', 'location']);
  });
  it('continues in memory after denied storage and rejects invalid identities', () => {
    const disk = storage(), notice = vi.fn(); disk.setItem = () => { throw Error('denied'); };
    const state = new SessionTabs(disk, notice); state.open('mac', 'root');
    expect(state.workspace().tabs).toHaveLength(1); expect(notice).toHaveBeenCalled();
    expect(() => state.open('', 'root')).toThrow(); expect(() => state.open('mac', '\n')).toThrow();
  });
  it('parses canonical routes without interpreting other destinations as sessions', () => {
    expect(sessionDestination('/h/mac/s/root%3Achild')).toEqual({ runtimeId: 'mac', rootId: 'root:child' });
    expect(sessionDestination('/settings')).toBeUndefined(); expect(sessionDestination('/h/mac/s/%ZZ')).toBeUndefined();
  });
});

describe('split workspace views', () => {
  it('duplicates a view with independent locations and builds nested directional splits', () => {
    const state = new SessionTabs();
    state.visit('mac', 'root', { agent: 'first', panel: 'context' });
    const right = state.split('root', 'right');
    const bottom = state.split(right, 'bottom');
    state.updateLocation(right, { agent: 'second', panel: 'execution' });
    const workspace = state.workspace();
    expect(workspace.layout).toMatchObject({ type: 'split', direction: 'horizontal', first: { type: 'pane' }, second: { type: 'split', direction: 'vertical' } });
    expect(workspace.tabs.map(tab => tab.rootId)).toEqual(['root', 'root', 'root']);
    expect(new Set(workspace.tabs.map(tab => tab.id)).size).toBe(3);
    expect(workspace.tabs.find(tab => tab.id === right)?.location).toEqual({ agent: 'second', panel: 'execution' });
    expect(workspace.tabs.find(tab => tab.id === bottom)?.location).toEqual({ agent: 'first', panel: 'context' });
    expect(state.preferred('mac', 'root')?.id).toBe(bottom);
    state.visit('mac', 'root', { agent: 'third' }, right);
    expect(selectedSessionTab(state.workspace())?.id).toBe(right);
    expect(state.workspace().tabs.find(tab => tab.id === 'root')?.location.agent).toBe('first');
  });
  it('transfers view identity, prunes empty branches and reopens into a surviving pane', () => {
    const state = new SessionTabs(); state.visit('mac', 'root', { agent: 'child' });
    const duplicate = state.split('root', 'right');
    const third = state.split(duplicate, 'bottom');
    const target = sessionViewPane(state.workspace(), 'root')!.id;
    state.transfer(duplicate, target, undefined, 0);
    expect(sessionPanes(state.workspace().layout)).toHaveLength(2);
    expect(sessionViewPane(state.workspace(), duplicate)?.tabs.map(tab => tab.id)).toEqual([duplicate, 'root']);
    expect(state.workspace().closed).toEqual([]);
    state.activate(third);
    expect(state.closeViews([third], third)).toBe(duplicate);
    expect(state.workspace().layout.type).toBe('pane');
    expect(state.reopenView()).toBe(third);
    expect(sessionPanes(state.workspace().layout)).toHaveLength(1);
    expect(state.workspace().tabs.find(tab => tab.id === third)?.location).toEqual({ agent: 'child' });
  });
  it('enforces pane and view limits atomically while allowing transfers at capacity', () => {
    const state = new SessionTabs(); state.open('mac', 'root');
    const copies = Array.from({ length: 3 }, () => state.split('root', 'right'));
    const fullPanes = state.getSnapshot();
    expect(() => state.split('root', 'top')).toThrow('four panes');
    expect(state.getSnapshot()).toBe(fullPanes);
    const target = sessionViewPane(state.workspace(), 'root')!.id;
    state.transfer(copies[0]!, target, 'bottom');
    expect(sessionPanes(state.workspace().layout)).toHaveLength(4);
    for (let i = 0; i < 28; i++) state.open('mac', `extra-${i}`);
    const full = state.getSnapshot();
    expect(() => state.open('mac', 'overflow')).toThrow('32 open');
    expect(state.getSnapshot()).toBe(full);
    state.closeViews([copies[1]!]);
    state.open('mac', 'replacement');
    expect(() => state.split('root', 'left')).toThrow('32 open');
    expect(state.workspace().tabs).toHaveLength(32);
  });
  it('migrates legacy layouts then restores v3 duplicate views, focus and split ratios', () => {
    const disk = storage();
    disk.setItem(LEGACY_TAB_STORAGE_KEY, JSON.stringify({ version: 1, workspaces: [{ runtimeId: 'mac', tabs: [{ rootId: 'root', location: { agent: 'child' } }], lastActiveRootId: 'root' }] }));
    const state = new SessionTabs(disk);
    expect(state.workspace().tabs[0]).toMatchObject({ id: 'root', kind: 'chat', rootId: 'root' });
    const duplicate = state.split('root', 'left');
    state.updateLocation(duplicate, { panel: 'execution' });
    state.resize(state.workspace().layout.id, .35);
    const restored = new SessionTabs(disk);
    expect(restored.workspace()).toEqual(state.workspace());
    expect(selectedSessionTab(restored.workspace())?.id).toBe(duplicate);
    expect(JSON.parse(disk.getItem(TAB_STORAGE_KEY)!).version).toBe(3);
  });
  it('rejects malformed split trees and sanitizes duplicate view identities on restore', () => {
    const disk = storage();
    const tab = { id: 'view', kind: 'chat', rootId: 'root', location: { panel: 'invalid', agent: 'child' } };
    const pane = { type: 'pane', id: 'main', tabs: [tab, tab, { ...tab, id: 'other', kind: 'terminal' }], selected: 'missing' };
    const load = (layout: unknown) => { disk.setItem(TAB_STORAGE_KEY, JSON.stringify({ version: 2, workspaces: [{ runtimeId: 'mac', layout, focusedPaneId: 'missing', closed: [] }] })); return new SessionTabs(disk); };
    const valid = load(pane).workspace();
    expect(valid.tabs).toHaveLength(1); expect(valid.focusedPaneId).toBe('main');
    expect(selectedSessionTab(valid)?.location).toEqual({ agent: 'child' });
    for (const ratio of [0, 1, -1, null]) expect(load({ type: 'split', id: 'split', direction: 'horizontal', ratio, first: pane, second: { ...pane, id: 'second' } }).getSnapshot().workspace.tabs).toEqual([]);
    expect(load({ type: 'split', id: 'split', direction: 'horizontal', ratio: .5, first: pane, second: pane }).getSnapshot().workspace.tabs).toEqual([]);
  });
});

it('sanitizes overfull restored panes and rejects trees beyond the four-pane bound', () => {
  const disk = storage();
  const pane = (id: string) => ({ type: 'pane', id, tabs: Array.from({ length: 40 }, (_, index) => ({ id: `${id}-${index}`, kind: 'chat', rootId: `root-${index}`, location: {} })) });
  const split = (id: string, first: unknown, second: unknown) => ({ type: 'split', id, direction: 'horizontal', ratio: .5, first, second });
  const load = (layout: unknown) => { disk.setItem(TAB_STORAGE_KEY, JSON.stringify({ version: 2, workspaces: [{ runtimeId: 'mac', layout, closed: [] }] })); return new SessionTabs(disk); };
  expect(load(pane('one')).workspace().tabs).toHaveLength(32);
  const five = split('top', split('left', pane('a'), pane('b')), split('right', pane('c'), split('nested', pane('d'), pane('e'))));
  expect(load(five).getSnapshot().workspace.tabs).toEqual([]);
});

it('moves a view within its pane using original drop positions without duplicating it', () => {
  const state = new SessionTabs(); ['a', 'b', 'c', 'd'].forEach(id => state.open('mac', id));
  const pane = state.workspace().focusedPaneId;
  state.transfer('a', pane, undefined, 3);
  expect(state.workspace().tabs.map(tab => tab.id)).toEqual(['b', 'c', 'a', 'd']);
  state.transfer('d', pane, undefined, 0);
  expect(state.workspace().tabs.map(tab => tab.id)).toEqual(['d', 'b', 'c', 'a']);
  expect(selectedSessionTab(state.workspace())?.id).toBe('d');
});

it('reuses the selected duplicate when multiple views of one root share a pane', () => {
  const tabs = new SessionTabs();
  tabs.visit('mac', 'root', { agent: 'first' });
  const duplicate = tabs.split('root', 'right');
  tabs.visit('mac', 'root', { agent: 'second' }, duplicate);
  tabs.transfer(duplicate, 'main');
  expect(tabs.preferred('mac', 'root')?.id).toBe(duplicate);
  expect(tabs.open('mac', 'root')).toBe(duplicate);
  tabs.visit('mac', 'root', { agent: 'second', panel: 'execution' });
  expect(tabs.workspace().tabs.find(tab => tab.id === 'root')?.location).toEqual({ agent: 'first' });
  expect(tabs.workspace().tabs.find(tab => tab.id === duplicate)?.location).toEqual({ agent: 'second', panel: 'execution' });
});

describe('session presentation mode', () => {
  it('switches only the targeted view in place and keeps mode out of its location', () => {
    const tabs = new SessionTabs();
    tabs.visit('mac', 'root', { agent: 'first', panel: 'context' });
    const duplicate = tabs.split('root', 'right');
    const paneId = sessionViewPane(tabs.workspace(), duplicate)!.id;
    tabs.visit('mac', 'root', { view: 'repl', agent: 'second', panel: 'execution' }, duplicate);
    expect(tabs.workspace().tabs).toHaveLength(2);
    expect(sessionViewPane(tabs.workspace(), duplicate)?.id).toBe(paneId);
    expect(tabs.workspace().tabs.find(tab => tab.id === 'root')).toMatchObject({ kind: 'chat', location: { agent: 'first', panel: 'context' } });
    expect(tabs.preferred('mac', 'root')).toMatchObject({ id: duplicate, kind: 'repl', location: { agent: 'second', panel: 'execution' } });
    expect(sessionSearch(tabs.preferred('mac', 'root'))).toEqual({ view: 'repl', agent: 'second', panel: 'execution' });
    tabs.updateLocation(duplicate, { agent: 'third' });
    expect(sessionSearch(tabs.preferred('mac', 'root'))).toEqual({ view: 'repl', agent: 'third' });
    tabs.visit('mac', 'root', { agent: 'third' }, duplicate);
    expect(tabs.preferred('mac', 'root')?.kind).toBe('chat');
    expect(sessionSearch(tabs.preferred('mac', 'root'))).toEqual({ agent: 'third' });
  });

  it('copies and retains REPL mode through splitting, transfer, close, reopen and reload', () => {
    const disk = storage(), tabs = new SessionTabs(disk);
    tabs.visit('mac', 'root', { view: 'repl', agent: 'child' });
    const duplicate = tabs.split('root', 'right');
    tabs.transfer(duplicate, 'main');
    expect(tabs.workspace().tabs.map(tab => tab.kind)).toEqual(['repl', 'repl']);
    tabs.closeViews([duplicate], duplicate);
    const restored = new SessionTabs(disk);
    expect(restored.workspace().closed[0]?.tab).toMatchObject({ id: duplicate, kind: 'repl', location: { agent: 'child' } });
    expect(restored.reopenView()).toBe(duplicate);
    expect(restored.workspace().tabs.find(tab => tab.id === duplicate)?.kind).toBe('repl');
    expect(new SessionTabs(disk).workspace()).toEqual(restored.workspace());
    expect(restored.workspace().tabs.map(tab => tab.runtimeId)).toEqual(['mac', 'mac']);
  });

  it('validates saved modes while migrating chat descriptors from legacy storage', () => {
    const disk = storage();
    const tabs = [{ rootId: 'old' }, { rootId: 'chat', kind: 'chat' }, { rootId: 'repl', kind: 'repl' }, { rootId: 'unknown', kind: 'terminal' }, { rootId: 'null', kind: null }];
    disk.setItem(LEGACY_TAB_STORAGE_KEY, JSON.stringify({ version: 1, workspaces: [{ runtimeId: 'mac', tabs }] }));
    expect(new SessionTabs(disk).workspace().tabs.map(tab => [tab.rootId, tab.kind])).toEqual([['old', 'chat'], ['chat', 'chat'], ['repl', 'repl']]);
    disk.setItem(TAB_STORAGE_KEY, JSON.stringify({ version: 2, workspaces: [{ runtimeId: 'mac', layout: { type: 'pane', id: 'main', tabs: tabs.map(tab => ({ ...tab, id: tab.rootId, location: { view: 'repl' } })) }, closed: [] }] }));
    const restored = new SessionTabs(disk).workspace();
    expect(restored.tabs.map(tab => [tab.rootId, tab.kind])).toEqual([['chat', 'chat'], ['repl', 'repl']]);
    expect(restored.tabs.map(tab => tab.location)).toEqual([{}, {}]);
  });

  it('keeps mode changes in memory when reads and writes to storage fail', () => {
    const notice = vi.fn(), disk = storage();
    disk.getItem = () => { throw Error('denied'); };
    disk.setItem = () => { throw Error('denied'); };
    const tabs = new SessionTabs(disk, notice);
    tabs.visit('mac', 'root', { view: 'repl', agent: 'child' });
    expect(sessionSearch(tabs.preferred('mac', 'root'))).toEqual({ view: 'repl', agent: 'child' });
    expect(notice).toHaveBeenCalled();
    tabs.visit('mac', 'root', {});
    expect(tabs.preferred('mac', 'root')?.kind).toBe('chat');
  });

  it('accepts only supported route modes and bounded agent/inspector locations', () => {
    expect(validateSessionSearch({ view: 'repl', agent: 'child', panel: 'execution', code: 'discard' })).toEqual({ view: 'repl', agent: 'child', panel: 'execution' });
    for (const view of [undefined, 'chat', 'terminal', ['repl'], true, 1, null]) expect(validateSessionSearch({ view })).toEqual({});
    for (const agent of ['', '\nchild', 'a'.repeat(257), ['child']]) expect(validateSessionSearch({ agent, panel: 'unknown', view: 'repl' })).toEqual({ view: 'repl' });
    expect(sessionSearch()).toEqual({});
  });
});

it('keeps same-ID roots, titles, close actions and history isolated across hosts', () => {
  const disk = storage(), tabs = new SessionTabs(disk);
  const local = tabs.visit('local', 'root', { agent: 'local-child' });
  const remote = tabs.visit('remote', 'root', { view: 'repl', agent: 'remote-child' });
  expect(remote).not.toBe(local);
  tabs.titles('remote', new Map([['root', 'Remote title']]));
  expect(tabs.preferred('local', 'root')?.titleHint).toBe('');
  const copy = tabs.split(remote, 'right');
  expect(tabs.workspace().tabs.find(tab => tab.id === copy)?.runtimeId).toBe('remote');
  tabs.close('local', ['root']);
  expect(tabs.workspace().tabs.map(tab => tab.runtimeId)).toEqual(['remote', 'remote']);
  expect(tabs.reopenView()).toBe(local);
  expect(new SessionTabs(disk).workspace()).toEqual(tabs.workspace());
  tabs.close('remote', ['root']);
  expect(tabs.workspace().tabs.map(tab => tab.runtimeId)).toEqual(['local']);
});

it('migrates the last host layout and retains overflow layouts until explicit restoration', () => {
  const disk = storage();
  const old = JSON.stringify({ version: 1, workspaces: ['local', 'remote'].map(runtimeId => ({ runtimeId, lastActiveRootId: 'root-0', tabs: Array.from({ length: 20 }, (_, i) => ({ rootId: `root-${i}`, titleHint: `${runtimeId} ${i}`, location: { agent: 'child', panel: 'execution' } })) })) });
  disk.setItem(LEGACY_TAB_STORAGE_KEY, old);
  const tabs = new SessionTabs(disk, undefined, 'local');
  expect(selectedSessionTab(tabs.workspace())?.runtimeId).toBe('local');
  expect(tabs.getSnapshot().previous.map(item => item.runtimeId)).toEqual(['remote']);
  const before = tabs.getSnapshot();
  expect(() => tabs.restorePrevious('remote')).toThrow('32 open');
  expect(tabs.getSnapshot()).toBe(before);
  expect(new SessionTabs(disk).getSnapshot().previous).toHaveLength(1);
  tabs.close('local', Array.from({ length: 10 }, (_, i) => `root-${i}`));
  tabs.restorePrevious('remote');
  expect(tabs.workspace().tabs).toHaveLength(30);
  expect(new Set(tabs.workspace().tabs.map(tab => tab.id)).size).toBe(30);
  expect(sessionPanes(tabs.workspace().layout)).toHaveLength(2);
  expect(tabs.preferred('remote', 'root-0')?.location).toEqual({ agent: 'child', panel: 'execution' });
  expect(new SessionTabs(disk).getSnapshot().previous).toEqual([]);
  expect(disk.getItem(LEGACY_TAB_STORAGE_KEY)).toBe(old);
});

it('keeps unmigrated layouts recoverable when storage writes fail', () => {
  const disk = storage(), notice = vi.fn();
  disk.setItem(PREVIOUS_TAB_STORAGE_KEY, JSON.stringify({ version: 2, workspaces: ['a', 'b'].map(runtimeId => ({ runtimeId, layout: { type: 'pane', id: 'main', selected: 'root', tabs: [{ id: 'root', kind: 'chat', rootId: 'root' }] }, closed: [] })) }));
  disk.setItem = () => { throw new Error('denied'); };
  const tabs = new SessionTabs(disk, notice);
  tabs.restorePrevious('a');
  expect(tabs.workspace().tabs).toHaveLength(2);
  expect(notice).toHaveBeenCalledWith(expect.stringContaining('memory'));
  const reloaded = new SessionTabs(disk, notice);
  expect(reloaded.getSnapshot().previous.map(item => item.runtimeId)).toEqual(['a']);
  reloaded.dismissPrevious('a');
  expect(reloaded.getSnapshot().previous).toEqual([]);
  expect(reloaded.workspace().tabs).toHaveLength(1);
});

it('keeps oversized legacy metadata recoverable without writing unreadable v3 storage', () => {
  const disk = storage(), notice = vi.fn();
  const long = '界'.repeat(256);
  const runtimeId = long;
  const entries = Array.from({ length: 32 }, (_, i) => ({ id: long.slice(0, 250) + i, rootId: long.slice(0, 250) + i, kind: 'chat', titleHint: '界'.repeat(64), location: { agent: long } }));
  // Stay within the original 64KiB representation, then exceed it when the
  // runtime identity is repeated in all 32 v3 descriptors.
  entries.forEach(tab => { tab.id = tab.rootId.slice(0, 40); });
  entries.forEach((tab, i) => { tab.id = tab.id + i; });
  const old = JSON.stringify({ version: 2, workspaces: [{ runtimeId, layout: { type: 'pane', id: 'main', tabs: entries }, closed: [] }] });
  expect(new TextEncoder().encode(old).byteLength).toBeLessThan(64 * 1024);
  disk.setItem(PREVIOUS_TAB_STORAGE_KEY, old);
  const tabs = new SessionTabs(disk, notice);
  expect(tabs.workspace().tabs).toEqual([]);
  expect(tabs.getSnapshot().previous).toHaveLength(1);
  expect(disk.getItem(TAB_STORAGE_KEY)).toBeNull();
  expect(() => tabs.restorePrevious(runtimeId)).toThrow('metadata');
  tabs.openPrevious(runtimeId, entries[0]!.id);
  const reloaded = new SessionTabs(disk);
  expect(reloaded.workspace().tabs).toHaveLength(1);
  expect(reloaded.getSnapshot().previous[0]?.workspace.tabs).toHaveLength(32);
  expect(reloaded.workspace().tabs[0]?.location.agent).toBe(long);
  expect(notice).toHaveBeenCalledWith(expect.stringContaining('individually'));
});

it('remaps previous closed tabs and refuses to silently evict history when merging', () => {
  const disk = storage();
  disk.setItem(PREVIOUS_TAB_STORAGE_KEY, JSON.stringify({ version: 2, workspaces: ['a', 'b'].map(runtimeId => ({ runtimeId, layout: { type: 'pane', id: 'main', tabs: [{ id: 'open', rootId: 'open', kind: 'chat' }] }, closed: [{ tab: { id: 'closed', rootId: 'closed', kind: 'repl', location: { agent: 'child' } }, paneId: 'main', index: 0 }] })) }));
  const tabs = new SessionTabs(disk, undefined, 'a');
  tabs.restorePrevious('b');
  expect(tabs.workspace().closed.map(item => item.tab.runtimeId)).toEqual(['a', 'b']);
  const id = tabs.reopenView();
  expect(tabs.workspace().tabs.find(tab => tab.id === id)).toMatchObject({ runtimeId: 'b', rootId: 'closed', kind: 'repl', location: { agent: 'child' } });
  expect(new SessionTabs(disk).workspace()).toEqual(tabs.workspace());

  const full = new SessionTabs();
  for (let i = 0; i < 20; i++) full.open('a', `closed-${i}`);
  full.close('a', full.workspace().tabs.map(tab => tab.rootId));
  disk.setItem(TAB_STORAGE_KEY, JSON.stringify({ version: 3, workspace: full.workspace(), migrated: ['a'] }));
  const merging = new SessionTabs(disk);
  const before = merging.getSnapshot();
  expect(() => merging.restorePrevious('b')).toThrow('20 closed tabs');
  expect(merging.getSnapshot()).toBe(before);
  expect(new SessionTabs(disk).getSnapshot().previous).toHaveLength(1);
});

describe('New Chat descriptors', () => {
  it('refuses known nonpersistent storage before writing durable descriptors', () => {
    const disk = { ...storage(), persistent: false }, write = vi.spyOn(disk, 'setItem');
    const state = new SessionTabs(disk), before = state.workspace();
    expect(() => state.openNew()).toThrow('Window storage is unavailable');
    expect(() => state.ensureNew('recovery')).toThrow('Window storage is unavailable');
    expect(write).not.toHaveBeenCalled();
    expect(state.workspace()).toBe(before);
  });
  it('refuses promotion if storage silently falls back to memory during the write', () => {
    const disk = { ...storage(), persistent: true }, state = new SessionTabs(disk);
    const draft = state.openNew(), before = state.workspace();
    disk.setItem = () => { disk.persistent = false; };
    expect(() => state.promoteNew(draft.id, 'host', 'accepted')).toThrow('Window storage is unavailable');
    expect(state.workspace()).toBe(before);
    expect(() => state.updateNew(draft.id, { cwd: '/changed' })).toThrow('Window storage is unavailable');
    expect(state.workspace()).toBe(before);
  });
  it('accepts the existing partial session search descriptor contract', () => {
    expect(sessionSearch({ kind: 'repl', location: { agent: 'child' } })).toEqual({ agent: 'child', view: 'repl' });
    expect(sessionSearch({ kind: 'new' })).toEqual({});
  });
  it('materializes recovery identities once without selecting, reopening or replacing accepted work', () => {
    const disk = storage(), state = new SessionTabs(disk);
    const selected = state.openNew();
    const id = 'legacy-welcome-' + encodeURIComponent('runtime/one');
    const recovered = state.ensureNew(id, { cwd: '/legacy' });
    expect(selectedSessionTab(state.workspace())?.id).toBe(selected.id);
    expect(state.ensureNew(id, { cwd: '/ignored' })).toEqual(recovered);
    expect(state.workspace().tabs).toHaveLength(2);
    state.closeViews([id]);
    expect(state.ensureNew(id)).toEqual(recovered);
    expect(state.workspace().tabs).toHaveLength(1);
    state.promoteNew(id, 'runtime/one', 'accepted');
    expect(state.ensureNew(id)).toMatchObject({ kind: 'chat', rootId: 'accepted' });
    expect(new SessionTabs(disk).workspace()).toEqual(state.workspace());
  });
  it('keeps recovery materialization atomic when capacity or storage refuses it', () => {
    const disk = storage(), state = new SessionTabs(disk);
    for (let i = 0; i < 32; i++) state.openNew();
    const before = state.workspace();
    expect(() => state.ensureNew('legacy-welcome-host')).toThrow('32');
    expect(state.workspace()).toBe(before);
    const other = new SessionTabs(disk);
    other.closeViews([other.workspace().tabs[0]!.id]);
    const beforeFailure = other.workspace();
    disk.setItem = () => { throw new Error('quota'); };
    expect(() => other.ensureNew('legacy-welcome-host')).toThrow('quota');
    expect(other.workspace()).toBe(beforeFailure);
  });
  it('reopens only the requested retained view', () => {
    const state = new SessionTabs();
    const first = state.openNew(), second = state.openNew();
    state.closeViews([first.id]); state.closeViews([second.id]);
    const before = state.workspace();
    expect(state.reopenView('missing')).toBeUndefined();
    expect(state.workspace()).toBe(before);
    expect(state.reopenView(first.id)).toBe(first.id);
    expect(state.workspace().tabs.map(tab => tab.id)).toEqual([first.id]);
    expect(state.workspace().closed.map(item => item.tab.id)).toEqual([second.id]);
    expect(state.reopenView()).toBe(second.id);
  });
  it('refuses oversized durable metadata without evicting retained closed drafts', () => {
    const state = new SessionTabs(storage());
    const closed = state.openNew(); state.closeViews([closed.id]);
    for (let i = 0; i < 13; i++) state.openNew({ cwd: 'x'.repeat(4096) });
    const before = state.workspace();
    expect(() => state.openNew({ cwd: '界'.repeat(4096) })).toThrow('layout is full');
    expect(state.workspace()).toBe(before);
    expect(state.workspace().closed[0]?.tab.id).toBe(closed.id);
  });
  it('opens fresh independent drafts after selection and restores mixed v3 layouts', () => {
    const disk = storage(), state = new SessionTabs(disk);
    state.visit('mac', 'root', {});
    const first = state.openNew({ hostProfileId: 'local', cwd: '/one', permissionMode: 'automatic' });
    state.open('mac', 'last');
    const second = state.openNew();
    expect(second.id).not.toBe(first.id);
    expect(state.workspace().tabs.map(tab => tab.id)).toEqual(['root', first.id, second.id, 'last']);
    expect(selectedSessionTab(state.workspace())?.id).toBe(second.id);
    state.updateNew(first.id, { cwd: '/changed', runtimeId: 'mac' });
    expect(state.workspace().tabs.find(tab => tab.id === second.id)).toEqual(second);
    expect(new SessionTabs(disk).workspace()).toEqual(state.workspace());
    expect(sessionSearch(first)).toEqual({});
    expect('rootId' in first).toBe(false);
  });
  it('preserves selection at capacity and promotes in place without adding capacity', () => {
    const state = new SessionTabs(storage());
    const first = state.openNew();
    for (let i = 1; i < 32; i++) state.openNew();
    const before = state.workspace();
    expect(state.canOpen()).toBe(false);
    expect(() => state.openNew()).toThrow('32');
    expect(state.workspace()).toBe(before);
    expect(state.promoteNew(first.id, 'mac', 'root')).toBe(true);
    expect(state.workspace().tabs).toHaveLength(32);
    expect(state.workspace().tabs[0]).toMatchObject({ id: first.id, kind: 'chat', rootId: 'root' });
    expect(selectedSessionTab(state.workspace())?.id).toBe(selectedSessionTab(before)?.id);
    expect(state.promoteNew(first.id, 'mac', 'root')).toBe(true);
    expect(state.promoteNew(first.id, 'mac', 'other')).toBe(false);
  });
  it('moves drafts into splits without duplicating their identity, including at capacity', () => {
    const state = new SessionTabs();
    const first = state.openNew();
    for (let i = 1; i < 32; i++) state.openNew();
    expect(state.split(first.id, 'right')).toBe(first.id);
    expect(sessionPanes(state.workspace().layout)).toHaveLength(2);
    expect(state.workspace().tabs.filter(tab => tab.id === first.id)).toHaveLength(1);
    expect(state.workspace().tabs).toHaveLength(32);
  });
  it('promotes retained closed drafts without reopening or stealing focus', () => {
    const disk = storage(), state = new SessionTabs(disk);
    const first = state.openNew(), second = state.openNew();
    state.closeViews([first.id]);
    state.updateNew(first.id, { cwd: '/closed' });
    expect(state.promoteNew(first.id, 'remote', 'accepted')).toBe(true);
    expect(selectedSessionTab(state.workspace())?.id).toBe(second.id);
    expect(state.workspace().tabs).toHaveLength(1);
    const restored = new SessionTabs(disk);
    expect(restored.reopenView()).toBe(first.id);
    expect(restored.workspace().tabs.find(tab => tab.id === first.id)).toMatchObject({ kind: 'chat', rootId: 'accepted' });
    expect(restored.promoteNew('missing', 'remote', 'accepted')).toBe(false);
  });
  it('roundtrips unfinished close/reopen and rejects malformed draft metadata', () => {
    const disk = storage(), state = new SessionTabs(disk);
    const draft = state.openNew({ cwd: '/work', permissionMode: 'automatic' });
    state.closeViews([draft.id]);
    const restored = new SessionTabs(disk);
    expect(restored.reopenView()).toBe(draft.id);
    expect(restored.workspace().tabs[0]).toEqual(draft);
    expect(() => state.openNew({ cwd: 'x'.repeat(4097) })).toThrow('Invalid');
    expect(() => state.updateNew(draft.id, { cwd: '\0' })).toThrow('Invalid');
    const raw = JSON.parse(disk.getItem(TAB_STORAGE_KEY)!);
    raw.workspace.layout.tabs[0].permissionMode = 'untrusted';
    disk.setItem(TAB_STORAGE_KEY, JSON.stringify(raw));
    expect(new SessionTabs(disk).workspace().tabs).toEqual([]);
  });
  it('refuses nondurable creation, updates and promotion without mutating the snapshot', () => {
    const disk = storage(), state = new SessionTabs(disk);
    const draft = state.openNew();
    const before = state.workspace();
    disk.setItem = () => { throw new Error('quota'); };
    expect(() => state.openNew()).toThrow('quota');
    expect(() => state.updateNew(draft.id, { cwd: '/changed' })).toThrow('quota');
    expect(() => state.promoteNew(draft.id, 'mac', 'accepted')).toThrow('quota');
    expect(state.workspace()).toBe(before);
  });
});

describe('terminal tabs', () => {
  const terminal = (state: SessionTabs, terminalId = 'term-1', cwd = '/work/project') => state.openTerminal('mac', terminalId, cwd);
  it('inserts after the selected tab, selects it, and keeps session-only operations away from it', () => {
    const state = new SessionTabs();
    state.open('mac', 'a'); state.open('mac', 'b'); state.visit('mac', 'a', {});
    const id = terminal(state);
    expect(state.workspace().tabs.map(tab => tab.kind)).toEqual(['chat', 'terminal', 'chat']);
    expect(selectedSessionTab(state.workspace())).toMatchObject({ id, kind: 'terminal', runtimeId: 'mac', terminalId: 'term-1', cwd: '/work/project', titleHint: '' });
    // Titles from session summaries and root purges never touch a terminal descriptor.
    state.titles('mac', new Map([['a', 'Alpha'], ['term-1', 'Nope']]));
    state.purge('mac', 'term-1');
    expect(state.workspace().tabs.find(tab => tab.id === id)).toMatchObject({ kind: 'terminal', titleHint: '' });
    expect(state.preferred('mac', 'term-1')).toBeUndefined();
    expect(state.updateTerminal(id, { titleHint: 'zsh — project', terminalId: 'term-2' })).toBe(true);
    expect(state.workspace().tabs.find(tab => tab.id === id)).toMatchObject({ titleHint: 'zsh — project', terminalId: 'term-2', cwd: '/work/project' });
    expect(state.updateTerminal('missing', { titleHint: 'x' })).toBe(false);
    expect(() => state.updateTerminal(id, { cwd: 'bad\nline' })).toThrow('Invalid terminal options');
  });
  it('persists and restores terminal descriptors while dropping malformed ones', () => {
    const disk = storage();
    const state = new SessionTabs(disk);
    const id = terminal(state);
    const restored = new SessionTabs(disk);
    expect(restored.workspace().tabs).toEqual(state.workspace().tabs);
    expect(restored.workspace().tabs[0]).toMatchObject({ id, kind: 'terminal', terminalId: 'term-1' });
    const raw = JSON.parse(disk.getItem(TAB_STORAGE_KEY)!);
    raw.workspace.layout.tabs.push({ id: 'broken', kind: 'terminal', runtimeId: 'mac', cwd: '/x' });
    raw.workspace.layout.tabs.push({ id: 'bad-cwd', kind: 'terminal', runtimeId: 'mac', terminalId: 't', cwd: 'a\0b' });
    disk.setItem(TAB_STORAGE_KEY, JSON.stringify(raw));
    expect(new SessionTabs(disk).workspace().tabs.map(tab => tab.id)).toEqual([id]);
  });
  it('never enters Reopen history, moves instead of duplicating, and counts toward capacity', () => {
    const state = new SessionTabs();
    state.open('mac', 'a');
    const id = terminal(state);
    state.closeViews([id]);
    expect(state.workspace().closed).toEqual([]);
    expect(state.reopenView()).toBeUndefined();
    const again = terminal(state, 'term-9');
    const moved = state.split(again, 'right');
    expect(moved).toBe(again);
    expect(sessionPanes(state.workspace().layout)).toHaveLength(2);
    expect(state.workspace().tabs.filter(tab => tab.kind === 'terminal')).toHaveLength(1);
    for (let index = 0; index < 30; index++) state.open('mac', `root-${index}`);
    expect(state.canOpen()).toBe(false);
    expect(() => terminal(state, 'term-full')).toThrow('32 open session tabs');
  });
});
