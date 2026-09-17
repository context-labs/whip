import { createContext, useContext, useEffect, useLayoutEffect, useRef, useState, type ReactNode } from 'react';
import { prefersReducedMotion } from '@whip/ui';
import * as stylex from '@stylexjs/stylex';
import { colors, surface } from '@whip/ui/tokens.stylex';

// Timings adapted from zeron/crates/ui/src/transcript.rs (MIT, Copyright 2026 Wing).
export const transcriptMotion = { row: 360, connector: 480, first: 90, stagger: 65, fold: 140, shimmer: 3400 };
export const MotionContext = createContext(false);
export interface Arrival { time: number; delay: number; consumed?: boolean }

export function useTranscriptMotion(): boolean {
  const available = () => !document.hidden && !prefersReducedMotion();
  const [enabled, setEnabled] = useState(available);
  useEffect(() => {
    const update = () => setEnabled(available());
    const media = typeof matchMedia === 'undefined' ? undefined : matchMedia('(prefers-reduced-motion: reduce)');
    const observer = new MutationObserver(update);
    observer.observe(document.documentElement, { attributes: true, attributeFilter: ['data-motion'] });
    media?.addEventListener('change', update);
    document.addEventListener('visibilitychange', update);
    return () => { observer.disconnect(); media?.removeEventListener('change', update); document.removeEventListener('visibilitychange', update); };
  }, []);
  return enabled;
}

export function RowMotion({ arrival, closing, children }: { arrival?: Arrival; closing?: boolean; children: ReactNode }) {
  const enabled = useContext(MotionContext);
  const ref = useRef<HTMLDivElement>(null);
  useLayoutEffect(() => {
    const element = ref.current;
    if (!element || !enabled || !element.animate) { if (arrival) arrival.consumed = true; return; }
    const animations: Animation[] = [];
    if (closing) {
      animations.push(element.animate([{ height: `${element.getBoundingClientRect().height}px`, overflow: 'hidden' }, { height: '0px', overflow: 'hidden' }], { duration: transcriptMotion.fold, easing: 'cubic-bezier(0,0,.58,1)', fill: 'forwards' }));
    } else if (arrival && !arrival.consumed) {
      arrival.consumed = true;
      const elapsed = performance.now() - arrival.time;
      if (elapsed < arrival.delay + transcriptMotion.connector) {
        const delay = Math.max(0, arrival.delay - elapsed);
        const height = element.getBoundingClientRect().height;
        animations.push(element.animate([{ height: '0px', overflow: 'hidden' }, { height: `${height}px`, overflow: 'hidden' }], { duration: transcriptMotion.row, delay, easing: 'cubic-bezier(.16,1,.3,1)', fill: 'backwards' }));
        const content = element.querySelector('[data-activity-content]');
        if (content) animations.push(content.animate([{ opacity: 0, transform: 'translateY(4px)' }, { opacity: 1, transform: 'translateY(0px)' }], { duration: transcriptMotion.row, delay, easing: 'cubic-bezier(.16,1,.3,1)', fill: 'backwards' }));
        for (const path of element.querySelectorAll<SVGPathElement>('[data-connector]')) {
          const length = path.getTotalLength?.() ?? 30;
          animations.push(path.animate([{ strokeDasharray: `${length}`, strokeDashoffset: `${length}` }, { strokeDasharray: `${length}`, strokeDashoffset: '0' }], { duration: transcriptMotion.connector, delay, easing: 'cubic-bezier(.22,1,.36,1)', fill: 'backwards' }));
        }
      }
    }
    return () => animations.forEach(animation => animation.cancel());
  }, [arrival, closing, enabled]);
  return <div ref={ref}>{children}</div>;
}

export function Shimmer({ active, children }: { active: boolean; children: ReactNode }) {
  const enabled = useContext(MotionContext);
  const ref = useRef<HTMLSpanElement>(null);
  useEffect(() => {
    if (!active || !enabled || !ref.current?.animate) return;
    const animation = ref.current.animate([{ backgroundPosition: '200% 0' }, { backgroundPosition: '-100% 0' }], { duration: transcriptMotion.shimmer, iterations: Infinity });
    return () => animation.cancel();
  }, [active, enabled]);
  return <span ref={ref} data-shimmer={active && enabled ? 'active' : undefined} {...stylex.props(active && enabled && styles.shimmer)}>{children}</span>;
}

const styles = stylex.create({ shimmer: { backgroundImage: `linear-gradient(90deg, ${surface.secondaryText} 30%, ${colors.foreground} 50%, ${surface.secondaryText} 70%)`, backgroundSize: '300% 100%', backgroundClip: 'text', color: 'transparent' } });

export function fadeDuration(previousGap: number, gap: number): { average: number; duration: number } {
  const average = previousGap * .7 + Math.min(Math.max(0, gap), 1000) * .3;
  return { average, duration: Math.max(120, Math.min(400, 3 * average)) };
}

/** Zeron's bounded, fixed-timestep spring, independent of render frequency. */
export function springStep(position: number, velocity: number, target: number, frames: number, growth = 0) {
  const chase = target - Math.min(32, growth * 9);
  for (let remaining = Math.min(8, Math.max(0, frames)); remaining > 0; remaining--) {
    const fraction = Math.min(1, remaining);
    velocity += fraction * ((.7 * velocity + .05 * Math.max(0, chase - position)) / 1.25 - velocity);
    position = Math.min(target, position + (velocity + growth) * fraction);
  }
  if (target - position <= .5) position = target;
  return { position, velocity };
}
