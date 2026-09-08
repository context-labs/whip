import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { createHostPrompts, createSessionNavigator, createWhipApplication, HostPrompts } from '@whip/app';
import { initializeTheme } from '@whip/ui/themes';
import type { AppPlatform } from '@whip/app/platform';
import type { DesktopBridge } from '@whip/app/desktop-bridge';
import '@whip/ui/fonts.css';
import '@whip/ui/reset.css';

/** Both hosts mount the same application and share its observation lifetime. */
export function mountApplication(platform: AppPlatform, desktop?: DesktopBridge) {
  initializeTheme({ storage: platform.storage });
  const application = createWhipApplication(platform);
  const element = document.getElementById('root');
  if (!element) throw new Error('The application root is missing');
  const root = createRoot(element);
  const prompts = desktop ? createHostPrompts(desktop) : undefined;
  const sessionNavigator = createSessionNavigator(application.runtime, path => application.router.history.push(path), () => application.router.state.location);
  root.render(<StrictMode><application.Application>{prompts && <HostPrompts prompts={prompts} />}</application.Application></StrictMode>);
  const warnBeforeUnload = (event: BeforeUnloadEvent) => {
    const drafts = application.runtime.flushDrafts();
    if (application.runtime.compositions.hasAttachments() || !drafts.saved) {
      event.preventDefault(); event.returnValue = '';
    }
  };
  const updateLeaveWarning = () => {
    window.removeEventListener('beforeunload', warnBeforeUnload);
    if (!desktop && (application.runtime.compositions.hasAttachments() || application.runtime.hasUnsavedDrafts()))
      window.addEventListener('beforeunload', warnBeforeUnload);
  };
  const unsubscribeCompositions = application.runtime.compositions.subscribe(updateLeaveWarning);
  const unsubscribeRuntime = application.runtime.subscribe(updateLeaveWarning);
  updateLeaveWarning();
  const unsubscribeDesktop = desktop?.onEvent(event => {
    if (event.kind === 'close-request') {
      let error: string | undefined;
      try { error = application.runtime.flushDrafts().error; }
      catch (value) { error = value instanceof Error ? value.message : String(value); }
      desktop.replyClose(event.id, { attachments: application.runtime.compositions.hasAttachments(), ...(error ? { error } : {}) });
    } else if (event.kind === 'navigate') void sessionNavigator.open(event.path);
  });
  let disposed = false;
  const dispose = () => {
    if (disposed) return;
    disposed = true;
    sessionNavigator.dispose();
    unsubscribeCompositions(); unsubscribeRuntime(); unsubscribeDesktop?.();
    window.removeEventListener('beforeunload', warnBeforeUnload);
    window.removeEventListener('pagehide', pagehide);
    window.removeEventListener('visibilitychange', visibility);
    prompts?.dispose(); application.dispose(); root.unmount(); platform.dispose?.();
  };
  const pagehide = (event: PageTransitionEvent) => { application.runtime.flushDrafts(); if (!event.persisted) dispose(); };
  const visibility = () => { if (document.visibilityState === 'hidden') application.runtime.flushDrafts(); };
  window.addEventListener('pagehide', pagehide);
  window.addEventListener('visibilitychange', visibility);
  requestAnimationFrame(() => requestAnimationFrame(() => {
    if (!disposed) { performance.mark('whip-shell-ready'); desktop?.ready(); }
  }));
  void application.runtime.connections.connectOnLaunch();
  return { ...application, dispose };
}
