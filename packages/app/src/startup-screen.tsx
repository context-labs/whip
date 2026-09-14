import { useEffect, useState, useSyncExternalStore, type ReactNode } from 'react';
import * as stylex from '@stylexjs/stylex';
import { useTheme, VisuallyHidden, WhipcodeWordmark } from '@whip/ui';
import { colors } from '@whip/ui/tokens.stylex';

// Match HALO's StartupTransition: 220ms fades, 420ms logo rise, and a 6px content entrance.
const fadeIn = stylex.keyframes({ from: { opacity: 0 }, to: { opacity: 1 } });
const fadeOut = stylex.keyframes({ from: { opacity: 1 }, to: { opacity: 0, transform: 'scale(0.992)' } });
const logoIn = stylex.keyframes({ from: { transform: 'translateY(8px) scale(0.985)' }, to: { transform: 'translateY(0) scale(1)' } });
const contentIn = stylex.keyframes({ from: { opacity: 0, transform: 'translateY(6px)' }, to: { opacity: 1, transform: 'translateY(0)' } });
const styles = stylex.create({
  shell: { minHeight: '100dvh', backgroundColor: colors.background },
  content: { minHeight: '100dvh' },
  pending: { visibility: 'hidden' },
  entering: { animationName: contentIn, animationDuration: '220ms', animationTimingFunction: 'cubic-bezier(0.16, 1, 0.3, 1)', animationFillMode: 'both' },
  splash: {
    position: 'fixed', inset: 0, zIndex: 50, display: 'grid', placeItems: 'center',
    backgroundColor: colors.background,
    backgroundImage: `linear-gradient(180deg, color-mix(in srgb, ${colors.background} 82%, ${colors.panel} 18%), ${colors.background})`,
    animationName: fadeIn, animationDuration: '220ms', animationTimingFunction: 'ease-out', animationFillMode: 'both',
  },
  exiting: { animationName: fadeOut, animationTimingFunction: 'ease-in' },
  logo: {
    width: 'clamp(260px, 52vw, 420px)', maxWidth: 'calc(100vw - 48px)', height: 'auto', aspectRatio: '2008 / 395',
    animationName: logoIn, animationDuration: '420ms', animationDelay: '80ms',
    animationTimingFunction: 'cubic-bezier(0.16, 1, 0.3, 1)', animationFillMode: 'both',
  },
  still: { animationName: 'none' },
});

function subscribeMotion(changed: () => void) {
  const query = window.matchMedia?.('(prefers-reduced-motion: reduce)');
  query?.addEventListener('change', changed);
  return () => query?.removeEventListener('change', changed);
}
const readMotion = () => window.matchMedia?.('(prefers-reduced-motion: reduce)').matches ?? false;
type Phase = 'pending' | 'exiting' | 'entering' | 'visible';

/** One startup per mounted application; reconnects and route changes do not replay it. */
export function StartupScreen({ startup, children }: { startup: Promise<unknown>; children: ReactNode }) {
  const { display } = useTheme();
  const systemReduced = useSyncExternalStore(subscribeMotion, readMotion, () => false);
  const reduced = display.motion === 'reduce' || systemReduced;
  const [ready, setReady] = useState(false);
  const [phase, setPhase] = useState<Phase>('pending');
  useEffect(() => {
    let active = true;
    const reveal = () => { if (active) setReady(true); };
    // A slow or offline host must never hide connection recovery or settings indefinitely.
    const timeout = setTimeout(reveal, 3000);
    void startup.then(reveal, reveal);
    return () => { active = false; clearTimeout(timeout); };
  }, [startup]);
  useEffect(() => {
    if (!ready) return;
    if (reduced) { setPhase('visible'); return; }
    if (phase === 'pending') { setPhase('exiting'); return; }
    if (phase === 'visible') return;
    const timer = setTimeout(() => setPhase(phase === 'exiting' ? 'entering' : 'visible'), phase === 'exiting' ? 220 : 260);
    return () => clearTimeout(timer);
  }, [ready, reduced, phase]);
  const covered = phase === 'pending' || phase === 'exiting';
  return <div {...stylex.props(styles.shell)} data-startup-phase={phase}>
    <div inert={covered} aria-hidden={covered || undefined} {...stylex.props(styles.content, covered && styles.pending, phase === 'entering' && styles.entering, reduced && styles.still)}>
      {children}
    </div>
    {covered && <div role="status" {...stylex.props(styles.splash, phase === 'exiting' && styles.exiting, reduced && styles.still)}>
      <WhipcodeWordmark aria-hidden="true" xstyle={[styles.logo, reduced && styles.still]} />
      <VisuallyHidden>Loading whipcode…</VisuallyHidden>
    </div>}
  </div>;
}
