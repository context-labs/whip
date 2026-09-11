/** @jest-environment node */
import { MobileWorkspace } from './workspace';
import { MobileRuntime, type SavedHost } from './runtime';
import type { MobileStorage } from './storage';
jest.mock('expo-crypto', () => ({ randomUUID: () => require('node:crypto').randomUUID() }));
const host = (id: string): SavedHost => ({ id, name: id, url: `https://${id}.example`, clientId: `phone-${id}` });
function fixture() {
  const records = new Map<string, any>(); const close = jest.fn(async () => {});
  const storage = { get: async (bucket: string, key: string) => records.get(`${bucket}/${key}`), set: async (bucket: string, key: string, value: unknown) => { records.set(`${bucket}/${key}`, value); }, delete: async (bucket: string, key: string) => { records.delete(`${bucket}/${key}`); }, list: async (bucket: string) => [...records.entries()].filter(([key]) => key.startsWith(`${bucket}/`)).map(([key, value]) => ({ key: key.slice(bucket.length + 1), value })), close } as unknown as MobileStorage;
  let gate: Promise<void> | undefined; const made: any[] = [];
  const make = () => {
    let state: any = { hosts: [], active: true }; const listeners = new Set<() => void>();
    const emit = (patch: any) => { state = { ...state, ...patch }; for (const fn of listeners) fn(); };
    const runtime = { getSnapshot: () => state, subscribe: (fn: () => void) => { listeners.add(fn); return () => listeners.delete(fn); }, start: async () => { emit({ hosts: (await storage.list<SavedHost>('hosts')).map(r => r.value) }); }, setHostProfiles: (hosts: SavedHost[]) => emit({ hosts }), setActive: jest.fn((active: boolean) => emit({ active })), report: jest.fn(), connect: async (value: SavedHost) => { emit({ host: value, connecting: true }); await gate; const verified = { ...value, runtimeId: value.runtimeId ?? `runtime-${value.id}` }; await storage.set('hosts', value.id, verified); emit({ host: verified, connecting: false, ready: true, client: {} }); }, detach: jest.fn(async () => emit({ client: undefined, ready: false, host: undefined })), dispose: jest.fn(async () => emit({ client: undefined, ready: false })), newHost: jest.fn() };
    made.push(runtime); return runtime as unknown as MobileRuntime;
  };
  const workspace = new MobileWorkspace(storage, make);
  return { workspace, records, close, made, block: (promise: Promise<void>) => { gate = promise; } };
}
test('hosts have isolated lifetimes and the shared database closes only with the device owner', async () => {
  const f = fixture(); await f.workspace.start(); const a = await f.workspace.connect(host('a')); const b = await f.workspace.connect(host('b'));
  expect(f.workspace.getSnapshot().connections).toHaveLength(2); expect(f.workspace.sessionRuntime('runtime-a')).toBe(a);
  f.workspace.setActive(false); expect(a.setActive).toHaveBeenLastCalledWith(false); expect(b.setActive).toHaveBeenLastCalledWith(false);
  await f.workspace.disconnect('a'); expect(a.dispose).toHaveBeenCalledWith(false); expect(b.dispose).not.toHaveBeenCalled(); expect(f.close).not.toHaveBeenCalled();
  expect(f.workspace.sessionRuntime('runtime-b')).toBe(b); await f.workspace.dispose(); expect(f.close).toHaveBeenCalledTimes(1);
});
test('one runtime cannot appear twice under different addresses', async () => {
  const f = fixture(); await f.workspace.start(); await f.workspace.connect({ ...host('a'), runtimeId: 'same' });
  await expect(f.workspace.connect({ ...host('b'), runtimeId: 'same' })).rejects.toThrow(/already connected/);
  expect(f.workspace.sessionRuntime('same')?.getSnapshot().host?.id).toBe('a'); await f.workspace.dispose();
});
test('disconnect cancels a late connect without affecting the healthy host', async () => {
  const f = fixture(); await f.workspace.start(); await f.workspace.connect(host('a'));
  let release!: () => void; f.block(new Promise<void>(resolve => { release = resolve; }));
  const pending = f.workspace.connect(host('b')); await Promise.resolve(); await Promise.resolve(); await f.workspace.disconnect('b'); release();
  await expect(pending).rejects.toThrow(/cancelled/); expect(f.workspace.sessionRuntime('runtime-a')).toBeDefined(); expect(f.workspace.sessionRuntime('runtime-b')).toBeUndefined(); await f.workspace.dispose();
});
