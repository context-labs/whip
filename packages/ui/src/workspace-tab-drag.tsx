import * as stylex from '@stylexjs/stylex';
import { useLayoutEffect, useRef, useState } from 'react';
import type { ReactNode } from 'react';
import { createPortal } from 'react-dom';
import type { DragDropEventHandlers } from '@dnd-kit/react';
import { isSortable } from '@dnd-kit/react/sortable';
import type { DragDropManager } from '@dnd-kit/dom';
import type { WorkspaceDrop } from './workspace-layout';
import { styles } from './workspace-tabs.stylex';

export type Point = { x: number; y: number };
export type TabRect = { left: number; top: number; width: number; height: number };
export type TabDrop = { drop: WorkspaceDrop; rect: TabRect; insertion: boolean; strip?: HTMLElement };
export const containsPoint = (rect: TabRect, point: Point) => point.x >= rect.left && point.x <= rect.left + rect.width && point.y >= rect.top && point.y <= rect.top + rect.height;

// offset geometry excludes our visual displacements, preventing animated tabs
// from moving the collision thresholds back and forth under a stationary pointer.
export function tabRect(tab: HTMLElement): TabRect {
  const strip = tab.parentElement!;
  const rect = strip.getBoundingClientRect();
  const zoom = rect.width / strip.offsetWidth || 1;
  return { left: rect.left + (tab.offsetLeft - strip.scrollLeft) * zoom, top: rect.top + tab.offsetTop * zoom,
    width: parseFloat(getComputedStyle(tab).width) * zoom, height: tab.offsetHeight * zoom };
}

export function tabDrop(strip: HTMLElement, viewId: string, point: Point): TabDrop {
  const tabs = [...strip.querySelectorAll<HTMLElement>('[data-workspace-tab]')];
  const rtl = getComputedStyle(strip).direction === 'rtl';
  const target = tabs.findIndex(tab => { const box = tabRect(tab); return rtl ? point.x > box.left + box.width / 2 : point.x < box.left + box.width / 2; });
  const index = target < 0 ? tabs.length : target;
  const box = tabs[index] ?? tabs.at(-1);
  const rect = box ? tabRect(box) : strip.getBoundingClientRect();
  const bounds = strip.getBoundingClientRect();
  const left = rtl ? index < tabs.length ? rect.left + rect.width - 3 : Math.max(rect.left, bounds.left)
    : index < tabs.length ? rect.left : Math.min(rect.left + rect.width, bounds.right - 3);
  return { drop: { viewId, paneId: strip.dataset.workspaceTabStrip ?? '', index }, strip, insertion: true,
    rect: { left, top: rect.top + 4, width: 3, height: rect.height - 8 } };
}

type Drag = {
  id: string; source: HTMLElement; strip: HTMLElement; rect: TabRect; offset: Point; point: Point;
  face: ReactNode; direction: 'rtl' | 'ltr'; manager: DragDropManager; target: TabDrop | null;
};

/** One visual-only drag path for standalone tabs and the split workspace. */
export function useWorkspaceTabDrag({ locate, onDrop, onPreview }: {
  locate(id: string, point: Point): TabDrop | null;
  onDrop(drop: WorkspaceDrop): void;
  onPreview?(target: TabDrop | null): void;
}) {
  const callbacks = useRef({ locate, onDrop, onPreview });
  callbacks.current = { locate, onDrop, onPreview };
  const active = useRef<Drag | null>(null);
  const [presentation, setPresentation] = useState<Drag | null>(null);
  const overlay = useRef<HTMLDivElement>(null);
  const frame = useRef(0);
  const offsets = useRef(new Map<HTMLElement, { x: number; animation?: Animation }>());
  const settling = useRef<Animation | null>(null);
  const finishFrame = useRef(0);
  const hidden = useRef<HTMLElement | null>(null);
  const clickCleanup = useRef<(() => void) | null>(null);
  const clickTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);

  function resetOffsets() {
    for (const [element, value] of offsets.current) {
      value.animation?.cancel(); element.style.removeProperty('transform');
    }
    offsets.current.clear();
  }

  function shift(element: HTMLElement, x: number, zoom: number) {
    const previous = offsets.current.get(element);
    if (previous?.x === x || (!previous && x === 0)) return;
    const from = getComputedStyle(element).transform;
    previous?.animation?.cancel();
    const transform = `translateX(${x / zoom}px)`;
    element.style.transform = transform;
    const animation = element.animate([{ transform: from }, { transform }], {
      duration: matchMedia('(prefers-reduced-motion: reduce)').matches ? 0 : 100, easing: 'ease-out',
    });
    offsets.current.set(element, { x, animation });
  }

  function showGap(drag: Drag, target: TabDrop | null) {
    const used = new Set<HTMLElement>();
    const strips = new Set([drag.strip, target?.strip].filter((strip): strip is HTMLElement => !!strip));
    for (const strip of strips) {
      const nodes = [...strip.querySelectorAll<HTMLElement>('[data-workspace-tab]')];
      if (!nodes.length) continue;
      const bounds = strip.getBoundingClientRect();
      const zoom = bounds.width / strip.offsetWidth || 1;
      const rtl = getComputedStyle(strip).direction === 'rtl';
      const gap = parseFloat(getComputedStyle(strip).columnGap) * zoom || 0;
      const original = nodes.map(element => ({ element, rect: tabRect(element) }));
      const sequence: { element?: HTMLElement; rect: TabRect }[] = [];
      for (let index = 0; index <= original.length; index++) {
        if (target?.strip === strip && target.drop.index === index) sequence.push({ rect: drag.rect });
        const item = original[index];
        if (item && (item.element !== drag.source || !target?.strip)) sequence.push(item);
      }
      let cursor = rtl ? original[0]!.rect.left + original[0]!.rect.width : original[0]!.rect.left;
      for (const item of sequence) {
        const left = rtl ? cursor - item.rect.width : cursor;
        if (item.element) { used.add(item.element); shift(item.element, left - item.rect.left, zoom); }
        else if (target) target.rect.left = Math.max(bounds.left, Math.min(left, bounds.right - target.rect.width));
        cursor += (rtl ? -1 : 1) * (item.rect.width + gap);
      }
    }
    for (const element of offsets.current.keys()) if (!used.has(element)) shift(element, 0, 1);
  }

  function paint() {
    const drag = active.current, element = overlay.current;
    if (!drag) return;
    if (!drag.source.isConnected) { drag.manager.actions.stop({ canceled: true }); return; }
    const target = callbacks.current.locate(drag.id, drag.point);
    const previous = drag.target;
    showGap(drag, target);
    drag.target = target;
    if (JSON.stringify(previous?.drop) !== JSON.stringify(target?.drop)
      || JSON.stringify(previous?.rect) !== JSON.stringify(target?.rect)) callbacks.current.onPreview?.(target);
    if (element) {
      const bounds = drag.strip.getBoundingClientRect();
      const inside = drag.point.y >= bounds.top && drag.point.y <= bounds.bottom;
      const left = drag.point.x - drag.offset.x;
      // Outside the source row, release the vertical constraint continuously.
      const beyond = drag.point.y < bounds.top ? drag.point.y - bounds.top : drag.point.y > bounds.bottom ? drag.point.y - bounds.bottom : 0;
      const releasedTop = drag.point.y - drag.offset.y;
      const blend = Math.min(1, Math.abs(beyond) / 16);
      const top = drag.rect.top + (releasedTop - drag.rect.top) * blend;
      const zoom = element.getBoundingClientRect().width / element.offsetWidth || 1;
      element.style.transform = `translate(${left / zoom}px, ${(inside ? tabRect(drag.source).top : top) / zoom}px)`;
    }
    frame.current = requestAnimationFrame(paint);
  }

  function releaseHidden() {
    hidden.current?.removeAttribute('data-tab-settling'); hidden.current = null;
  }

  function end(canceled: boolean) {
    const drag = active.current;
    if (!drag) return;
    cancelAnimationFrame(frame.current);
    // Re-evaluate at release, including any final auto-scroll or product limit change.
    const target = canceled ? null : callbacks.current.locate(drag.id, drag.point);
    active.current = null;
    clickTimer.current = setTimeout(() => clickCleanup.current?.(), 400);
    resetOffsets(); callbacks.current.onPreview?.(null);
    drag.source.setAttribute('data-tab-settling', ''); hidden.current = drag.source;
    if (target) callbacks.current.onDrop(target.drop);
    finishFrame.current = requestAnimationFrame(() => {
      const element = overlay.current;
      const destination = [...drag.source.ownerDocument.querySelectorAll<HTMLElement>('[data-workspace-tab]')].find(tab => tab.dataset.workspaceTab === drag.id);
      releaseHidden();
      if (!element || !destination) { setPresentation(null); return; }
      destination.setAttribute('data-tab-settling', ''); hidden.current = destination;
      const rect = destination.getBoundingClientRect();
      const zoom = element.getBoundingClientRect().width / element.offsetWidth || 1;
      const animation = element.animate([{ transform: element.style.transform }, { transform: `translate(${rect.left / zoom}px, ${rect.top / zoom}px)` }], {
        duration: matchMedia('(prefers-reduced-motion: reduce)').matches ? 0 : 100, easing: 'ease-out', fill: 'forwards',
      });
      settling.current = animation;
      void animation.finished.catch(() => {}).then(() => {
        if (settling.current !== animation) return;
        settling.current = null; releaseHidden(); setPresentation(null);
      });
    });
  }

  const handlers: Partial<DragDropEventHandlers> = {
    onDragStart(event, manager) {
      const source = event.operation.source;
      if (!isSortable(source) || !(source.element instanceof HTMLElement)) return;
      manager.dragOperation.shape = source.sortable.refreshShape() ?? null;
      cancelAnimationFrame(finishFrame.current); settling.current?.cancel(); settling.current = null; releaseHidden();
      const rect = source.element.getBoundingClientRect();
      const initial = event.operation.position.initial;
      const drag: Drag = { id: String(source.id), source: source.element, strip: source.element.parentElement!, rect,
        offset: { x: initial.x - rect.left, y: initial.y - rect.top }, point: event.nativeEvent instanceof PointerEvent ? { x: event.nativeEvent.clientX, y: event.nativeEvent.clientY } : event.operation.position.current,
        face: source.data.tabFace as ReactNode, direction: getComputedStyle(source.element).direction === 'rtl' ? 'rtl' : 'ltr', manager, target: null };
      manager.dragOperation.position.current = drag.point;
      active.current = drag; setPresentation(drag);
      clearTimeout(clickTimer.current); clickCleanup.current?.();
      const doc = source.element.ownerDocument;
      const block = (click: MouseEvent) => { if (click.detail > 0) { click.preventDefault(); click.stopImmediatePropagation(); } };
      const remove = () => { doc.removeEventListener('click', block, true); doc.removeEventListener('pointerdown', remove, true); };
      doc.addEventListener('click', block, true); doc.addEventListener('pointerdown', remove, { capture: true, once: true });
      clickCleanup.current = remove;
      frame.current = requestAnimationFrame(paint);
    },
    onDragMove(event, manager) {
      if (!event.to || !active.current) return;
      event.preventDefault(); manager.dragOperation.position.current = event.to; manager.collisionObserver.forceUpdate();
      active.current.point = event.to;
    },
    onDragEnd(event) {
      if (active.current && event.nativeEvent instanceof PointerEvent) active.current.point = { x: event.nativeEvent.clientX, y: event.nativeEvent.clientY };
      end(event.canceled);
    },
  };

  useLayoutEffect(() => {
    const cancel = () => active.current?.manager.actions.stop({ canceled: true });
    const captureLost = (event: PointerEvent) => { if (event.buttons) cancel(); };
    window.addEventListener('blur', cancel);
    window.addEventListener('pointercancel', cancel);
    window.addEventListener('lostpointercapture', captureLost);
    return () => {
      cancel(); cancelAnimationFrame(frame.current); cancelAnimationFrame(finishFrame.current);
      settling.current?.cancel(); settling.current = null; resetOffsets(); releaseHidden(); clearTimeout(clickTimer.current); clickCleanup.current?.();
      window.removeEventListener('blur', cancel); window.removeEventListener('pointercancel', cancel); window.removeEventListener('lostpointercapture', captureLost);
    };
  }, []);
  useLayoutEffect(() => {
    if (!presentation || !overlay.current) return;
    const element = overlay.current;
    const zoom = element.getBoundingClientRect().width / element.offsetWidth || 1;
    element.style.width = `${presentation.rect.width / zoom}px`; element.style.height = `${presentation.rect.height / zoom}px`;
    element.style.transform = `translate(${(presentation.point.x - presentation.offset.x) / zoom}px, ${presentation.rect.top / zoom}px)`;
  }, [presentation]);

  return { handlers, preview: presentation && createPortal(
    <div ref={overlay} dir={presentation.direction} aria-hidden="true" inert data-workspace-drag-preview={presentation.id} {...stylex.props(styles.item, styles.preview)}
      style={{ width: presentation.rect.width, height: presentation.rect.height }}>{presentation.face}</div>, presentation.source.ownerDocument.body),
  };
}
