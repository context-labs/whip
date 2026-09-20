import { describe, expect, it } from 'vitest';
import { emptySidebarState, newSessionSearch, readSidebarState, setDirectoryCollapsed, sidebarMaxWidth, sidebarRows, sidebarWidth } from '../src/sidebar-state';

const session = (id: string, cwd: string, pinned = false) => ({ id, cwd, pinned, title: id, kind: 'root', model: '', provider: '', updated_at: '', truncated: false });
describe('directory navigation projection', () => {
  it('groups exact directories in catalog order without merging worktrees or resolving paths', () => {
    const items = [session('pinned', '/repo/main', true), session('recent', '/worktrees/main'), session('older', '/repo/main'), session('unknown', ''), session('relative', './repo/main')];
    const rows = sidebarRows(items);
    expect(rows.map(row => row.key)).toEqual(['directory:/repo/main', 'session:pinned', 'session:older', 'directory:/worktrees/main', 'session:recent', 'directory:', 'session:unknown', 'directory:./repo/main', 'session:relative']);
    expect(rows.filter(row => row.kind === 'directory').map(row => row.label)).toEqual(['main · repo', 'main · worktrees', 'Other sessions', 'main · ./repo']);
    expect(rows[1]?.kind === 'session' && rows[1].session).toBe(items[0]);
    expect(sidebarRows([...items, session('next page', '/repo/main')]).map(row => row.key).slice(0, 4)).toEqual(['directory:/repo/main', 'session:pinned', 'session:older', 'session:next page']);
  });
  it('limits each directory to seven sessions and expands only the requested directory', () => {
    const a = Array.from({ length: 17 }, (_, index) => session(`a${index}`, '/a', index === 0));
    const b = Array.from({ length: 8 }, (_, index) => session(`b${index}`, '/b'));
    const items = [...a, ...b];
    const visible = (limit = 7, collapsed: string[] = []) => sidebarRows(items, collapsed, new Map([['/a', limit]]));
    expect(visible().filter(row => row.kind === 'session').map(row => row.session.id)).toEqual([...a.slice(0, 7), ...b.slice(0, 7)].map(item => item.id));
    expect(visible().filter(row => row.kind === 'more')).toEqual([
      { key: 'more:/a', kind: 'more', cwd: '/a', expanded: false, visibleCount: 7 },
      { key: 'more:/b', kind: 'more', cwd: '/b', expanded: false, visibleCount: 7 },
    ]);
    expect(visible(14).filter(row => row.kind === 'session').map(row => row.session.id)).toEqual([...a.slice(0, 14), ...b.slice(0, 7)].map(item => item.id));
    expect(visible(14).find(row => row.key === 'more:/a')).toMatchObject({ expanded: false, visibleCount: 14 });
    expect(visible(21).filter(row => row.kind === 'session').map(row => row.session.id)).toEqual([...a, ...b.slice(0, 7)].map(item => item.id));
    expect(visible(21).find(row => row.key === 'more:/a')).toMatchObject({ expanded: true, visibleCount: 17 });
    expect(visible(7).filter(row => row.kind === 'session')).toHaveLength(14);
    expect(visible(21, ['/a']).some(row => row.key === 'more:/a')).toBe(false);
    expect(sidebarRows(a.slice(0, 7)).some(row => row.kind === 'more')).toBe(false);
    expect(sidebarRows([...items, session('next-page', '/a')], [], new Map([['/a', 21]])).some(row => row.key === 'session:next-page')).toBe(true);
  });
  it('keeps a root-level duplicate name readable', () => {
    expect(sidebarRows([session('a', '/foo'), session('b', '/repo/foo')]).filter(row => row.kind === 'directory').map(row => row.label)).toEqual(['foo · /foo', 'foo · repo']);
  });
  it('collapses only the chosen directory', () => {
    const items = [session('one', '/a'), session('two', '/b'), session('three', '/a')];
    expect(sidebarRows(items, ['/a']).map(row => row.key)).toEqual(['directory:/a', 'directory:/b', 'session:two']);
    expect(sidebarRows([])).toEqual([]);
  });
});
describe('bounded window sidebar preferences', () => {
  it('validates persisted metadata and constrains remembered and effective widths', () => {
    for (const raw of [null, 'bad', 'null', '{}', JSON.stringify({ version: 1, width: '400', hidden: false, hosts: [] }), ' '.repeat(65537)]) expect(readSidebarState(raw)).toEqual(emptySidebarState());
    expect(readSidebarState(JSON.stringify({ version: 1, width: 999, hidden: true, hosts: [{ runtimeId: 'a', collapsed: ['/x', '/x', null] }] }))).toEqual({ width: 420, hidden: true, hosts: [{ runtimeId: 'a', collapsed: ['/x'] }] });
    expect(sidebarWidth(320, 768)).toBe(288);
    expect(sidebarWidth(100, 1440)).toBe(256);
    expect(sidebarMaxWidth(1440)).toBe(420);
  });
  it('scopes collapse to four hosts and sixty-four paths without mutating previous snapshots', () => {
    let state = emptySidebarState();
    for (let host = 0; host < 5; host++) for (let path = 0; path < 65; path++) state = setDirectoryCollapsed(state, `h${host}`, `/p${path}`, true);
    expect(state.hosts.map(host => host.runtimeId)).toEqual(['h1', 'h2', 'h3', 'h4']);
    expect(state.hosts[3]?.collapsed).toHaveLength(64);
    expect(state.hosts[3]?.collapsed[0]).toBe('/p1');
    const before = JSON.stringify(state);
    const expanded = setDirectoryCollapsed(state, 'h4', '/p64', false);
    expect(expanded.hosts[3]?.collapsed).not.toContain('/p64');
    expect(JSON.stringify(state)).toBe(before);
    const long = '/😀'.repeat(1000);
    const snapshots = [];
    for (let i = 0; i < 40; i++) { snapshots.push([state, JSON.stringify(state)] as const); state = setDirectoryCollapsed(state, 'long', `${long}/${i}`, true); }
    expect(new TextEncoder().encode(JSON.stringify(state)).length).toBeLessThan(64 << 10);
    for (const [snapshot, json] of snapshots) expect(JSON.stringify(snapshot)).toBe(json);
  });
});
it('accepts explicit creation intent and bounded host-only or directory prefill', () => {
  expect(newSessionSearch({ new: '1', cwd: '/repo', runtimeId: 'host', ignored: true })).toEqual({ new: 1, cwd: '/repo', runtimeId: 'host' });
  expect(newSessionSearch({ new: 1 })).toEqual({ new: 1 });
  expect(newSessionSearch({ runtimeId: 'host' })).toEqual({ runtimeId: 'host' });
  for (const cwd of [5, '', 'x'.repeat(4097), '/bad\npath']) expect(newSessionSearch({ cwd, runtimeId: 'host' })).toEqual({ runtimeId: 'host' });
  for (const input of [{ cwd: '/repo' }, { new: 2 }, { runtimeId: 'x'.repeat(257) }, { runtimeId: 'bad\nhost' }]) expect(newSessionSearch(input)).toEqual({});
});
