import { describe, expect, it, vi } from 'vitest';
import { SessionTabs, TAB_STORAGE_KEY } from '../src/session-tabs';
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
    expect(state.reopen('mac')).toBe('a');
    expect(state.workspace('mac').tabs.map(x => x.rootId)).toEqual(['a', 'b', 'c']);
    expect(state.close('mac', ['b'], 'b')).toBe('c');
    expect(state.close('mac', ['c'], 'c')).toBe('a');
    expect(state.close('mac', ['a'], 'a')).toBeNull();
    state.reopen('mac'); state.reopen('mac'); state.reopen('mac');
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
    expect(() => state.reorder('mac', ['a', 'c', 'c'])).toThrow();
    expect(() => state.reorder('mac', ['a', 'c', 'unknown'])).toThrow();
    state.reorder('mac', ['b', 'c', 'a']); expect(state.workspace('mac').tabs.map(x => x.rootId)).toEqual(['b', 'c', 'a']);
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
    expect(Object.keys(state.workspace('host-5').tabs[0]!)).toEqual(['rootId', 'titleHint', 'location']);
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
