import type { BrowserAction, BrowserPresentation, BrowserRestoreTab, BrowserTarget } from '@whip/app/desktop-bridge';

export const browserLimits = Object.freeze({ tabs: 8, restored: 32, visible: 4, urlBytes: 8192, metadataBytes: 64 << 10, title: 128 });
export function object(value: unknown, keys: readonly string[]): Record<string, unknown> {
  if (!value || typeof value !== 'object' || Array.isArray(value) || Object.keys(value).some(key => !keys.includes(key))) throw new Error('Invalid browser request');
  return value as Record<string, unknown>;
}
export function boundedString(value: unknown, max = 128): string {
  if (typeof value !== 'string' || !value || Buffer.byteLength(value, 'utf8') > max || /[\u0000-\u001f\u007f]/u.test(value)) throw new Error('Invalid browser text');
  return value;
}
export function browserID(value: unknown): string {
  const id = boundedString(value);
  if (!/^[a-zA-Z0-9_-]+$/.test(id)) throw new Error('Invalid browser identity');
  return id;
}
export function browserURL(value: unknown): string {
  let text = boundedString(value, browserLimits.urlBytes).trim();
  if (text === 'about:blank') return text;
  if (!/^[a-z][a-z\d+.-]*:/i.test(text) || /^(?:localhost|[a-z\d.-]+):\d+(?:[/?#]|$)/i.test(text)) {
    text = /^(?:localhost|127(?:\.\d+){3}|\[::1\])(?::|\/|$)/i.test(text) ? `http://${text}` : `https://${text}`;
  }
  let url: URL;
  try { url = new URL(text); } catch { throw new Error('Enter an HTTP or HTTPS address'); }
  if (!['http:', 'https:'].includes(url.protocol) || !url.hostname || url.username || url.password) throw new Error('Only HTTP, HTTPS and about:blank addresses are allowed');
  if (Buffer.byteLength(url.href, 'utf8') > browserLimits.urlBytes) throw new Error('Browser address is too long');
  return url.href;
}
export function guestResourceAllowed(value: string): boolean {
  try { return ['http:', 'https:', 'ws:', 'wss:', 'about:', 'data:', 'blob:'].includes(new URL(value).protocol); }
  catch { return false; }
}
export function browserTitle(value: string): string { return [...value.replace(/[\u0000-\u001f\u007f]/gu, '')].slice(0, browserLimits.title).join(''); }
export function target(value: unknown): BrowserTarget {
  const input = object(value, ['epoch', 'tabId', 'generation']);
  return { epoch: browserID(input.epoch), tabId: browserID(input.tabId), generation: browserID(input.generation) };
}
export function restoreTabs(value: unknown): { epoch: string; tabs: BrowserRestoreTab[] } {
  const input = object(value, ['epoch', 'tabs']);
  const epoch = browserID(input.epoch);
  if (!Array.isArray(input.tabs) || input.tabs.length > browserLimits.restored || Buffer.byteLength(JSON.stringify(input.tabs), 'utf8') > browserLimits.metadataBytes) throw new Error('Browser restore limit exceeded');
  const ids = new Set<string>();
  const tabs = input.tabs.map(value => {
    const tab = object(value, ['id', 'url', 'titleHint', 'environmentId']);
    const id = browserID(tab.id); if (ids.has(id)) throw new Error('Duplicate browser tab'); ids.add(id);
    if (tab.titleHint !== undefined && typeof tab.titleHint !== 'string') throw new Error('Invalid browser title');
    return { id, url: browserURL(tab.url), ...(tab.titleHint === undefined ? {} : { titleHint: browserTitle(tab.titleHint) }), ...(tab.environmentId === undefined ? {} : { environmentId: browserID(tab.environmentId) }) };
  });
  return { epoch, tabs };
}
export function presentation(value: unknown): BrowserPresentation {
  const input = object(value, ['epoch', 'revision', 'blocked', 'slots']);
  const epoch = browserID(input.epoch);
  if (!Number.isSafeInteger(input.revision) || (input.revision as number) < 1 || typeof input.blocked !== 'boolean' || !Array.isArray(input.slots) || input.slots.length > browserLimits.visible) throw new Error('Invalid browser presentation');
  const tabs = new Set<string>(), ids = new Set<string>();
  const slots = input.slots.map(value => {
    const slot = object(value, ['tabId', 'slotId', 'bounds']);
    const tabId = browserID(slot.tabId), slotId = browserID(slot.slotId);
    if (tabs.has(tabId) || ids.has(slotId)) throw new Error('Duplicate browser presentation');
    tabs.add(tabId); ids.add(slotId);
    const rect = object(slot.bounds, ['x', 'y', 'width', 'height']);
    if (['x', 'y', 'width', 'height'].some(key => typeof rect[key] !== 'number' || !Number.isFinite(rect[key]) || Math.abs(rect[key] as number) > 100000) || (rect.width as number) < 0 || (rect.height as number) < 0) throw new Error('Invalid browser bounds');
    return { tabId, slotId, bounds: { x: rect.x as number, y: rect.y as number, width: rect.width as number, height: rect.height as number } };
  });
  return { epoch, revision: input.revision as number, blocked: input.blocked, slots };
}
export function nativeBounds(rect: BrowserPresentation['slots'][number]['bounds'], zoom: number, width: number, height: number) {
  const left = Math.max(0, Math.min(width, Math.round(rect.x * zoom)));
  const top = Math.max(0, Math.min(height, Math.round(rect.y * zoom)));
  const right = Math.max(left, Math.min(width, Math.round((rect.x + rect.width) * zoom)));
  const bottom = Math.max(top, Math.min(height, Math.round((rect.y + rect.height) * zoom)));
  return { x: left, y: top, width: right - left, height: bottom - top };
}
export function browserAction(value: unknown): BrowserAction {
  const raw = object(value, ['kind', 'url', 'ignoreCache', 'text', 'forward', 'findNext', 'action', 'factor', 'open']);
  switch (raw.kind) {
    case 'navigate': object(raw, ['kind', 'url']); return { kind: 'navigate', url: browserURL(raw.url) };
    case 'back': case 'forward': case 'stop': case 'focus': case 'clear-profile': object(raw, ['kind']); return { kind: raw.kind };
    case 'reload': object(raw, ['kind', 'ignoreCache']); if (raw.ignoreCache !== undefined && typeof raw.ignoreCache !== 'boolean') throw new Error('Invalid reload'); return { kind: raw.kind, ignoreCache: raw.ignoreCache as boolean | undefined };
    case 'find':
      object(raw, ['kind', 'text', 'forward', 'findNext']);
      if (typeof raw.text !== 'string' || Buffer.byteLength(raw.text) > 4096 || [raw.forward, raw.findNext].some(value => value !== undefined && typeof value !== 'boolean')) throw new Error('Invalid find request');
      return { kind: raw.kind, text: raw.text, forward: raw.forward as boolean | undefined, findNext: raw.findNext as boolean | undefined };
    case 'stop-find': object(raw, ['kind', 'action']); if (!['clear', 'keep', 'activate'].includes(raw.action as string)) throw new Error('Invalid find action'); return { kind: raw.kind, action: raw.action as 'clear' | 'keep' | 'activate' };
    case 'zoom': object(raw, ['kind', 'factor']); if (typeof raw.factor !== 'number' || !Number.isFinite(raw.factor) || raw.factor < 0.25 || raw.factor > 5) throw new Error('Zoom must be between 25% and 500%'); return { kind: raw.kind, factor: raw.factor };
    case 'devtools': object(raw, ['kind', 'open']); if (typeof raw.open !== 'boolean') throw new Error('Invalid DevTools action'); return { kind: raw.kind, open: raw.open };
    default: throw new Error('Unknown browser action');
  }
}
