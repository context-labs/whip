import { afterEach, describe, expect, it, vi } from 'vitest';
import type { AnyRouter } from '@tanstack/react-router';
import { AppRuntime } from '../src/runtime';
import { bindSettingsNavigation, parseSettingsReturn, searchSettings, settingsCategories, settingsBackDestination, settingsReturnKey, validateSettingsSearch } from '../src/settings/navigation';

const runtimes: AppRuntime[] = [];
afterEach(() => { for (const runtime of runtimes.splice(0)) runtime.dispose(); });
function fixture() {
  const data = new Map<string, string>();
  const storage = { keys: () => [...data.keys()], getItem: (key: string) => data.get(key) ?? null, setItem: (key: string, value: string) => { data.set(key, value); }, removeItem: (key: string) => { data.delete(key); } };
  const platform = { defaultEndpoint: 'http://127.0.0.1:8080', storage, windowStorage: storage, copy: async () => {}, download: async () => {}, openExternal: async () => {} };
  const runtime = new AppRuntime(platform); runtimes.push(runtime);
  return { runtime, storage, data, platform };
}
describe('Settings navigation', () => {
  it('names server management consistently while retaining saved Connections routes and search terms', () => {
    expect(settingsCategories.find(category => category.id === 'connections')?.label).toBe('Servers');
    for (const query of ['servers', 'connections', 'execution hosts', 'ssh', 'tailscale']) {
      expect(searchSettings(query, false, false)).toContainEqual(expect.objectContaining({ id: 'hosts', section: 'connections', label: 'Servers' }));
    }
    expect(validateSettingsSearch({ section: 'connections', setting: 'hosts' })).toEqual({ section: 'connections', setting: 'hosts' });
  });
  it('keeps search local, capability-scoped and typed with bounded host identifiers', () => {
    expect(searchSettings('code font', false, false).map(item => item.id)).toEqual(['codeSize', 'codeFont']);
    expect(searchSettings('notifications', false, false)).toEqual([]);
    expect(searchSettings('notifications', true, false).map(item => item.id)).toEqual(['desktopNotifications']);
    expect(validateSettingsSearch({ section: 'providers', setting: 'codeSize', host: 'a'.repeat(257) })).toEqual({ section: 'providers' });
    expect(validateSettingsSearch({ section: 'appearance', setting: 'codeSize', host: 'host-a' })).toEqual({ section: 'appearance', setting: 'codeSize', host: 'host-a' });
    expect(validateSettingsSearch({ section: 'unknown', setting: 'unknown' })).toEqual({});
    expect(parseSettingsReturn({ runtimeId: 'a', rootId: '\n' })).toBeUndefined();
  });
  it('restores the exact view and location after reload and falls back when it was closed', () => {
    const f = fixture();
    const first = f.runtime.tabs.open('mac', 'a');
    f.runtime.tabs.visit('mac', 'a', { agent: 'child', panel: 'execution', view: 'repl' }, first);
    f.runtime.rememberSettingsReturn({ runtimeId: 'mac', rootId: 'a', viewId: first, search: { agent: 'child', panel: 'execution', view: 'repl' } });
    const restored = new AppRuntime(f.platform); runtimes.push(restored);
    expect(settingsBackDestination(restored)).toMatchObject({ params: { runtimeId: 'mac', rootId: 'a' }, search: { agent: 'child', panel: 'execution', view: 'repl' }, state: { whipViewId: first } });
    restored.tabs.open('remote', 'b');
    restored.tabs.closeViews([first]);
    expect(settingsBackDestination(restored)).toMatchObject({ params: { runtimeId: 'remote', rootId: 'b' } });
    restored.rememberSettingsReturn({ search: {} });
    expect(settingsBackDestination(restored)).toEqual({ to: '/', search: {}, replace: true });
  });
  it('captures a draft destination, restores it after reload, and follows in-place promotion', () => {
    const f = fixture();
    const draft = f.runtime.tabs.openNew({ runtimeId: 'mac', cwd: '/repo' });
    let listener!: (event: any) => void;
    bindSettingsNavigation(f.runtime, { subscribe: (_type: string, callback: typeof listener) => { listener = callback; return () => {}; } } as unknown as AnyRouter);
    listener({ fromLocation: { pathname: `/new/${draft.id}`, search: {}, state: {} }, toLocation: { pathname: '/settings' } });
    expect(f.runtime.settingsReturn?.viewId).toBe(draft.id);
    const restored = new AppRuntime(f.platform); runtimes.push(restored);
    expect(settingsBackDestination(restored)).toMatchObject({ to: '/new/$draftId', params: { draftId: draft.id }, replace: true });
    restored.tabs.promoteNew(draft.id, 'mac', 'created');
    expect(settingsBackDestination(restored)).toMatchObject({ to: '/h/$runtimeId/s/$rootId', params: { runtimeId: 'mac', rootId: 'created' }, state: { whipViewId: draft.id } });
  });
  it('falls back to the selected draft when the Settings return view is closed', () => {
    const f = fixture();
    const first = f.runtime.tabs.openNew();
    f.runtime.rememberSettingsReturn({ viewId: first.id, search: {} });
    const second = f.runtime.tabs.openNew();
    f.runtime.tabs.closeViews([first.id]);
    expect(settingsBackDestination(f.runtime)).toMatchObject({ to: '/new/$draftId', params: { draftId: second.id } });
  });
  it('captures once on entry, retains through category changes, and clears on successful exit', () => {
    const f = fixture(); const tab = f.runtime.tabs.open('mac', 'root');
    let listener!: (event: any) => void;
    const off = vi.fn();
    const router = { subscribe: (_type: string, callback: typeof listener) => { listener = callback; return off; } } as unknown as AnyRouter;
    const dispose = bindSettingsNavigation(f.runtime, router);
    listener({ fromLocation: { pathname: '/h/mac/s/root', search: { agent: 'child' }, state: { whipViewId: tab } }, toLocation: { pathname: '/settings' } });
    expect(f.runtime.settingsReturn).toMatchObject({ runtimeId: 'mac', rootId: 'root', search: { agent: 'child' } });
    const saved = f.data.get(settingsReturnKey);
    listener({ fromLocation: { pathname: '/settings' }, toLocation: { pathname: '/settings' } });
    expect(f.data.get(settingsReturnKey)).toBe(saved);
    listener({ fromLocation: { pathname: '/settings' }, toLocation: { pathname: '/h/mac/s/root' } });
    expect(f.runtime.settingsReturn).toBeUndefined(); expect(f.data.has(settingsReturnKey)).toBe(false);
    dispose(); expect(off).toHaveBeenCalledOnce();
  });
});
