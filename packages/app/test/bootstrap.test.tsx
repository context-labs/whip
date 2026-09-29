import { act } from '@testing-library/react';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { HostConnections } from '../src/hosts';
import { AppRuntime } from '../src/runtime';
import { createFallbackStorage, resolveURLConnection, urlProfile, type AppPlatform, type AppStorage } from '../src/platform';
import type { DesktopBridge, DesktopEvent } from '../src/desktop-bridge';
import { providerFixture } from './provider-fixture';
import type { HostConnection } from '../src/hosts';
import { mountApplication } from '../../../apps/web/src/bootstrap';

const renderStartup = vi.hoisted(() => vi.fn());

// Exercise the real bootstrap and draft runtime without mounting unrelated product screens.
vi.mock('@whip/app', async () => ({
  ...await import('../src/host-prompts'),
  ...await import('../src/session-tab-routing'),
  createWhipApplication(platform: AppPlatform) {
    const runtime = new AppRuntime(platform);
    return { runtime, Application: ({ startup }: { startup: Promise<unknown> }) => { renderStartup(startup); return null; }, router: { history: { push: vi.fn() } }, dispose: () => runtime.dispose() };
  },
}));

const mounted: ReturnType<typeof mountApplication>[] = [];
beforeEach(() => {
  renderStartup.mockClear();
  vi.useFakeTimers();
  vi.spyOn(HostConnections.prototype, 'connectOnLaunch').mockResolvedValue();
  document.body.innerHTML = '<div id="root"></div>';
});

it('connects once and shows the desktop window while startup is still pending', () => {
  const startup = new Promise<void>(() => {});
  vi.mocked(HostConnections.prototype.connectOnLaunch).mockReturnValue(startup);
  const f = fixture();
  expect(HostConnections.prototype.connectOnLaunch).toHaveBeenCalledOnce();
  expect(renderStartup).toHaveBeenCalledWith(expect.any(Promise));
  act(() => vi.advanceTimersByTime(40));
  expect(f.bridge.ready).toHaveBeenCalledOnce();
});
afterEach(() => {
  act(() => { for (const app of mounted.splice(0)) app.dispose(); });
  vi.useRealTimers();
});

function fixture(desktop = true) {
  const values = new Map<string, string>();
  const source: AppStorage = {
    keys: () => [...values.keys()], getItem: key => values.get(key) ?? null,
    setItem: (key, value) => { values.set(key, value); }, removeItem: key => { values.delete(key); },
  };
  const storage = createFallbackStorage(() => source, vi.fn());
  const platform: AppPlatform = {
    storage, defaultConnection: urlProfile('https://host.example'), connectionKinds: ['url'],
    resolveConnection: resolveURLConnection, openExternal: async () => {}, copy: async () => {},
    download: async () => 'saved', sessionLink: path => path,
    dispose: vi.fn(),
  };
  const listeners = new Set<(event: DesktopEvent) => void>();
  const bridge = {
    onEvent(listener: (event: DesktopEvent) => void) { listeners.add(listener); return () => { listeners.delete(listener); }; },
    replyClose: vi.fn(), ready: vi.fn(),
  };
  let app!: ReturnType<typeof mountApplication>;
  act(() => { app = mountApplication(platform, desktop ? bridge as unknown as DesktopBridge : undefined); });
  mounted.push(app);
  return { app, platform, source, storage, bridge, listeners,
    close(reason: 'quit' | 'reload' | 'update' = 'quit') {
      act(() => { for (const listener of listeners) listener({ kind: 'close-request', id: 'close', reason }); });
    },
  };
}

it('reports text-only draft loss during a desktop update when storage falls back after quota denial', () => {
  const f = fixture();
  f.app.runtime.setDraft('runtime:root:agent', 'keep this text');
  vi.spyOn(f.source, 'setItem').mockImplementation(() => { throw new Error('Quota'); });
  act(() => vi.advanceTimersByTime(150));
  expect(f.storage.persistent).toBe(false);
  f.close('update');
  expect(f.bridge.replyClose).toHaveBeenLastCalledWith('close', {
    attachments: false, error: expect.stringContaining('may lose draft changes'),
  });
  expect(f.app.runtime.draft('runtime:root:agent')).toBe('keep this text');
  expect(f.source.getItem('whip.web.draft.v1:runtime:root:agent')).toBeNull();
});

it('saves a text-only close flush when another window exceeded the draft bound, evicting that overflow', () => {
  const f = fixture();
  f.app.runtime.setDraft('runtime:root:agent', 'unsaved local changes');
  for (let index = 0; index < 33; index++)
    f.source.setItem(`whip.web.draft.v1:other:root:${index}`, 'another window');
  f.close();
  expect(f.bridge.replyClose).toHaveBeenLastCalledWith('close', { attachments: false });
  expect(f.source.getItem('whip.web.draft.v1:runtime:root:agent')).toBe('unsaved local changes');
  expect(f.source.keys().filter(key => key.startsWith('whip.web.draft.v1:'))).toHaveLength(32);
});

it('flushes durable text for a desktop close and releases its lifecycle listeners on disposal', () => {
  const f = fixture();
  f.app.runtime.setDraft('runtime:root:agent', 'durable text');
  f.close('reload');
  expect(f.bridge.replyClose).toHaveBeenLastCalledWith('close', { attachments: false });
  expect(f.source.getItem('whip.web.draft.v1:runtime:root:agent')).toBe('durable text');
  act(() => f.app.dispose());
  expect(f.listeners.size).toBe(0);
  expect(f.platform.dispose).toHaveBeenCalledOnce();
  act(() => f.app.dispose());
  expect(f.platform.dispose).toHaveBeenCalledOnce();
});

it('warns browsers for memory-only text and removes the warning after explicit clearing', () => {
  const f = fixture(false);
  f.app.runtime.setDraft('runtime:root:agent', 'keep open');
  vi.spyOn(f.source, 'setItem').mockImplementation(() => { throw new Error('Quota'); });
  act(() => vi.advanceTimersByTime(150));
  const event = new Event('beforeunload', { cancelable: true });
  window.dispatchEvent(event);
  expect(event.defaultPrevented).toBe(true);
  f.app.runtime.setDraft('runtime:root:agent', '');
  f.app.runtime.flushDrafts();
  const cleared = new Event('beforeunload', { cancelable: true });
  window.dispatchEvent(cleared);
  expect(cleared.defaultPrevented).toBe(false);
});

it('installs the browser leave warning for edits to a saved draft without replacing app state', () => {
  const f = fixture(false), key = 'runtime:root:agent';
  f.app.runtime.setDraft(key, 'saved'); f.app.runtime.flushDrafts();
  const saved = f.app.runtime.getSnapshot();
  f.app.runtime.setDraft(key, 'unsaved edit');
  expect(f.app.runtime.getSnapshot()).toBe(saved);
  const write = vi.spyOn(f.source, 'setItem').mockImplementation(() => { throw new Error('Quota'); });
  const leave = new Event('beforeunload', { cancelable: true });
  window.dispatchEvent(leave);
  expect(leave.defaultPrevented).toBe(true);
  expect(f.app.runtime.draft(key)).toBe('unsaved edit');
  expect(f.source.getItem('whip.web.draft.v1:' + key)).toBe('saved');
  write.mockRestore();
});

it('lets browsers leave after a successful final text flush and preserves the back-forward cache lifetime', () => {
  const f = fixture(false);
  f.app.runtime.setDraft('runtime:root:agent', 'final change');
  const event = new Event('beforeunload', { cancelable: true });
  window.dispatchEvent(event);
  expect(event.defaultPrevented).toBe(false);
  expect(f.source.getItem('whip.web.draft.v1:runtime:root:agent')).toBe('final change');
  const dispose = vi.spyOn(f.app.runtime, 'dispose');
  window.dispatchEvent(new PageTransitionEvent('pagehide', { persisted: true }));
  expect(dispose).not.toHaveBeenCalled();
  act(() => window.dispatchEvent(new PageTransitionEvent('pagehide', { persisted: false })));
  expect(dispose).toHaveBeenCalledOnce();
});


it('reports startup readiness only for an attached native local client with matching identity', async () => {
  const measurement = window as Window & { whipStartupMeasurement?: boolean; whipStartupSnapshot?: () => { sdkState: string; sdkConnected: boolean; tabCount: number; newDraftMatchesRoute: boolean } };
  measurement.whipStartupMeasurement = true;
  const f = fixture();
  try {
    const { client } = await providerFixture({ runtimeID: 'host' });
    const home = { id: 'local', runtimeId: 'host', local: true, state: 'connected', profile: { target: { kind: 'local' } }, client } as HostConnection;
    const current = f.app.runtime.getSnapshot();
    const snapshot = vi.spyOn(f.app.runtime, 'getSnapshot').mockReturnValue({ ...current, home });
    const attached = vi.spyOn(f.app.runtime.connections, 'isAttached').mockReturnValue(false);
    expect(measurement.whipStartupSnapshot!().sdkConnected).toBe(false);
    attached.mockReturnValue(true);
    expect(measurement.whipStartupSnapshot!()).toMatchObject({ sdkState: 'connected', sdkConnected: true });
    snapshot.mockReturnValue({ ...current, home: { ...home, runtimeId: 'another' } });
    expect(measurement.whipStartupSnapshot!().sdkConnected).toBe(false);
    snapshot.mockReturnValue({ ...current, home: { ...home, state: 'closed' } });
    expect(measurement.whipStartupSnapshot!()).toMatchObject({ sdkState: 'closed', sdkConnected: false });
    expect(Object.keys(measurement.whipStartupSnapshot!()).sort()).toEqual(['newDraftMatchesRoute', 'sdkConnected', 'sdkState', 'tabCount']);
    act(() => f.app.dispose());
    expect(measurement.whipStartupSnapshot).toBeUndefined();
  } finally { delete measurement.whipStartupMeasurement; }
});
