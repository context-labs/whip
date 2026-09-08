import { clipboard, dialog, Notification, shell, type BrowserWindow } from 'electron';
import { randomUUID } from 'node:crypto';
import { open, rename, rm, type FileHandle } from 'node:fs/promises';
import path from 'node:path';
import type { DesktopBridge, DesktopEvent } from '@whip/app/desktop-bridge';
import { validHandle } from './transport';

function text(value: unknown, limit: number): asserts value is string {
  if (typeof value !== 'string' || value.length > limit || value.includes('\0')) throw new Error('Invalid desktop text');
}
export function externalURL(value: unknown): string {
  text(value, 8192);
  const url = new URL(value);
  if (!['https:', 'http:', 'mailto:'].includes(url.protocol) || url.username || url.password)
    throw new Error('This external link is not supported');
  return url.href;
}
export function navigationPath(value: unknown): asserts value is string {
  text(value, 8192);
  if (!/^\/h\/[^/]+\/s\/[^/?#]+(?:\?[^#]*)?$/.test(value) || /[\u0000-\u0020\u007f\\]/.test(value))
    throw new Error('Invalid session link');
}

export class NativeEffects {
  private saves = new Map<string, { file: FileHandle; temporary: string; target: string; size: number; offset: number; busy: boolean; operation?: Promise<void> }>();
  private savingDialog = false;
  private epoch = 0;
  private notifications = new Map<string, Notification>();
  private notificationTimes = new Map<string, number>();
  constructor(private window: BrowserWindow, private emit: (event: DesktopEvent) => void) {}

  async copy(value: unknown) { text(value, 8 << 20); clipboard.writeText(value); }
  async openExternal(value: unknown) { await shell.openExternal(externalURL(value)); }
  async pickDirectory() {
    const result = await dialog.showOpenDialog(this.window, { properties: ['openDirectory'], title: 'Choose a local project directory' });
    return result.canceled ? undefined : result.filePaths[0];
  }
  async beginSave(filename: unknown, mediaType: unknown, size: unknown) {
    text(filename, 256); text(mediaType, 256);
    if (!Number.isSafeInteger(size) || (size as number) < 0 || (size as number) > 64 << 20) throw new Error('Save exceeds the 64 MiB limit');
    if (this.savingDialog || this.saves.size >= 2) throw new Error('Finish the current save first');
    this.savingDialog = true;
    const epoch = this.epoch;
    try {
      const result = await dialog.showSaveDialog(this.window, { defaultPath: path.basename(filename).replace(/[\u0000-\u001f\u007f]/g, '_') || 'download' });
      if (result.canceled || !result.filePath || this.window.isDestroyed() || epoch !== this.epoch) return undefined;
      const id = randomUUID();
      const temporary = path.join(path.dirname(result.filePath), `.whip-save-${id}`);
      const file = await open(temporary, 'wx', 0o600);
      if (epoch !== this.epoch || this.window.isDestroyed()) {
        await file.close(); await rm(temporary, { force: true }); return undefined;
      }
      this.saves.set(id, { file, temporary, target: result.filePath, size: size as number, offset: 0, busy: false });
      return id;
    } finally { this.savingDialog = false; }
  }
  async writeSave(id: string, offset: number, bytes: Uint8Array) {
    validHandle(id);
    const save = this.saves.get(id);
    if (!save || save.busy || !(bytes instanceof Uint8Array) || bytes.byteLength > 256 << 10 || offset !== save.offset ||
        offset + bytes.byteLength > save.size) throw new Error('Invalid save chunk');
    save.busy = true;
    save.operation = (async () => {
      let written = 0;
      while (written < bytes.byteLength) {
        const result = await save.file.write(bytes, written, bytes.byteLength - written, offset + written);
        if (!result.bytesWritten) throw new Error('Could not write the selected file');
        written += result.bytesWritten;
      }
      save.offset += written;
    })();
    try { await save.operation; } finally { save.busy = false; }
  }
  async finishSave(id: string) {
    validHandle(id);
    const save = this.saves.get(id);
    if (!save || save.busy || save.offset !== save.size) throw new Error('Save is incomplete');
    save.busy = true;
    save.operation = (async () => {
      await save.file.sync(); await save.file.close();
      if (this.saves.get(id) !== save) throw new Error('Save was cancelled');
      await rename(save.temporary, save.target); this.saves.delete(id);
    })();
    try { await save.operation; } catch (error) { save.busy = false; throw error; }
  }
  async cancelSave(id: string) {
    validHandle(id);
    const save = this.saves.get(id);
    if (!save) return;
    if (save.busy) throw new Error('Save is still writing');
    this.saves.delete(id);
    await save.file.close().catch(() => {}); await rm(save.temporary, { force: true });
  }
  async notify(options: Parameters<DesktopBridge['notify']>[0]) {
    if (!options || typeof options !== 'object') throw new Error('Invalid notification');
    validHandle(options.id); text(options.title, 256); text(options.body, 1024); navigationPath(options.path);
    if (this.window.isFocused() || !Notification.isSupported() || Date.now() - (this.notificationTimes.get(options.id) ?? 0) < 1000) return;
    this.notificationTimes.delete(options.id);
    this.notificationTimes.set(options.id, Date.now());
    if (this.notificationTimes.size > 64) this.notificationTimes.delete(this.notificationTimes.keys().next().value!);
    this.notifications.get(options.id)?.close();
    if (this.notifications.size >= 32) {
      const oldest = this.notifications.keys().next().value!;
      this.notifications.get(oldest)?.close(); this.notifications.delete(oldest);
    }
    const notification = new Notification({ title: options.title, body: options.body });
    this.notifications.set(options.id, notification);
    notification.once('click', () => {
      if (!this.window.isDestroyed()) { this.window.show(); this.window.focus(); this.emit({ kind: 'navigate', path: options.path }); }
    });
    notification.once('close', () => { if (this.notifications.get(options.id) === notification) this.notifications.delete(options.id); });
    notification.show();
  }
  async dispose() {
    ++this.epoch;
    for (const notification of this.notifications.values()) notification.close();
    this.notifications.clear(); this.notificationTimes.clear();
    const saves = [...this.saves.values()]; this.saves.clear();
    for (const save of saves) {
      await save.operation?.catch(() => {});
      await save.file.close().catch(() => {}); await rm(save.temporary, { force: true }).catch(() => {});
    }
  }
}
