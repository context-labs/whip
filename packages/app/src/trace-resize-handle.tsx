import { useRef } from 'react';
import * as stylex from '@stylexjs/stylex';
import { scale, surface } from '@whip/ui/tokens.stylex';

export function TraceResizeHandle({ label, width, min, max, defaultWidth, reverse = false, onResize }: {
  label: string; width: number; min: number; max: number; defaultWidth: number;
  reverse?: boolean; onResize(width: number): void;
}) {
  const drag = useRef<{ x: number; width: number } | null>(null);
  const resize = (value: number) => onResize(Math.max(min, Math.min(max, value)));
  const direction = reverse ? -1 : 1;
  return <div role="separator" tabIndex={0} aria-label={label} aria-orientation="vertical"
    aria-valuemin={Math.round(min)} aria-valuemax={Math.round(max)} aria-valuenow={Math.round(width)} aria-valuetext={`${Math.round(width)} pixels`}
    {...stylex.props(styles.handle)} style={reverse ? { right: width - 1 } : { left: width - 1 }}
    onPointerDown={event => { if (event.button !== 0) return; event.currentTarget.setPointerCapture(event.pointerId); drag.current = { x: event.clientX, width }; }}
    onPointerMove={event => { if (drag.current) resize(drag.current.width + direction * (event.clientX - drag.current.x)); }}
    onPointerUp={event => { drag.current = null; if (event.currentTarget.hasPointerCapture(event.pointerId)) event.currentTarget.releasePointerCapture(event.pointerId); }}
    onPointerCancel={() => { if (drag.current) resize(drag.current.width); drag.current = null; }}
    onLostPointerCapture={() => { drag.current = null; }}
    onDoubleClick={() => resize(defaultWidth)}
    onKeyDown={event => {
      if (!['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(event.key)) return;
      event.preventDefault();
      resize(event.key === 'Home' ? min : event.key === 'End' ? max : width + direction * (event.key === 'ArrowRight' ? 8 : -8));
    }}>
    <span aria-hidden="true" {...stylex.props(styles.target)} />
  </div>;
}

const styles = stylex.create({
  handle: { position: 'absolute', top: 0, bottom: 0, width: 1, zIndex: 2, cursor: 'col-resize', touchAction: 'none', userSelect: 'none', backgroundColor: surface.quietBorder, outline: { default: 'none', ':focus-visible': `1px solid ${surface.secondaryText}` } },
  target: { position: 'absolute', inset: { default: -4, [scale.touch]: -9 } },
});
