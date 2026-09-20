/** Phase 0 executable contract sketch, not production browser implementation. */
export interface BrowserDescriptor {
  readonly id: string;
  readonly kind: 'browser';
  readonly url: string;
  readonly titleHint: string;
  /** Native-owned preview environment reference; absent means This Mac. Not a grant. */
  readonly environmentId?: string;
}

export function parseBrowserDescriptor(value: unknown): BrowserDescriptor | undefined {
  if (!value || typeof value !== 'object') return;
  const item = value as Record<string, unknown>;
  const identity = (value: unknown): value is string => typeof value === 'string' && /^[a-zA-Z0-9_-]{1,160}$/.test(value);
  if (item.kind !== 'browser' || !identity(item.id) || typeof item.url !== 'string'
    || new TextEncoder().encode(item.url).byteLength > 8192 || /[\0\r\n]/.test(item.url)
    || (item.environmentId !== undefined && !identity(item.environmentId))) return;
  let url: URL;
  try { url = new URL(item.url); } catch { return; }
  if (!(url.protocol === 'https:' || url.protocol === 'http:' || item.url === 'about:blank') || url.username || url.password
    || new TextEncoder().encode(url.href).byteLength > 8192) return;
  // Allowlist serialization: live native IDs, generations, grants and controller state never survive reload.
  return { id: item.id, kind: 'browser', url: url.href,
    titleHint: typeof item.titleHint === 'string' ? [...item.titleHint.replace(/[\0\r\n]/g, ' ')].slice(0, 128).join('') : '',
    ...(item.environmentId === undefined ? {} : { environmentId: item.environmentId as string }) };
}

export interface Rect { x: number; y: number; width: number; height: number }
/** Main-side geometry sketch. DOM coordinates are viewport CSS px; Electron content bounds are DIPs. */
export function browserDIPBounds(rect: Rect, zoom: number, content: { width: number; height: number }): Rect | undefined {
  if (![rect.x, rect.y, rect.width, rect.height, zoom, content.width, content.height].every(Number.isFinite)
    || zoom <= 0 || rect.width <= 0 || rect.height <= 0 || content.width <= 0 || content.height <= 0) return;
  // Round edges inward: no guest pixel may cover toolbar, adjacent pane or native-window chrome.
  const x = Math.max(0, Math.ceil(rect.x * zoom)), y = Math.max(0, Math.ceil(rect.y * zoom));
  const right = Math.min(Math.floor(content.width), Math.floor((rect.x + rect.width) * zoom));
  const bottom = Math.min(Math.floor(content.height), Math.floor((rect.y + rect.height) * zoom));
  if (right <= x || bottom <= y) return;
  return { x, y, width: right - x, height: bottom - y };
}

export interface OverlayHold { ready: Promise<void>; release(): void }
/** Token ownership, not a mutable counter: duplicate and stale cleanup cannot release another overlay. */
export function createOverlayHolds(present: (blocked: boolean, revision: number) => Promise<void>) {
  const tokens = new Set<symbol>();
  let revision = 0;
  let hidden: Promise<void> = Promise.resolve();
  return {
    get size() { return tokens.size; },
    acquire(): OverlayHold {
      if (tokens.size >= 128) throw new Error('Too many overlays');
      const token = Symbol();
      tokens.add(token);
      if (tokens.size === 1) hidden = present(true, ++revision);
      return { ready: hidden, release() {
        if (!tokens.delete(token) || tokens.size) return;
        // Production adapter owns reporting transport errors and fail-closed native presentation.
        void present(false, ++revision).catch(() => {});
      } };
    },
  };
}

/** Complete-snapshot receiver sketch. A native-issued epoch prevents late renderer work from resurfacing guests. */
export function presentationReceiver(epoch: string) {
  let revision = -1;
  let visible: readonly string[] = [];
  return {
    get visible() { return visible; },
    apply(input: { epoch: string; revision: number; blocked: boolean; slots: readonly string[] }) {
      if (input.epoch !== epoch || !Number.isSafeInteger(input.revision) || input.revision <= revision) return false;
      if (input.slots.length > 4 || new Set(input.slots).size !== input.slots.length) return false;
      revision = input.revision;
      visible = input.blocked ? [] : [...input.slots];
      return true;
    },
  };
}
