import { describe, expect, it, vi } from 'vitest';
import type { AnyRouter } from '@tanstack/react-router';
import type { Client } from '@whip/sdk';
import type { AppRuntime } from '../src/runtime';
import { SessionTabs, TAB_STORAGE_KEY, type TerminalTab } from '../src/session-tabs';
import { openTerminalTab, tabDestination } from '../src/session-tab-routing';
import { inspectTerminals, sendTerminalOpen } from '../src/terminal-open';

function fixture() {
  const disk = new Map<string, string>();
  const storage = { keys: () => [...disk.keys()], removeItem: (key: string) => { disk.delete(key); }, getItem: (key: string) => disk.get(key) ?? null, setItem: vi.fn((key: string, value: string) => { disk.set(key, value); }) };
  const tabs = new SessionTabs(storage);
  let attached = true;
  const open = vi.fn(async () => ({ id: 'opened', process_epoch: 'boot', cwd: '/work' }));
  const client = { runtimeID: 'host', processEpoch: 'boot', openTerminal: open } as unknown as Client;
  const runtime = { tabs, connections: { isAttached: () => attached, host: () => ({ client }) }, reportWorkspace: vi.fn() } as unknown as AppRuntime;
  const navigate = vi.fn(async () => {}) as unknown as AnyRouter['navigate'];
  return { runtime, client, tabs, storage, disk, open, navigate, detach: () => { attached = false; },
    run: () => openTerminalTab(runtime, navigate, { runtimeId: 'host', cwd: '/work' }) };
}

describe('terminal open delivery boundary', () => {
  it('saves the exact process marker before HTTP and persists acknowledged handles', async () => {
    const f = fixture();
    f.open.mockImplementation(async () => {
      const restored = new SessionTabs(f.storage).pendingTerminal('host');
      expect(restored).toMatchObject({ terminalId: null, processEpoch: 'boot', opening: { processEpoch: 'boot' } });
      expect(tabDestination(restored!).params).toEqual({ runtimeId: 'host', terminalId: restored!.id });
      return { id: 'opened', process_epoch: 'boot', cwd: '/work' };
    });
    const id = await f.run();
    expect(new SessionTabs(f.storage).workspace().tabs.find(tab => tab.id === id)).toMatchObject({ terminalId: 'opened', processEpoch: 'boot' });
    expect(f.tabs.pendingTerminal('host')).toBeUndefined();
    expect(f.open).toHaveBeenCalledOnce();
  });
  it('sends nothing if the pending descriptor cannot be saved', async () => {
    const f = fixture(); f.storage.setItem.mockImplementation(() => { throw new Error('storage unavailable'); });
    expect(await f.run()).toBeUndefined();
    expect(f.open).not.toHaveBeenCalled(); expect(f.tabs.workspace().tabs).toHaveLength(0);
  });
  it('retains lost replies across close/reload and routes repeated intent to inspection without another open', async () => {
    const f = fixture(); f.open.mockRejectedValue(new Error('reply lost'));
    await f.run();
    const pending = f.tabs.pendingTerminal('host')!;
    f.tabs.closeViews([pending.id]);
    expect(f.tabs.workspace().closed[0]?.tab).toMatchObject({ id: pending.id, opening: pending.opening });
    const restored = { ...f.runtime, tabs: new SessionTabs(f.storage) } as AppRuntime;
    await openTerminalTab(restored, f.navigate, { runtimeId: 'host', cwd: '/work' });
    expect(f.open).toHaveBeenCalledOnce();
    expect(restored.tabs.workspace().tabs).toHaveLength(1);
    expect(restored.tabs.pendingTerminal('host')?.id).toBe(pending.id);
  });
  it('does not evict pending recovery when later ordinary closed-tab history fills', async () => {
    const f = fixture(); const id = f.tabs.openTerminal('host', null, '/work', undefined, 'boot'); f.tabs.closeViews([id]);
    for (let index = 0; index < 30; index++) { const chat = f.tabs.openNew({ cwd: '/work' }); f.tabs.closeViews([chat.id]); }
    expect(f.tabs.workspace().closed).toHaveLength(20);
    expect(new SessionTabs(f.storage).pendingTerminal('host')?.id).toBe(id);
    expect(f.disk.get(TAB_STORAGE_KEY)!.length).toBeLessThan(64 << 10);
  });
  it('retains a known late ACK in closed recovery without reopening or navigating it', async () => {
    const f = fixture(); let resolve!: (value: { id: string; process_epoch: string; cwd: string }) => void;
    f.open.mockImplementation(() => new Promise(done => { resolve = done; }));
    const running = f.run(); await vi.waitFor(() => expect(f.open).toHaveBeenCalled());
    const pending = f.tabs.pendingTerminal('host')!; f.tabs.closeViews([pending.id]);
    resolve({ id: 'late', process_epoch: 'boot', cwd: '/work' }); await running;
    expect(f.tabs.workspace().tabs).toHaveLength(0);
    expect(f.tabs.workspace().closed[0]?.tab).toMatchObject({ terminalId: 'late' });
    expect((f.tabs.workspace().closed[0]?.tab as TerminalTab).opening).toBeUndefined();
    expect(f.navigate).toHaveBeenCalledTimes(1);
  });
  it('cannot retarget an explicitly selected shell with an obsolete open ACK', async () => {
    const f = fixture(); let resolve!: (value: { id: string; process_epoch: string; cwd: string }) => void;
    f.open.mockImplementation(() => new Promise(done => { resolve = done; }));
    const running = f.run(); await vi.waitFor(() => expect(f.open).toHaveBeenCalled());
    const pending = f.tabs.pendingTerminal('host')!;
    f.tabs.updateTerminal(pending.id, { terminalId: 'selected', processEpoch: 'boot', opening: undefined }, pending.opening!.id);
    resolve({ id: 'late', process_epoch: 'boot', cwd: '/work' }); await running;
    expect(f.tabs.workspace().tabs[0]).toMatchObject({ terminalId: 'selected' });
    expect(f.runtime.reportWorkspace).toHaveBeenCalled(); expect(f.open).toHaveBeenCalledOnce();
  });
  it('never sends a restored marker through another process or detached connection', async () => {
    const f = fixture(); const id = f.tabs.openTerminal('host', null, '/work', undefined, 'retired');
    await expect(sendTerminalOpen(f.runtime, f.client, f.tabs.workspace().tabs.find(tab => tab.id === id) as TerminalTab)).rejects.toThrow('host connection');
    const captured = f.tabs.beginTerminalOpen(id, 'boot'); f.detach();
    await expect(sendTerminalOpen(f.runtime, f.client, captured)).rejects.toThrow('host connection');
    expect(f.open).not.toHaveBeenCalled();
  });
  it('rejects foreign, repeated or oversized list evidence without opening a shell', async () => {
    const item = { id: 'one', process_epoch: 'boot', cwd: '/work' };
    for (const result of [{ process_epoch: 'other', items: [] }, { process_epoch: 'boot', items: [{ ...item, process_epoch: 'other' }] },
      { process_epoch: 'boot', items: [item, item] }, { process_epoch: 'boot', items: [{ ...item, cwd: 'x'.repeat(128 << 10) }] }]) {
      const listTerminals = vi.fn(async () => result);
      await expect(inspectTerminals({ processEpoch: 'boot', listTerminals } as unknown as Client, new AbortController().signal)).rejects.toThrow();
      expect(listTerminals).toHaveBeenCalledOnce();
    }
  });
});

it('retains the saved marker if persisting an acknowledged handle fails', async () => {
  const f = fixture();
  f.open.mockImplementation(async () => {
    f.storage.setItem.mockImplementation(() => { throw new Error('storage unavailable after dispatch'); });
    return { id: 'opened', process_epoch: 'boot', cwd: '/work' };
  });
  await f.run();
  expect(f.tabs.pendingTerminal('host')).toBeDefined();
  expect(new SessionTabs(f.storage).pendingTerminal('host')).toBeDefined();
  expect(f.runtime.reportWorkspace).toHaveBeenCalled(); expect(f.open).toHaveBeenCalledOnce();
});

it('keeps a pending view open if its closed recovery record cannot be saved', () => {
  const f = fixture(); const id = f.tabs.openTerminal('host', null, '/work', undefined, 'boot');
  f.storage.setItem.mockImplementation(() => { throw new Error('storage unavailable'); });
  expect(() => f.tabs.closeViews([id])).toThrow('storage unavailable');
  expect(f.tabs.workspace().tabs).toHaveLength(1);
  expect(new SessionTabs(f.storage).pendingTerminal('host')?.id).toBe(id);
});

it('bounds open plus closed pending descriptors and preserves recovery at capacity', () => {
  const f = fixture();
  for (let index = 0; index < 32; index++) {
    const id = f.tabs.openTerminal(`host-${index}`, null, '/work', undefined, 'boot');
    if (index < 20) f.tabs.closeViews([id]);
  }
  expect(f.tabs.canOpen()).toBe(false);
  expect(() => f.tabs.openTerminal('extra', null, '/work', undefined, 'boot')).toThrow('32');
  const pending = f.tabs.pendingTerminal('host-0')!;
  expect(f.tabs.reopenView(pending.id)).toBe(pending.id);
  expect(f.tabs.workspace().tabs).toHaveLength(13);
  expect(f.tabs.workspace().closed).toHaveLength(19);
  f.tabs.closeViews([pending.id]);
  const open = f.tabs.workspace().tabs[0]!;
  expect(() => f.tabs.closeViews([open.id])).toThrow('pending terminal opens');
  expect(new SessionTabs(f.storage).workspace().closed).toHaveLength(20);
});

it('refuses a duplicate local send while the exact original request is outstanding', async () => {
  const f = fixture(); let reject!: (error: Error) => void;
  f.open.mockImplementation(() => new Promise((_, fail) => { reject = fail; }));
  const running = f.run(); await vi.waitFor(() => expect(f.open).toHaveBeenCalled());
  const pending = f.tabs.pendingTerminal('host')!;
  await expect(sendTerminalOpen(f.runtime, f.client, pending)).rejects.toThrow('outstanding terminal open');
  reject(new Error('reply lost')); await running;
  expect(f.open).toHaveBeenCalledOnce();
});

it('preserves an ACK that arrives while a multi-tab close is still awaiting another shell', () => {
  const f = fixture(); const id = f.tabs.openTerminal('host', null, '/work', undefined, 'boot');
  const pending = f.tabs.pendingTerminal('host')!;
  f.tabs.updateTerminal(id, { terminalId: 'late', opening: undefined }, pending.opening!.id);
  f.tabs.closeViews([id], id, [id]);
  expect(f.tabs.workspace().closed[0]?.tab).toMatchObject({ id, terminalId: 'late', processEpoch: 'boot' });
  expect(new SessionTabs(f.storage).reopenView(id)).toBe(id);
});

it.each(['detached', 'other-process'])('does not publish an acknowledged handle from a %s connection', async kind => {
  const f = fixture(); let resolve!: (value: { id: string; process_epoch: string; cwd: string }) => void;
  f.open.mockImplementation(() => new Promise(done => { resolve = done; }));
  const running = f.run(); await vi.waitFor(() => expect(f.open).toHaveBeenCalled());
  if (kind === 'detached') f.detach();
  resolve({ id: 'late', process_epoch: kind === 'other-process' ? 'other' : 'boot', cwd: '/work' }); await running;
  expect(f.tabs.pendingTerminal('host')).toMatchObject({ terminalId: null, opening: { processEpoch: 'boot' } });
  expect(f.open).toHaveBeenCalledOnce(); expect(f.runtime.reportWorkspace).toHaveBeenCalled();
});
