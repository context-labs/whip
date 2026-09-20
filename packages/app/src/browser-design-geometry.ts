import type { BrowserDesignBounds, BrowserDesignState, BrowserDesignLease, BrowserDesignRevision } from './browser-design-types';

/** Keep clipboard text within the native UTF-8 byte cap without splitting a code point. */
export function boundedDesignText(value: string, max: number): string {
  const text = value.replace(/[\u0000-\u0008\u000b\u000c\u000e-\u001f]/gu, '');
  const bytes = new TextEncoder().encode(text);
  return bytes.length <= max ? text : new TextDecoder().decode(bytes.subarray(0, max), { stream: true });
}

/** Explicit projections preserve the native IPC closed-schema boundary. */
export function designLease(value: BrowserDesignLease): BrowserDesignLease {
  return { epoch: value.epoch, tabId: value.tabId, generation: value.generation, designId: value.designId };
}
export function designRevision(value: BrowserDesignRevision): BrowserDesignRevision {
  return { ...designLease(value), documentRevision: value.documentRevision, selectionRevision: value.selectionRevision };
}
/** Animate only a target switch inside one valid geometry generation. Equal snapshots
 * preserve an in-flight transition; a same-node geometry update must snap instead. */
export function designHoverMotion(previous: BrowserDesignState | undefined, next: BrowserDesignState, wasAnimating: boolean): boolean {
  if (!previous?.hover || !next.hover || previous.status !== 'active' || next.status !== 'active'
    || !sameDesignRevision(previous, next)
    || next.hoverGeometryRevision === undefined || previous.hoverGeometryRevision !== next.hoverGeometryRevision
    || previous.viewport.width !== next.viewport.width || previous.viewport.height !== next.viewport.height) return false;
  if (previous.hover.id !== next.hover.id) return true;
  const a = previous.hover.bounds, b = next.hover.bounds;
  return wasAnimating && a.x === b.x && a.y === b.y && a.width === b.width && a.height === b.height;
}

/** Overlay-local CSS pixels. A measured composer flips above a single target, then clamps. */
export function designComposerBounds(state: Pick<BrowserDesignState, 'viewport' | 'elements'>, height: number): BrowserDesignBounds {
  const gap = 12, { width: viewportWidth, height: viewportHeight } = state.viewport;
  const width = Math.min(360, Math.max(0, viewportWidth - gap * 2));
  const actualHeight = Math.min(height, Math.max(0, viewportHeight - gap * 2));
  const element = state.elements.length === 1 ? state.elements[0] : undefined;
  let x = viewportWidth - width - gap, y = viewportHeight - actualHeight - gap;
  if (element) {
    x = element.bounds.x;
    y = element.bounds.y + element.bounds.height + gap;
    if (y + actualHeight > viewportHeight - gap) y = element.bounds.y - actualHeight - gap;
  }
  return { x: Math.max(gap, Math.min(x, viewportWidth - width - gap)), y: Math.max(gap, Math.min(y, viewportHeight - actualHeight - gap)), width, height: actualHeight };
}
/** Labels are display text only; never interpret page-supplied markup. */
export function designHoverLabel(label: string): { kind: string; description: string } {
  const normalized = label.replace(/\s+/gu, ' ').trim();
  const separator = normalized.indexOf(' · ');
  return separator < 0 ? { kind: normalized, description: '' }
    : { kind: normalized.slice(0, separator), description: normalized.slice(separator + 3) };
}

/** Prefer outside the visible target, then choose the least obstructed clamped position. */
export function designHoverBounds(target: BrowserDesignBounds, viewport: BrowserDesignState['viewport'], size: { width: number; height: number }, obstacles: BrowserDesignBounds[] = []): BrowserDesignBounds {
  const gap = 8;
  const width = Math.min(size.width, Math.max(0, viewport.width - gap * 2));
  const height = Math.min(size.height, Math.max(0, viewport.height - gap * 2));
  const left = Math.max(0, Math.min(target.x, viewport.width));
  const top = Math.max(0, Math.min(target.y, viewport.height));
  const right = Math.max(left, Math.min(target.x + target.width, viewport.width));
  const bottom = Math.max(top, Math.min(target.y + target.height, viewport.height));
  const candidates = [
    { x: left, y: top - height - gap },
    { x: left, y: bottom + gap },
    { x: right + gap, y: top },
    { x: left - width - gap, y: top },
    { x: right - width, y: top - height - gap },
    { x: right - width, y: bottom + gap },
    { x: left, y: bottom - height - gap },
    { x: right - width - gap, y: top + gap },
  ].map(point => ({
    x: Math.max(gap, Math.min(point.x, viewport.width - width - gap)),
    y: Math.max(gap, Math.min(point.y, viewport.height - height - gap)), width, height,
  }));
  const overlap = (a: BrowserDesignBounds, b: BrowserDesignBounds) =>
    Math.max(0, Math.min(a.x + a.width, b.x + b.width) - Math.max(a.x, b.x)) *
    Math.max(0, Math.min(a.y + a.height, b.y + b.height) - Math.max(a.y, b.y));
  const obstruction = (bounds: BrowserDesignBounds) => obstacles.reduce((total, obstacle) => total + overlap(bounds, obstacle), 0);
  return candidates.reduce((best, candidate) => {
    const difference = obstruction(candidate) - obstruction(best);
    return difference < 0 || (difference === 0 && overlap(candidate, target) < overlap(best, target)) ? candidate : best;
  });
}

export function sameDesignRevision(a: BrowserDesignRevision, b: BrowserDesignRevision): boolean {
  return a.epoch === b.epoch && a.tabId === b.tabId && a.generation === b.generation && a.designId === b.designId && a.documentRevision === b.documentRevision && a.selectionRevision === b.selectionRevision;
}
