// Bounded evidence from the disposable Electron acceptance process, never product telemetry.
import { app, type BrowserWindow, type WebContents } from 'electron';
import { writeFileSync } from 'node:fs';
import type { BrowserManager } from '../src/browser-manager';

export function browserNativeDiagnostics() {
  const started = performance.now();
  const events: object[] = [];
  let step: object = { name: 'startup' };
  const record = (value: object) => {
    if (events.length === 128) events.shift();
    events.push({ elapsedMs: Math.round(performance.now() - started), ...value });
  };
  const address = (value: string) => value.slice(0, 256);
  const label = (value: unknown) => typeof value === 'string' ? value.slice(0, 128) : undefined;
  const created = (_event: Electron.Event, contents: WebContents) => {
    const id = contents.id;
    const loadURL = contents.loadURL.bind(contents);
    // Return Electron's original promise unchanged; retain only its bounded failure code.
    contents.loadURL = (...args) => {
      record({ kind: 'load', id, url: address(args[0]) });
      const promise = loadURL(...args);
      void promise.catch((error: unknown) => {
        const details = error as { code?: unknown; errno?: unknown };
        record({ kind: 'load-rejected', id, url: address(args[0]),
          code: typeof details?.code === 'string' ? details.code.slice(0, 64) : undefined,
          errno: typeof details?.errno === 'number' ? details.errno : undefined });
      });
      return promise;
    };
    contents.on('did-start-navigation', (_event, url, inPlace, main) => {
      if (main) record({ kind: 'navigation-start', id, url: address(url), inPlace });
    });
    contents.on('did-navigate', (_event, url) => record({ kind: 'navigation-commit', id, url: address(url) }));
    contents.on('did-fail-load', (_event, code, description, url, main) => {
      if (main) record({ kind: 'navigation-failed', id, code, description: description.slice(0, 64), url: address(url) });
    });
    contents.on('did-stop-loading', () => record({ kind: 'load-stopped', id }));
    contents.on('render-process-gone', (_event, details) => record({ kind: 'process-gone', id, reason: details.reason }));
    contents.once('destroyed', () => record({ kind: 'destroyed', id }));
  };
  app.on('web-contents-created', created);
  return {
    step(name: string, value?: unknown) {
      const input = value && typeof value === 'object' ? value as Record<string, unknown> : {};
      const action = input.action && typeof input.action === 'object' ? input.action as Record<string, unknown> : {};
      step = { name: name.slice(0, 256), tabId: label(input.tabId), generation: label(input.generation), action: label(action.kind) };
    },
    failure(error: unknown, window?: BrowserWindow, manager?: BrowserManager) {
      const live = window && !window.isDestroyed();
      let inventory: object | null = null;
      try {
        const state = manager?.snapshot();
        if (state) inventory = { epoch: state.epoch, revision: state.revision, tabs: state.tabs.slice(0, 32).map(tab => ({
          id: tab.id, generation: tab.generation, documentGeneration: tab.documentGeneration, status: tab.status,
          url: address(tab.url), pendingURL: tab.pendingURL && address(tab.pendingURL), loading: tab.loading, errorCode: tab.error?.code,
        })) };
      } catch { /* The failed step may already have disposed its manager. */ }
      const evidence = { pid: process.pid, directory: process.env.BROWSER_NATIVE_DIRECTORY, step, error: String(error).slice(0, 512), events,
        window: live ? { visible: window.isVisible(), focused: window.isFocused(), bounds: window.getBounds(),
          guests: window.contentView.children.slice(0, 8).map(view => ({ visible: view.getVisible(), bounds: view.getBounds(),
            ...('webContents' in view ? { id: (view.webContents as WebContents).id } : {}) })) } : null,
        inventory };
      const encoded = JSON.stringify(evidence, null, 2);
      console.error('NATIVE_BROWSER_FAILURE', encoded);
      if (process.env.BROWSER_NATIVE_FAILURE_FILE) writeFileSync(process.env.BROWSER_NATIVE_FAILURE_FILE, encoded);
    },
  };
}
