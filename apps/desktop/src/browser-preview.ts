import { session, type WebContents } from 'electron';
import type { BrowserEnvironmentLease } from './browser-manager';
import type { PreviewEnvironments } from './preview-environments';

/** Materialize a tab-lifetime lease without exposing its partition or credentials. */
export async function browserPreviewLease(environments: PreviewEnvironments, id: string, tabId: string): Promise<BrowserEnvironmentLease> {
  const lease = await environments.acquire(id, tabId);
  const profile = session.fromPartition(lease.partition);
  const cleanup: Array<() => void> = [];
  let closed = false;
  try { await profile.setProxy(lease.proxyConfig); }
  catch (error) { await lease.release(); throw error; }
  return {
    session: profile,
    bind(contents: WebContents) {
      if (closed) throw new Error('Preview lease is closed');
      cleanup.push(lease.bindContents(contents));
      const login = (event: Electron.Event, _request: Electron.AuthenticationResponseDetails, info: Electron.AuthInfo, callback: (username?: string, password?: string) => void) => {
        event.preventDefault();
        const credentials = closed ? undefined : lease.authenticate(contents, info);
        if (credentials) callback(credentials.username, credentials.password); else callback();
      };
      contents.on('login', login);
      cleanup.push(() => contents.removeListener('login', login));
    },
    async close() {
      if (closed) return; closed = true;
      for (const dispose of cleanup.splice(0)) dispose();
      await lease.release();
    },
  };
}
