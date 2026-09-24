import { useEffect, useState } from 'react';
import * as stylex from '@stylexjs/stylex';
import { colors, scale } from './tokens.stylex';

const breathe = stylex.keyframes({
  '0%, 100%': { opacity: 0.35 },
  '50%': { opacity: 0.85 },
});
// Matrix timing/phase pattern adapted from Zeron (MIT, Copyright 2026 Wing).
const wave = stylex.keyframes({ '0%, 100%': { opacity: 0.25 }, '50%': { opacity: 1 } });
const styles = stylex.create({
  cluster: { width: 16, height: 16, flexShrink: 0, display: 'flex', alignItems: 'center', gap: 2 },
  dot: { width: 3, height: 3, borderRadius: '50%', backgroundColor: 'currentColor', opacity: 0.65 },
  moving: { animationName: { default: breathe, [scale.reducedMotion]: 'none' }, animationDuration: '2.4s', animationIterationCount: 'infinite', animationTimingFunction: 'ease-in-out' },
  second: { animationDelay: '0.3s' },
  third: { animationDelay: '0.6s' },
  matrix: { display: 'grid', gridTemplateColumns: 'repeat(3, 2.5px)', gap: 1.25, alignContent: 'center', justifyContent: 'center' },
  cell: { width: 2.5, height: 2.5 },
  wave: { animationName: { default: wave, [scale.reducedMotion]: 'none' }, animationDuration: '750ms', animationIterationCount: 'infinite', animationTimingFunction: 'ease-in-out' },
  phase1: { animationDelay: '-187.5ms' },
  phase2: { animationDelay: '-375ms' },
  phase3: { animationDelay: '-562.5ms' },
  top: { color: colors.primary },
  middle: { color: `color-mix(in srgb, ${colors.primary} 50%, currentColor)` },
});

/** Decorative only: the caller supplies a readable status, never an animated label. */
export function ActivityIndicator({ active, reduceMotion = false, variant = 'dots' }: { active: boolean; reduceMotion?: boolean; variant?: 'dots' | 'matrix' }) {
  const [visible, setVisible] = useState(() => typeof document === 'undefined' || !document.hidden);
  const [ready, setReady] = useState(false);
  useEffect(() => {
    const changed = () => setVisible(!document.hidden);
    document.addEventListener('visibilitychange', changed);
    return () => document.removeEventListener('visibilitychange', changed);
  }, []);
  useEffect(() => {
    setReady(false);
    if (!active || !visible || reduceMotion || variant === 'matrix') return;
    const timer = setTimeout(() => setReady(true), 1000);
    return () => clearTimeout(timer);
  }, [active, visible, reduceMotion, variant]);
  const moving = active && visible && (ready || variant === 'matrix') && !reduceMotion;
  if (variant === 'matrix') return <span aria-hidden="true" data-activity-animation={moving ? 'running' : 'static'} {...stylex.props(styles.cluster, styles.matrix)}>
    {[3, 2, 3, 2, 1, 2, 1, 0, 1].map((phase, index) => <span key={index} {...stylex.props(styles.dot, styles.cell, index < 3 ? styles.top : index < 6 && styles.middle,
      moving && styles.wave, phase === 1 && styles.phase1, phase === 2 && styles.phase2, phase === 3 && styles.phase3)} />)}
  </span>;
  return <span aria-hidden="true" data-activity-animation={moving ? 'running' : 'static'} {...stylex.props(styles.cluster)}>
    <span {...stylex.props(styles.dot, moving && styles.moving)} />
    <span {...stylex.props(styles.dot, moving && styles.moving, styles.second)} />
    <span {...stylex.props(styles.dot, moving && styles.moving, styles.third)} />
  </span>;
}
