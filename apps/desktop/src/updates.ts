import { autoUpdater } from 'electron';
import type { EventEmitter } from 'node:events';
import type { DesktopEvent } from '@whip/app/desktop-bridge';

type UpdateEvent = Extract<DesktopEvent, { kind: 'update' }>;
export interface DesktopConfig { channel: 'stable' | 'beta'; updateURL?: string }
export function readDesktopConfig(value: unknown): DesktopConfig {
  if (!value || typeof value !== 'object') throw new Error('Invalid desktop release configuration');
  const config = value as DesktopConfig;
  if (!['stable', 'beta'].includes(config.channel)) throw new Error('Invalid desktop release channel');
  if (config.updateURL !== undefined) {
    if (typeof config.updateURL !== 'string' || config.updateURL.length > 4096) throw new Error('Invalid update feed');
    const url = new URL(config.updateURL);
    if (url.protocol !== 'https:' || url.username || url.password || url.search || url.hash || !url.pathname.endsWith('/RELEASES.json'))
      throw new Error('Updates require an HTTPS RELEASES.json feed without credentials');
  }
  return config;
}

/** Native macOS signature verification and replacement remain Squirrel's job. */
export class DesktopUpdates {
  private state?: UpdateEvent;
  private downloaded = false;
  private downloadedVersion?: string;
  private checking = false;
  private timer?: ReturnType<typeof setTimeout>;
  private scheduled = false;
  private installing = false;
  private listeners: { name: string; listener: (...args: any[]) => void }[] = [];
  constructor(private config: DesktopConfig, private emit: (event: DesktopEvent) => void,
    private requestInstall: (version: string) => Promise<void>, private installationState: (quitting: boolean) => void = () => {}) {
    if (config.updateURL) autoUpdater.setFeedURL({ url: config.updateURL, serverType: 'json' });
    this.on('checking-for-update', () => this.publish({ kind: 'update', state: 'checking' }));
    this.on('update-available', () => this.publish({ kind: 'update', state: 'available' }));
    this.on('update-not-available', () => { this.checking = false; this.publish({ kind: 'update', state: 'current' }); });
    this.on('error', error => {
      if (this.installing) { this.installing = false; this.installationState(false); }
      this.downloaded = false;
      this.checking = false; this.publish({ kind: 'update', state: 'error', error: error.message.slice(0, 2048) });
    });
    this.on('update-downloaded', (_event, _notes, releaseName) => {
      // Electron supplies updateTo.name, which Forge formats as the product
      // name followed by vVERSION. Persist only the matching channel's version.
      const prefix = config.channel === 'beta' ? 'Whip Beta v' : 'Whip v';
      const version = typeof releaseName === 'string' && releaseName.startsWith(prefix) ? releaseName.slice(prefix.length) : undefined;
      if (typeof version !== 'string' || !/^\d+\.\d+\.\d+(?:-[A-Za-z0-9.-]+)?$/.test(version) || version.length > 128) {
        this.checking = false; this.downloaded = false;
        this.publish({ kind: 'update', state: 'error', error: 'The update reported an invalid version.' }); return;
      }
      if (version.includes('-') !== (config.channel === 'beta')) {
        this.checking = false; this.downloaded = false;
        this.publish({ kind: 'update', state: 'error', error: 'The update belongs to a different release channel.' }); return;
      }
      this.downloadedVersion = version;
      this.checking = false; this.downloaded = true;
      this.publish({ kind: 'update', state: 'downloaded', version: String(version).slice(0, 128) });
    });
    // Squirrel closes windows before app.before-quit. Only the already-approved
    // quitAndInstall path may authorize that earlier close event.
    this.on('before-quit-for-update', () => { if (this.installing) this.installationState(true); });
  }
  private on(name: string, listener: (...args: any[]) => void) {
    (autoUpdater as EventEmitter).on(name, listener); this.listeners.push({ name, listener });
  }
  private publish(event: UpdateEvent) { this.state = event; this.emit(event); }
  ready() {
    if (this.state) this.emit(this.state);
    if (!this.config.updateURL || this.scheduled) return;
    this.scheduled = true;
    this.timer = setTimeout(() => { void this.check().catch(() => {}); }, 30_000);
    this.timer.unref();
  }
  async check() {
    if (!this.config.updateURL) {
      const error = 'This build has no automatic update feed. Install a published Whip release to enable updates.';
      this.publish({ kind: 'update', state: 'error', error }); throw new Error(error);
    }
    if (this.downloaded) { if (this.state) this.emit(this.state); return; }
    if (this.checking) return;
    this.checking = true;
    try { autoUpdater.checkForUpdates(); }
    catch (error) {
      this.checking = false;
      this.publish({ kind: 'update', state: 'error', error: error instanceof Error ? error.message : 'Update check failed' });
      throw error;
    }
  }
  async install() {
    if (!this.downloaded) throw new Error('No update has finished downloading');
    await this.requestInstall(this.downloadedVersion!);
  }
  quitAndInstall() {
    if (!this.downloaded) throw new Error('No update has finished downloading');
    this.installing = true;
    try { autoUpdater.quitAndInstall(); }
    catch (error) { this.installing = false; this.installationState(false); throw error; }
  }
  dispose() {
    clearTimeout(this.timer);
    for (const { name, listener } of this.listeners) (autoUpdater as EventEmitter).removeListener(name, listener);
    this.listeners = [];
  }
}
