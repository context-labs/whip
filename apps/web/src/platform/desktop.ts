import { localProfile, resolveURLConnection, type AppPlatform, type AppUpdateSnapshot, type ConnectionProfile } from '@whip/app/platform';
import type { DesktopBridge } from '@whip/app/desktop-bridge';
import type { TransportFactory } from '@whip/sdk';
import { browserStorage } from './storage';

const frameLimit = 1 << 20;
const queueLimit = 8 << 20;
const encoder = new TextEncoder();

function nativeFailure(error: unknown): never {
  if (error instanceof Error) {
    const message = error.message.replace(/^Error invoking remote method 'whip:[^']+': (?:Error: )?/, '');
    if (message !== error.message) throw new Error(message, { cause: error });
  }
  throw error;
}

function abortable<T>(promise: Promise<T>, signal: AbortSignal): Promise<T> {
  return new Promise((resolve, reject) => {
    const abort = () => reject(signal.reason ?? new Error('Connection cancelled'));
    const cleanup = () => signal.removeEventListener('abort', abort);
    promise.then(value => { cleanup(); resolve(value); }, error => { cleanup(); reject(error); });
    if (signal.aborted) abort(); else signal.addEventListener('abort', abort, { once: true });
  });
}

/** A single SDK connection over the host's existing Unix transport. */
export function desktopTransport(bridge: DesktopBridge, connectionId: string): TransportFactory {
  return async (handlers, signal) => {
    signal.throwIfAborted();
    const id = crypto.randomUUID();
    let closed = false;
    let opening = true;
    let failure: Error | undefined;
    let sequence = 0;
    let received = 0;
    let nativeBuffered = 0;
    let pendingBytes = 0;
    const pending = new Map<number, number>();
    let unsubscribe = () => {};
    const close = (error: Error) => {
      if (closed) return;
      closed = true; failure = error;
      signal.removeEventListener('abort', abort);
      unsubscribe(); pending.clear(); pendingBytes = nativeBuffered = 0;
      bridge.closeTransport(id);
      if (!opening) handlers.close(error);
    };
    const abort = () => close(new Error('Connection cancelled'));
    unsubscribe = bridge.onEvent(event => {
      if (!('id' in event) || event.id !== id || closed) return;
      if (event.kind === 'closed') close(new Error(event.error));
      else if (event.kind === 'frame') {
        if (event.sequence !== received + 1 || encoder.encode(event.frame).length > frameLimit) {
          close(new Error('Invalid desktop transport frame')); return;
        }
        received = event.sequence;
        try { handlers.message(event.frame); }
        catch { close(new Error('Invalid daemon response')); return; }
        if (!closed) bridge.acknowledgeTransport(id, event.sequence);
      } else if (event.kind === 'sent') {
        const bytes = pending.get(event.sequence);
        if (bytes === undefined || !Number.isSafeInteger(event.buffered) || event.buffered < 0 || event.buffered > queueLimit) {
          close(new Error('Invalid desktop transport acknowledgement')); return;
        }
        pending.delete(event.sequence); pendingBytes -= bytes; nativeBuffered = event.buffered;
      } else if (event.kind === 'buffered') {
        if (!Number.isSafeInteger(event.bytes) || event.bytes < 0 || event.bytes > queueLimit) {
          close(new Error('Invalid desktop transport queue')); return;
        }
        nativeBuffered = event.bytes;
      }
    });
    signal.addEventListener('abort', abort, { once: true });
    try {
      await abortable(bridge.openTransport(id, connectionId), signal);
      if (closed || signal.aborted) throw failure ?? new Error('Connection cancelled');
      opening = false;
      return {
        kind: 'unix',
        get bufferedAmount() { return pendingBytes + nativeBuffered; },
        send(frame) {
          if (closed) throw failure ?? new Error('Connection closed');
          const bytes = encoder.encode(frame).length + 1;
          if (bytes > frameLimit || pendingBytes + nativeBuffered + bytes > queueLimit || pending.size >= 1024)
            throw new Error('Desktop transport outbound limit reached');
          pending.set(++sequence, bytes); pendingBytes += bytes;
          try { bridge.sendTransport(id, sequence, frame); }
          catch (error) { close(error instanceof Error ? error : new Error('Could not send desktop message')); throw error; }
        },
        close() { close(new Error('Connection closed')); },
      };
    } catch (error) {
      close(error instanceof Error ? error : new Error('Desktop connection failed'));
      throw error;
    }
  };
}

export function createDesktopPlatform(bridge: DesktopBridge, unavailable: () => void): AppPlatform {
  let disposed = false;
  const prepared = new Map<string, { id: string; urlSource?: ConnectionProfile }>();
  let notificationsEnabled = false;
  let update: AppUpdateSnapshot = Object.freeze({ state: 'idle' });
  const updateListeners = new Set<() => void>();
  let systemContrast: boolean | undefined;
  let contrastEventReceived = false;
  const contrastListeners = new Set<() => void>();
  const receiveContrast = (value: unknown) => {
    if (disposed || typeof value !== 'boolean' || value === systemContrast) return;
    systemContrast = value;
    for (const listener of contrastListeners) listener();
  };
  void bridge.getSystemContrast().then(value => { if (!contrastEventReceived) receiveContrast(value); }).catch(() => {
    // Browser media queries remain available if this optional OS read fails.
  });
  const unsubscribeUpdates = bridge.onEvent(event => {
    if (disposed) return;
    if (event.kind === 'system-contrast') {
      if (typeof event.highContrast === 'boolean') { contrastEventReceived = true; receiveContrast(event.highContrast); }
      return;
    }
    if (event.kind !== 'update') return;
    if (event.state === update.state && event.version === update.version && event.error === update.error) return;
    update = Object.freeze({ state: event.state, ...(event.version ? { version: event.version } : {}), ...(event.error ? { error: event.error } : {}) });
    for (const listener of updateListeners) listener();
  });
  return {
    systemContrast: {
      getSnapshot: () => systemContrast,
      subscribe(listener) { contrastListeners.add(listener); return () => { contrastListeners.delete(listener); }; },
    },
    storage: browserStorage(() => window.localStorage, unavailable),
    windowStorage: browserStorage(() => window.localStorage, unavailable, 'whip.desktop.window.main.'),
    ...(bridge.chrome === 'inset' ? { chrome: 'inset' as const } : {}),
    defaultConnection: localProfile,
    connectionKinds: bridge.connectionKinds,
    async resolveConnection(profile, options) {
      options.signal.throwIfAborted();
      const id = crypto.randomUUID();
      let released = false;
      const unsubscribe = bridge.onEvent(event => {
        if (event.kind === 'progress' && event.attemptId === id && !released) options.onProgress(event.message);
      });
      const dispose = () => {
        if (released) return;
        released = true;
        if (prepared.get(profile.id)?.id === id) prepared.delete(profile.id);
        unsubscribe(); options.signal.removeEventListener('abort', dispose);
        bridge.releaseConnection(id);
      };
      options.signal.addEventListener('abort', dispose, { once: true });
      try {
        if (profile.target.kind === 'url') {
          const resolved = await resolveURLConnection(profile, options);
          options.signal.throwIfAborted();
          prepared.set(profile.id, { id, urlSource: profile });
          return { endpoint: resolved.endpoint, dispose };
        }
        await abortable(bridge.prepareConnection(id, profile), options.signal);
        options.signal.throwIfAborted();
        prepared.set(profile.id, { id });
        return { endpoint: desktopTransport(bridge, id), dispose };
      } catch (error) { dispose(); nativeFailure(error); }
    },
    openExternal: url => bridge.openExternal(url),
    copy: text => bridge.copy(text),
    pickDirectory: () => bridge.pickDirectory(),
    projectEditors: {
      async list() {
        if (disposed) throw new Error('The desktop application has closed');
        return bridge.listProjectEditors();
      },
      async open(request) {
        if (disposed) throw new Error('The desktop application has closed');
        const source = prepared.get(request.connectionId);
        if (!source) throw new Error('The source host is disconnected. Reconnect it before opening this folder.');
        return bridge.openProject({ ...request, connectionId: source.id }, source.urlSource);
      },
    },
    localRuntime: {
      ...(bridge.installDefaultLocalRuntime ? { async installDefault() {
        if (disposed) throw new Error('The desktop application has closed');
        return bridge.installDefaultLocalRuntime!().catch(nativeFailure);
      } } : {}),
      async test() {
        if (disposed) throw new Error('The desktop application has closed');
        return bridge.testLocalRuntime().catch(nativeFailure);
      },
      async choose() {
        if (disposed) throw new Error('The desktop application has closed');
        return bridge.chooseLocalRuntime().catch(nativeFailure);
      },
      async install() {
        if (disposed) throw new Error('The desktop application has closed');
        return bridge.installLocalRuntime().catch(nativeFailure);
      },
      async restart() {
        if (disposed) throw new Error('The desktop application has closed');
        return bridge.restartLocalRuntime().catch(nativeFailure);
      },
    },
    async notify(notification) {
      if (disposed) return;
      await bridge.notify(notification);
    },
    setNotificationsEnabled(enabled) {
      if (disposed || enabled === notificationsEnabled) return;
      notificationsEnabled = enabled; bridge.setNotificationsEnabled(enabled);
    },
    onCloseTab(listener) {
      if (disposed) return () => {};
      return bridge.onEvent(event => { if (!disposed && event.kind === 'close-tab') listener(); });
    },
    hideWindow() { if (!disposed) bridge.hideWindow(); },
    updates: {
      currentVersion: bridge.appVersion,
      getSnapshot: () => update,
      subscribe(listener) {
        if (disposed) return () => {};
        updateListeners.add(listener); return () => { updateListeners.delete(listener); };
      },
      async check() {
        if (disposed) throw new Error('The desktop application has closed');
        await bridge.checkForUpdates();
      },
      async install() {
        if (disposed) throw new Error('The desktop application has closed');
        await bridge.installUpdate();
      },
    },
    dispose() {
      if (disposed) return;
      if (notificationsEnabled) bridge.setNotificationsEnabled(false);
      disposed = true; unsubscribeUpdates(); updateListeners.clear(); contrastListeners.clear();
      prepared.clear();
    },
    async download(bytes, filename, mediaType) {
      if (bytes.byteLength > 64 << 20) throw new Error('Downloads are limited to 64 MiB');
      const id = await bridge.beginSave(filename, mediaType, bytes.byteLength);
      if (!id) return 'cancelled';
      try {
        for (let offset = 0; offset < bytes.byteLength; offset += 256 << 10)
          await bridge.writeSave(id, offset, bytes.slice(offset, offset + (256 << 10)));
        await bridge.finishSave(id);
        return 'saved';
      } catch (error) { await bridge.cancelSave(id).catch(() => {}); throw error; }
    },
    sessionLink(path, profile) {
      if (profile?.target.kind === 'url') {
        const base = new URL(profile.target.endpoint);
        base.protocol = base.protocol === 'wss:' ? 'https:' : base.protocol === 'ws:' ? 'http:' : base.protocol;
        return new URL(path, base.origin).href;
      }
      const route = new URL(path, 'https://whip.invalid');
      const match = /^\/h\/([^/]+)\/s\/([^/]+)$/.exec(route.pathname);
      if (!match) throw new Error('Invalid session link');
      return `${bridge.sessionScheme ?? 'whip'}://session/${match[1]}/${match[2]}${route.search}`;
    },
  };
}
