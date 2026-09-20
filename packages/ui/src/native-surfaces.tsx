import { createContext, useCallback, useContext, useLayoutEffect, useRef, useState, type ReactNode } from 'react';

export interface NativeSurfaceHold { ready: Promise<void>; release(): void }
export type AcquireNativeSurfaceHold = () => NativeSurfaceHold;
const SurfaceContext = createContext<AcquireNativeSurfaceHold | undefined>(undefined);
/** Host-neutral: web/Storybook have no native surfaces, and retain synchronous overlay behavior. */
export function NativeSurfaceProvider({ acquire, children }: { acquire?: AcquireNativeSurfaceHold; children: ReactNode }) {
  return <SurfaceContext.Provider value={acquire}>{children}</SurfaceContext.Provider>;
}

/** Controls are never opened behind a native surface while its hide acknowledgment is pending. */
export function useNativeOverlay(open?: boolean, onOpenChange?: (open: boolean) => void) {
  const acquire = useContext(SurfaceContext);
  const [requested, setRequested] = useState(false);
  const desired = open ?? requested;
  const desiredRef = useRef(desired); desiredRef.current = desired;
  const [readyFor, setReadyFor] = useState<AcquireNativeSurfaceHold | undefined>(undefined);
  const hold = useRef<NativeSurfaceHold | undefined>(undefined);
  const visible = useRef(false);
  const release = useCallback(() => {
    hold.current?.release(); hold.current = undefined; visible.current = false; setReadyFor(undefined);
  }, [acquire]);
  const ensure = useCallback(() => {
    if (!acquire || hold.current) return;
    const current = acquire(); hold.current = current;
    void current.ready.then(() => { if (hold.current === current && desiredRef.current) setReadyFor(() => acquire); }, () => {
      // The host reports the failure; fail closed instead of exposing inaccessible approval controls.
      if (hold.current === current) setReadyFor(undefined);
    });
  }, [acquire]);
  useLayoutEffect(() => {
    if (desired) ensure();
    else if (!visible.current) release();
  }, [desired, ensure, release]);
  useLayoutEffect(() => () => { hold.current?.release(); hold.current = undefined; }, [acquire]);
  const effective = desired && (!acquire || readyFor === acquire);
  if (effective) visible.current = true;
  return {
    open: effective,
    onOpenChange(next: boolean) {
      desiredRef.current = next;
      setRequested(next); onOpenChange?.(next);
    },
    onOpenChangeComplete(next: boolean) { if (!next && !desiredRef.current) release(); },
  };
}

/** For portals whose owner retains exiting content (toast/drag); cleanup follows actual removal. */
export function useNativeSurfacePresence(present: boolean): boolean {
  const acquire = useContext(SurfaceContext);
  const [readyFor, setReadyFor] = useState<AcquireNativeSurfaceHold | undefined>(undefined);
  useLayoutEffect(() => {
    if (!present || !acquire) { setReadyFor(undefined); return; }
    let alive = true;
    const hold = acquire();
    void hold.ready.then(() => { if (alive) setReadyFor(() => acquire); }, () => { if (alive) setReadyFor(undefined); });
    return () => { alive = false; hold.release(); setReadyFor(undefined); };
  }, [present, acquire]);
  return present && (!acquire || readyFor === acquire);
}
