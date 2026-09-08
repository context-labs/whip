import { afterEach, expect, it, vi } from 'vitest';
import type { DesktopBridge, DesktopEvent } from '../src/desktop-bridge';
import { createDesktopPlatform, desktopTransport } from '../../../apps/web/src/platform/desktop';
import { localProfile } from '../src/connections';

function fixture() {
  const listeners = new Set<(event: DesktopEvent) => void>();
  const bridge = {
    version: 1, appVersion: '1.2.3', connectionKinds: ['local', 'url', 'ssh'],
    onEvent(listener: (event: DesktopEvent) => void) { listeners.add(listener); return () => { listeners.delete(listener); }; },
    openTransport: vi.fn(async (_id: string, _connectionId: string) => {}),
    sendTransport: vi.fn(), closeTransport: vi.fn(), acknowledgeTransport: vi.fn(),
    prepareConnection: vi.fn(async () => {}), releaseConnection: vi.fn(),
    beginSave: vi.fn(async () => 'save'), writeSave: vi.fn(async () => {}),
    finishSave: vi.fn(async () => {}), cancelSave: vi.fn(async () => {}),
    checkForUpdates: vi.fn(async () => {}), installUpdate: vi.fn(async () => {}),
    pickDirectory: vi.fn(async () => '/native/project'),
    notify: vi.fn(async () => {}),
    setNotificationsEnabled: vi.fn(), hideWindow: vi.fn(),
  };
  return { bridge, api: bridge as unknown as DesktopBridge, listeners,
    emit(event: DesktopEvent) { for (const listener of listeners) listener(event); } };
}
afterEach(() => { localStorage.clear(); sessionStorage.clear(); });

it('preserves frame order, bounded backpressure and listener lifetime across the bridge', async () => {
  const f = fixture();
  const handlers = { message: vi.fn(), close: vi.fn() };
  const transport = await desktopTransport(f.api, 'connection')(handlers, new AbortController().signal);
  const id = f.bridge.openTransport.mock.calls[0]![0];
  transport.send('hello');
  expect(transport.kind).toBe('unix');
  expect(transport.bufferedAmount).toBe(6);
  f.emit({ kind: 'sent', id, sequence: 1, buffered: 4 });
  expect(transport.bufferedAmount).toBe(4);
  f.emit({ kind: 'buffered', id, bytes: 0 });
  expect(transport.bufferedAmount).toBe(0);
  f.emit({ kind: 'frame', id, sequence: 1, frame: 'response' });
  expect(handlers.message).toHaveBeenCalledWith('response');
  expect(f.bridge.acknowledgeTransport).toHaveBeenCalledWith(id, 1);
  expect(() => transport.send('x'.repeat(1 << 20))).toThrow('limit');
  f.emit({ kind: 'frame', id, sequence: 3, frame: 'out of order' });
  expect(handlers.close).toHaveBeenCalledOnce();
  transport.close();
  expect(f.listeners.size).toBe(0);
  expect(f.bridge.closeTransport).toHaveBeenCalledOnce();
});

it('cancels unresolved host discovery immediately and releases a late native attempt', async () => {
  const f = fixture();
  let finish!: () => void;
  f.bridge.prepareConnection.mockImplementation(() => new Promise(resolve => { finish = resolve; }));
  const platform = createDesktopPlatform(f.api, vi.fn());
  const controller = new AbortController();
  const pending = platform.resolveConnection(localProfile, { signal: controller.signal, onProgress() {} });
  controller.abort();
  await expect(pending).rejects.toThrow();
  expect(f.bridge.releaseConnection).toHaveBeenCalledOnce();
  expect(f.listeners.size).toBe(1); // The platform's update observer remains until application disposal.
  platform.dispose?.();
  expect(f.listeners.size).toBe(0);
  finish();
  await Promise.resolve();
  expect(f.bridge.releaseConnection).toHaveBeenCalledOnce();
});

it('cancels transport opening without waiting for a native response', async () => {
  const f = fixture();
  f.bridge.openTransport.mockImplementation(() => new Promise(() => {}));
  const controller = new AbortController();
  const pending = desktopTransport(f.api, 'connection')({ message() {}, close() {} }, controller.signal);
  controller.abort();
  await expect(pending).rejects.toThrow();
  expect(f.bridge.closeTransport).toHaveBeenCalledOnce();
  expect(f.listeners.size).toBe(0);
});

it('restores desktop window storage separately from device preferences', () => {
  const f = fixture();
  const first = createDesktopPlatform(f.api, vi.fn());
  first.storage.setItem('theme', 'paper');
  first.windowStorage!.setItem('tabs', 'retained');
  const second = createDesktopPlatform(f.api, vi.fn());
  expect(second.windowStorage!.keys()).toEqual(['tabs']);
  expect(second.windowStorage!.getItem('tabs')).toBe('retained');
  second.windowStorage!.removeItem('tabs');
  expect(second.storage.getItem('theme')).toBe('paper');
});

it('uses bounded save chunks and cancels failed writes', async () => {
  const f = fixture();
  const platform = createDesktopPlatform(f.api, vi.fn());
  expect(await platform.download(new Uint8Array(600_000), 'content', 'application/octet-stream')).toBe('saved');
  expect(f.bridge.writeSave).toHaveBeenCalledTimes(3);
  expect(f.bridge.finishSave).toHaveBeenCalledOnce();
  f.bridge.writeSave.mockRejectedValueOnce(new Error('Disk full'));
  await expect(platform.download(new Uint8Array(1), 'content', 'application/octet-stream')).rejects.toThrow('Disk full');
  expect(f.bridge.cancelSave).toHaveBeenCalledOnce();
});

it('copies navigational session links without exposing the asset origin or SSH configuration', () => {
  const platform = createDesktopPlatform(fixture().api, vi.fn());
  expect(platform.sessionLink('/h/runtime/s/root?agent=child', localProfile)).toBe('whip://session/runtime/root?agent=child');
  expect(() => platform.sessionLink('/settings', localProfile)).toThrow();
  const beta = createDesktopPlatform({ ...fixture().api, sessionScheme: 'whip-beta' }, vi.fn());
  expect(beta.sessionLink('/h/runtime/s/root', localProfile)).toBe('whip-beta://session/runtime/root');
});

it('retains immutable native update state and forwards explicit actions without starting a check', async () => {
  const f = fixture(); const platform = createDesktopPlatform(f.api, vi.fn()); const updates = platform.updates!;
  const idle = updates.getSnapshot();
  expect(idle).toEqual({ state: 'idle' }); expect(updates.getSnapshot()).toBe(idle);
  expect(updates.currentVersion).toBe('1.2.3'); expect(f.bridge.checkForUpdates).not.toHaveBeenCalled();
  const changed = vi.fn(); const unsubscribe = updates.subscribe(changed);
  f.emit({ kind: 'attention-wakeup' }); expect(changed).not.toHaveBeenCalled();
  f.emit({ kind: 'update', state: 'downloaded', version: '1.2.4' });
  const downloaded = updates.getSnapshot();
  expect(downloaded).toEqual({ state: 'downloaded', version: '1.2.4' }); expect(Object.isFrozen(downloaded)).toBe(true);
  expect(idle).toEqual({ state: 'idle' }); expect(changed).toHaveBeenCalledOnce();
  f.emit({ kind: 'update', state: 'downloaded', version: '1.2.4' }); expect(changed).toHaveBeenCalledOnce();
  await updates.check(); await updates.install();
  expect(f.bridge.checkForUpdates).toHaveBeenCalledOnce(); expect(f.bridge.installUpdate).toHaveBeenCalledOnce();
  expect(await platform.pickDirectory!()).toBe('/native/project'); expect(f.bridge.pickDirectory).toHaveBeenCalledOnce();
  const message = { id: 'notification-id', title: 'Session title', body: '1 question awaiting your response.', path: '/h/runtime/s/root' };
  await platform.notify!(message); expect(f.bridge.notify).toHaveBeenCalledExactlyOnceWith(message);
  platform.setNotificationsEnabled!(true); platform.setNotificationsEnabled!(true);
  expect(f.bridge.setNotificationsEnabled).toHaveBeenCalledExactlyOnceWith(true);
  const closeTab = vi.fn(); const offClose = platform.onCloseTab!(closeTab);
  f.emit({ kind: 'close-tab' }); expect(closeTab).toHaveBeenCalledOnce();
  platform.hideWindow!(); expect(f.bridge.hideWindow).toHaveBeenCalledOnce();
  offClose(); f.emit({ kind: 'close-tab' }); expect(closeTab).toHaveBeenCalledOnce();
  unsubscribe(); platform.dispose?.(); platform.dispose?.();
  expect(f.listeners.size).toBe(0);
  expect(f.bridge.setNotificationsEnabled.mock.calls).toEqual([[true], [false]]);
  f.emit({ kind: 'update', state: 'error', error: 'late result' }); expect(updates.getSnapshot()).toBe(downloaded);
  await expect(updates.check()).rejects.toThrow('closed'); await expect(updates.install()).rejects.toThrow('closed');
  await platform.notify!(message); expect(f.bridge.notify).toHaveBeenCalledOnce();
});
