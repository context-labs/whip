import { useEffect, useState } from 'react';
import * as stylex from '@stylexjs/stylex';
import { scale } from './tokens.stylex';

const breathe = stylex.keyframes({
  '0%, 100%': { opacity: 0.35 },
  '50%': { opacity: 0.85 },
});
const styles = stylex.create({
  cluster: { width: 16, height: 16, flexShrink: 0, display: 'flex', alignItems: 'center', gap: 2 },
  dot: { width: 3, height: 3, borderRadius: '50%', backgroundColor: 'currentColor', opacity: 0.65 },
  moving: { animationName: { default: breathe, [scale.reducedMotion]: 'none' }, animationDuration: '2.4s', animationIterationCount: 'infinite', animationTimingFunction: 'ease-in-out' },
  second: { animationDelay: '0.3s' },
  third: { animationDelay: '0.6s' },
});

/** Decorative only: the caller supplies a readable status, never an animated label. */
export function ActivityIndicator({ active, reduceMotion = false }: { active: boolean; reduceMotion?: boolean }) {
  const [visible, setVisible] = useState(() => typeof document === 'undefined' || !document.hidden);
  const [ready, setReady] = useState(false);
  useEffect(() => {
    const changed = () => setVisible(!document.hidden);
    document.addEventListener('visibilitychange', changed);
    return () => document.removeEventListener('visibilitychange', changed);
  }, []);
  useEffect(() => {
    setReady(false);
    if (!active || !visible || reduceMotion) return;
    const timer = setTimeout(() => setReady(true), 1000);
    return () => clearTimeout(timer);
  }, [active, visible, reduceMotion]);
  const moving = active && visible && ready && !reduceMotion;
  return <span aria-hidden="true" data-activity-animation={moving ? 'running' : 'static'} {...stylex.props(styles.cluster)}>
    <span {...stylex.props(styles.dot, moving && styles.moving)} />
    <span {...stylex.props(styles.dot, moving && styles.moving, styles.second)} />
    <span {...stylex.props(styles.dot, moving && styles.moving, styles.third)} />
  </span>;
}
