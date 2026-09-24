import { useEffect, useRef, useState, type RefObject } from 'react';
import * as stylex from '@stylexjs/stylex';
import { scale, surface } from '@whip/ui/tokens.stylex';
import { useRuntime } from './context';
import { emptySidebarState, readSidebarState, sidebarMaxWidth, sidebarStorageKey, sidebarWidth } from './sidebar-state';

export function useSidebarLayout() {
  const runtime = useRuntime();
  const readFailed = useRef(false);
  const [state, setState] = useState(() => {
    try { return readSidebarState(runtime.platform.windowStorage?.getItem(sidebarStorageKey) ?? null); }
    catch { readFailed.current = true; return emptySidebarState(); }
  });
  const failed = useRef(false);
  useEffect(() => {
    if (failed.current) return;
    try {
      if (readFailed.current) throw new Error('Window storage could not be read');
      runtime.platform.windowStorage?.setItem(sidebarStorageKey, JSON.stringify({ version: 1, ...state })); }
    catch { failed.current = true; runtime.report('Sidebar preferences are kept in memory because window storage is unavailable.'); }
  }, [runtime, state]);
  const [viewport, setViewport] = useState(() => window.innerWidth);
  useEffect(() => { const resize = () => setViewport(window.innerWidth); window.addEventListener('resize', resize); return () => window.removeEventListener('resize', resize); }, []);
  return { state, setState, width: sidebarWidth(state.width, viewport), maxWidth: sidebarMaxWidth(viewport) };
}

export function SidebarResize({ width, maxWidth, onResize, onHide, toggleRef }: {
  width: number; maxWidth: number; onResize(width: number): void; onHide(): void; toggleRef: RefObject<HTMLButtonElement | null>;
}) {
  const drag = useRef<{ x: number; width: number } | null>(null);
  const resize = (value: number) => onResize(Math.max(256, Math.min(maxWidth, value)));
  return <div role="separator" tabIndex={0} aria-label="Resize session navigation" aria-orientation="vertical"
    aria-controls="whip-session-navigation" aria-valuemin={256} aria-valuemax={maxWidth} aria-valuenow={Math.round(width)} aria-valuetext={`${Math.round(width)} pixels`}
    {...stylex.props(styles.handle)}
    onPointerDown={event => { if (event.button !== 0) return; event.preventDefault(); event.currentTarget.focus(); event.currentTarget.setPointerCapture(event.pointerId); drag.current = { x: event.clientX, width }; }}
    onPointerMove={event => { if (drag.current) resize(drag.current.width + event.clientX - drag.current.x); }}
    onPointerUp={event => { drag.current = null; if (event.currentTarget.hasPointerCapture(event.pointerId)) event.currentTarget.releasePointerCapture(event.pointerId); }}
    onPointerCancel={() => { if (drag.current) resize(drag.current.width); drag.current = null; }}
    onLostPointerCapture={() => { drag.current = null; }}
    onDoubleClick={() => resize(320)}
    onKeyDown={event => {
      if (!['ArrowLeft', 'ArrowRight', 'Home', 'End', 'Enter'].includes(event.key)) return;
      event.preventDefault();
      if (event.key === 'Enter') { onHide(); toggleRef.current?.focus(); }
      else resize(event.key === 'Home' ? 256 : event.key === 'End' ? maxWidth : width + (event.key === 'ArrowRight' ? 8 : -8));
    }}>
    <span aria-hidden="true" {...stylex.props(styles.handleTarget)} />
  </div>;
}
const styles = stylex.create({
  handle: { position: 'absolute', top: 0, bottom: 0, right: -1, width: 1, zIndex: 2, cursor: 'col-resize', touchAction: 'none', backgroundColor: surface.quietBorder, outline: { default: 'none', ':focus-visible': `1px solid ${surface.secondaryText}` }, outlineOffset: -2 },
  handleTarget: { position: 'absolute', inset: { default: -4, [scale.touch]: -9 } },
});
