import type { WhipClient } from '@whip/sdk';
import type { MobileRuntime } from './runtime';
/** Native document pickers may background the app. Resume only the original host. */
export function waitForReady(runtime: MobileRuntime, client: WhipClient, signal: AbortSignal) {
  return new Promise<void>((resolve, reject) => {
    let stop = () => {};
    const finish = (error?: Error) => { clearTimeout(timer); stop(); signal.removeEventListener('abort', abort); error ? reject(error) : resolve(); };
    const abort = () => finish(new Error('Import cancelled.'));
    const timer = setTimeout(() => finish(new Error('Reconnect this host and try importing again.')), 15_000);
    const check = () => { const state = runtime.getSnapshot(); if (state.client !== client) finish(new Error('The theme source changed. Choose the file again.')); else if (state.active && state.ready) finish(); };
    stop = runtime.subscribe(check); signal.addEventListener('abort', abort, { once: true });
    if (signal.aborted) abort(); else check();
  });
}
