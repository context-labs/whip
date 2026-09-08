import { describe, expect, it, vi } from 'vitest';
import { SessionTabs, TAB_STORAGE_KEY, LEGACY_TAB_STORAGE_KEY, sessionPanes, sessionViewPane, selectedSessionTab, sessionSearch, validateSessionSearch } from '../src/session-tabs';
import { sessionDestination } from '../src/session-tab-routing';
import type { AppStorage } from '../src/platform';

function storage(): AppStorage {
  const values = new Map<string, string>();
  return { keys: () => [...values.keys()], getItem: key => values.get(key) ?? null, setItem: (key, value) => { values.set(key, value); }, removeItem: key => { values.delete(key); } };
}
describe('window session tabs', () => {
  it('deduplicates roots while preserving separate hosts and selected child context', () => {
    const state = new SessionTabs();
    state.open('mac', 'root', 'Review'); state.visit('mac', 'root', { agent: 'child', panel: 'execution' }); state.open('mac', 'root', 'ignored');
    state.open('server', 'root', 'Review');
    expect(state.workspace('mac').tabs).toHaveLength(1);
    expect(state.workspace('mac').tabs[0]?.location).toEqual({ agent: 'child', panel: 'execution' });
    expect(state.workspace('server').lastActiveRootId).toBeUndefined();
    const snapshot = state.getSnapshot(); state.visit('mac', 'root', { agent: 'child', panel: 'execution' });
    expect(state.getSnapshot()).toBe(snapshot);
  });
  it('closes active tabs to the right then left, preserving background selection and reopen order', () => {
    const state = new SessionTabs();
    ['a', 'b', 'c'].forEach(id => state.open('mac', id));
    state.visit('mac', 'b', { panel: 'context' });
    expect(state.close('mac', ['a'], 'b')).toBeUndefined();
    expect(state.workspace('mac').lastActiveRootId).toBe('b');
    expect(state.reopenView('mac')).toBe('a');
    expect(state.workspace('mac').tabs.map(x => x.rootId)).toEqual(['a', 'b', 'c']);
    expect(state.close('mac', ['b'], 'b')).toBe('c');
    expect(state.close('mac', ['c'], 'c')).toBe('a');
    expect(state.close('mac', ['a'], 'a')).toBeNull();
    state.reopenView('mac'); state.reopenView('mac'); state.reopenView('mac');
    expect(state.workspace('mac').tabs.find(x => x.rootId === 'b')?.location.panel).toBe('context');
  });
  it('closes a group atomically and does not replace the active background session', () => {
    const state = new SessionTabs(), update = vi.fn();
    ['a', 'b', 'c', 'd'].forEach(id => state.open('mac', id)); state.visit('mac', 'c', {});
    const off = state.subscribe(update);
    expect(state.close('mac', ['a', 'b', 'd'], 'c')).toBeUndefined();
    expect(update).toHaveBeenCalledTimes(1);
    expect(state.workspace('mac').tabs.map(x => x.rootId)).toEqual(['c']); off();
  });
  it('bounds open tabs without silently evicting work and bounds closed history', () => {
    const state = new SessionTabs();
    for (let i = 0; i < 32; i++) state.open('mac', `root-${i}`);
    expect(() => state.open('mac', 'extra')).toThrow('32 open');
    expect(state.workspace('mac').tabs[0]?.rootId).toBe('root-0');
    expect(() => state.open('mac', 'root-0')).not.toThrow();
    state.close('mac', state.workspace('mac').tabs.map(x => x.rootId));
    expect(state.workspace('mac').closed).toHaveLength(20);
  });
  it('accepts exact reorder permutations only', () => {
    const state = new SessionTabs(); ['a', 'b', 'c'].forEach(id => state.open('mac', id));
    state.move('mac', 'c', -1); expect(state.workspace('mac').tabs.map(x => x.rootId)).toEqual(['a', 'c', 'b']);
    expect(() => state.reorderPane('mac', 'main', ['a', 'c', 'c'])).toThrow();
    expect(() => state.reorderPane('mac', 'main', ['a', 'c', 'unknown'])).toThrow();
    state.reorderPane('mac', 'main', ['b', 'c', 'a']); expect(state.workspace('mac').tabs.map(x => x.rootId)).toEqual(['b', 'c', 'a']);
  });
  it('restores a window independently, preserving valid entries from damaged records', () => {
    const disk = storage(); disk.setItem(TAB_STORAGE_KEY, JSON.stringify({ version: 1, workspaces: [{ runtimeId: 'mac', tabs: [null, { rootId: 'a', titleHint: 'A', location: { panel: 'fake', agent: 'child', secret: 'not copied' } }, { rootId: 'a' }, { rootId: 'b' }], lastActiveRootId: 'b' }] }));
    const state = new SessionTabs(disk), independent = new SessionTabs(disk);
    expect(state.workspace('mac').tabs.map(x => x.rootId)).toEqual(['a', 'b']);
    expect(state.workspace('mac').tabs[0]?.location).toEqual({ agent: 'child' });
    state.close('mac', ['a']); expect(independent.workspace('mac').tabs).toHaveLength(2);
    const restored = new SessionTabs(disk); expect(restored.workspace('mac').tabs).toHaveLength(1);
    expect(restored.workspace('mac').lastActiveRootId).toBe('b');
    restored.home('mac'); expect(new SessionTabs(disk).workspace('mac').lastActiveRootId).toBeUndefined();
  });
  it('bounds bytes and host layouts and retains no prompt or operation payloads', () => {
    const disk = storage(), state = new SessionTabs(disk);
    for (let host = 0; host < 6; host++) for (let i = 0; i < 32; i++) state.open(`host-${host}`, `root-${i}`, '界'.repeat(1000));
    const saved = disk.getItem(TAB_STORAGE_KEY)!;
    expect(new TextEncoder().encode(saved).byteLength).toBeLessThanOrEqual(64 * 1024);
    expect(state.getSnapshot().workspaces.length).toBeLessThanOrEqual(4);
    expect(state.workspace('host-5').tabs).toHaveLength(32);
    expect(Object.keys(state.workspace('host-5').tabs[0]!)).toEqual(['id', 'kind', 'rootId', 'titleHint', 'location']);
  });
  it('continues in memory after denied storage and rejects invalid identities', () => {
    const disk = storage(), notice = vi.fn(); disk.setItem = () => { throw Error('denied'); };
    const state = new SessionTabs(disk, notice); state.open('mac', 'root');
    expect(state.workspace('mac').tabs).toHaveLength(1); expect(notice).toHaveBeenCalled();
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
    const right = state.split('mac', 'root', 'right');
    const bottom = state.split('mac', right, 'bottom');
    state.updateLocation('mac', right, { agent: 'second', panel: 'execution' });
    const workspace = state.workspace('mac');
    expect(workspace.layout).toMatchObject({ type: 'split', direction: 'horizontal', first: { type: 'pane' }, second: { type: 'split', direction: 'vertical' } });
    expect(workspace.tabs.map(tab => tab.rootId)).toEqual(['root', 'root', 'root']);
    expect(new Set(workspace.tabs.map(tab => tab.id)).size).toBe(3);
    expect(workspace.tabs.find(tab => tab.id === right)?.location).toEqual({ agent: 'second', panel: 'execution' });
    expect(workspace.tabs.find(tab => tab.id === bottom)?.location).toEqual({ agent: 'first', panel: 'context' });
    expect(state.preferred('mac', 'root')?.id).toBe(bottom);
    state.visit('mac', 'root', { agent: 'third' }, right);
    expect(selectedSessionTab(state.workspace('mac'))?.id).toBe(right);
    expect(state.workspace('mac').tabs.find(tab => tab.id === 'root')?.location.agent).toBe('first');
  });
  it('transfers view identity, prunes empty branches and reopens into a surviving pane', () => {
    const state = new SessionTabs(); state.visit('mac', 'root', { agent: 'child' });
    const duplicate = state.split('mac', 'root', 'right');
    const third = state.split('mac', duplicate, 'bottom');
    const target = sessionViewPane(state.workspace('mac'), 'root')!.id;
    state.transfer('mac', duplicate, target, undefined, 0);
    expect(sessionPanes(state.workspace('mac').layout)).toHaveLength(2);
    expect(sessionViewPane(state.workspace('mac'), duplicate)?.tabs.map(tab => tab.id)).toEqual([duplicate, 'root']);
    expect(state.workspace('mac').closed).toEqual([]);
    state.activate('mac', third);
    expect(state.closeViews('mac', [third], third)).toBe(duplicate);
    expect(state.workspace('mac').layout.type).toBe('pane');
    expect(state.reopenView('mac')).toBe(third);
    expect(sessionPanes(state.workspace('mac').layout)).toHaveLength(1);
    expect(state.workspace('mac').tabs.find(tab => tab.id === third)?.location).toEqual({ agent: 'child' });
  });
  it('enforces pane and view limits atomically while allowing transfers at capacity', () => {
    const state = new SessionTabs(); state.open('mac', 'root');
    const copies = Array.from({ length: 3 }, () => state.split('mac', 'root', 'right'));
    const fullPanes = state.getSnapshot();
    expect(() => state.split('mac', 'root', 'top')).toThrow('four panes');
    expect(state.getSnapshot()).toBe(fullPanes);
    const target = sessionViewPane(state.workspace('mac'), 'root')!.id;
    state.transfer('mac', copies[0]!, target, 'bottom');
    expect(sessionPanes(state.workspace('mac').layout)).toHaveLength(4);
    for (let i = 0; i < 28; i++) state.open('mac', `extra-${i}`);
    const full = state.getSnapshot();
    expect(() => state.open('mac', 'overflow')).toThrow('32 open');
    expect(state.getSnapshot()).toBe(full);
    state.closeViews('mac', [copies[1]!]);
    state.open('mac', 'replacement');
    expect(() => state.split('mac', 'root', 'left')).toThrow('32 open');
    expect(state.workspace('mac').tabs).toHaveLength(32);
  });
  it('migrates legacy layouts then restores v2 duplicate views, focus and split ratios', () => {
    const disk = storage();
    disk.setItem(LEGACY_TAB_STORAGE_KEY, JSON.stringify({ version: 1, workspaces: [{ runtimeId: 'mac', tabs: [{ rootId: 'root', location: { agent: 'child' } }], lastActiveRootId: 'root' }] }));
    const state = new SessionTabs(disk);
    expect(state.workspace('mac').tabs[0]).toMatchObject({ id: 'root', kind: 'chat', rootId: 'root' });
    const duplicate = state.split('mac', 'root', 'left');
    state.updateLocation('mac', duplicate, { panel: 'execution' });
    state.resize('mac', state.workspace('mac').layout.id, .35);
    const restored = new SessionTabs(disk);
    expect(restored.workspace('mac')).toEqual(state.workspace('mac'));
    expect(selectedSessionTab(restored.workspace('mac'))?.id).toBe(duplicate);
    expect(JSON.parse(disk.getItem(TAB_STORAGE_KEY)!).version).toBe(2);
  });
  it('rejects malformed split trees and sanitizes duplicate view identities on restore', () => {
    const disk = storage();
    const tab = { id: 'view', kind: 'chat', rootId: 'root', location: { panel: 'invalid', agent: 'child' } };
    const pane = { type: 'pane', id: 'main', tabs: [tab, tab, { ...tab, id: 'other', kind: 'terminal' }], selected: 'missing' };
    const load = (layout: unknown) => { disk.setItem(TAB_STORAGE_KEY, JSON.stringify({ version: 2, workspaces: [{ runtimeId: 'mac', layout, focusedPaneId: 'missing', closed: [] }] })); return new SessionTabs(disk); };
    const valid = load(pane).workspace('mac');
    expect(valid.tabs).toHaveLength(1); expect(valid.focusedPaneId).toBe('main');
    expect(selectedSessionTab(valid)?.location).toEqual({ agent: 'child' });
    for (const ratio of [0, 1, -1, null]) expect(load({ type: 'split', id: 'split', direction: 'horizontal', ratio, first: pane, second: { ...pane, id: 'second' } }).getSnapshot().workspaces).toEqual([]);
    expect(load({ type: 'split', id: 'split', direction: 'horizontal', ratio: .5, first: pane, second: pane }).getSnapshot().workspaces).toEqual([]);
  });
});

it('sanitizes overfull restored panes and rejects trees beyond the four-pane bound', () => {
  const disk = storage();
  const pane = (id: string) => ({ type: 'pane', id, tabs: Array.from({ length: 40 }, (_, index) => ({ id: `${id}-${index}`, kind: 'chat', rootId: `root-${index}`, location: {} })) });
  const split = (id: string, first: unknown, second: unknown) => ({ type: 'split', id, direction: 'horizontal', ratio: .5, first, second });
  const load = (layout: unknown) => { disk.setItem(TAB_STORAGE_KEY, JSON.stringify({ version: 2, workspaces: [{ runtimeId: 'mac', layout, closed: [] }] })); return new SessionTabs(disk); };
  expect(load(pane('one')).workspace('mac').tabs).toHaveLength(32);
  const five = split('top', split('left', pane('a'), pane('b')), split('right', pane('c'), split('nested', pane('d'), pane('e'))));
  expect(load(five).getSnapshot().workspaces).toEqual([]);
});

it('moves a view within its pane using original drop positions without duplicating it', () => {
  const state = new SessionTabs(); ['a', 'b', 'c', 'd'].forEach(id => state.open('mac', id));
  const pane = state.workspace('mac').focusedPaneId;
  state.transfer('mac', 'a', pane, undefined, 3);
  expect(state.workspace('mac').tabs.map(tab => tab.id)).toEqual(['b', 'c', 'a', 'd']);
  state.transfer('mac', 'd', pane, undefined, 0);
  expect(state.workspace('mac').tabs.map(tab => tab.id)).toEqual(['d', 'b', 'c', 'a']);
  expect(selectedSessionTab(state.workspace('mac'))?.id).toBe('d');
});

it('reuses the selected duplicate when multiple views of one root share a pane', () => {
  const tabs = new SessionTabs();
  tabs.visit('mac', 'root', { agent: 'first' });
  const duplicate = tabs.split('mac', 'root', 'right');
  tabs.visit('mac', 'root', { agent: 'second' }, duplicate);
  tabs.transfer('mac', duplicate, 'main');
  expect(tabs.preferred('mac', 'root')?.id).toBe(duplicate);
  expect(tabs.open('mac', 'root')).toBe(duplicate);
  tabs.visit('mac', 'root', { agent: 'second', panel: 'execution' });
  expect(tabs.workspace('mac').tabs.find(tab => tab.id === 'root')?.location).toEqual({ agent: 'first' });
  expect(tabs.workspace('mac').tabs.find(tab => tab.id === duplicate)?.location).toEqual({ agent: 'second', panel: 'execution' });
});

describe('session presentation mode', () => {
  it('switches only the targeted view in place and keeps mode out of its location', () => {
    const tabs = new SessionTabs();
    tabs.visit('mac', 'root', { agent: 'first', panel: 'context' });
    const duplicate = tabs.split('mac', 'root', 'right');
    const paneId = sessionViewPane(tabs.workspace('mac'), duplicate)!.id;
    tabs.visit('mac', 'root', { view: 'repl', agent: 'second', panel: 'execution' }, duplicate);
    expect(tabs.workspace('mac').tabs).toHaveLength(2);
    expect(sessionViewPane(tabs.workspace('mac'), duplicate)?.id).toBe(paneId);
    expect(tabs.workspace('mac').tabs.find(tab => tab.id === 'root')).toMatchObject({ kind: 'chat', location: { agent: 'first', panel: 'context' } });
    expect(tabs.preferred('mac', 'root')).toMatchObject({ id: duplicate, kind: 'repl', location: { agent: 'second', panel: 'execution' } });
    expect(sessionSearch(tabs.preferred('mac', 'root'))).toEqual({ view: 'repl', agent: 'second', panel: 'execution' });
    tabs.updateLocation('mac', duplicate, { agent: 'third' });
    expect(sessionSearch(tabs.preferred('mac', 'root'))).toEqual({ view: 'repl', agent: 'third' });
    tabs.visit('mac', 'root', { agent: 'third' }, duplicate);
    expect(tabs.preferred('mac', 'root')?.kind).toBe('chat');
    expect(sessionSearch(tabs.preferred('mac', 'root'))).toEqual({ agent: 'third' });
  });

  it('copies and retains REPL mode through splitting, transfer, close, reopen and reload', () => {
    const disk = storage(), tabs = new SessionTabs(disk);
    tabs.visit('mac', 'root', { view: 'repl', agent: 'child' });
    const duplicate = tabs.split('mac', 'root', 'right');
    tabs.transfer('mac', duplicate, 'main');
    expect(tabs.workspace('mac').tabs.map(tab => tab.kind)).toEqual(['repl', 'repl']);
    tabs.closeViews('mac', [duplicate], duplicate);
    const restored = new SessionTabs(disk);
    expect(restored.workspace('mac').closed[0]?.tab).toMatchObject({ id: duplicate, kind: 'repl', location: { agent: 'child' } });
    expect(restored.reopenView('mac')).toBe(duplicate);
    expect(restored.workspace('mac').tabs.find(tab => tab.id === duplicate)?.kind).toBe('repl');
    expect(new SessionTabs(disk).workspace('mac')).toEqual(restored.workspace('mac'));
    expect(restored.workspace('other-host').tabs).toEqual([]);
  });

  it('validates saved modes while migrating chat descriptors from legacy storage', () => {
    const disk = storage();
    const tabs = [{ rootId: 'old' }, { rootId: 'chat', kind: 'chat' }, { rootId: 'repl', kind: 'repl' }, { rootId: 'unknown', kind: 'terminal' }, { rootId: 'null', kind: null }];
    disk.setItem(LEGACY_TAB_STORAGE_KEY, JSON.stringify({ version: 1, workspaces: [{ runtimeId: 'mac', tabs }] }));
    expect(new SessionTabs(disk).workspace('mac').tabs.map(tab => [tab.rootId, tab.kind])).toEqual([['old', 'chat'], ['chat', 'chat'], ['repl', 'repl']]);
    disk.setItem(TAB_STORAGE_KEY, JSON.stringify({ version: 2, workspaces: [{ runtimeId: 'mac', layout: { type: 'pane', id: 'main', tabs: tabs.map(tab => ({ ...tab, id: tab.rootId, location: { view: 'repl' } })) }, closed: [] }] }));
    const restored = new SessionTabs(disk).workspace('mac');
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
