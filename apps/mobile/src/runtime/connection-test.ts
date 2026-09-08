import * as Crypto from 'expo-crypto';
import { fetch } from 'expo/fetch';
import { createWhipClient, WhipError, type WhipClient } from '@whip/sdk';
import { manifest } from '@whip/protocol';
import { serverOrigin } from './address';

export type ConnectionStage = 'https' | 'websocket' | 'sessions';
export const connectionStages: Record<ConnectionStage, string> = {
  https: 'Reach the Whip HTTPS API', websocket: 'Open the live WebSocket connection', sessions: 'Read sessions',
};
export type ConnectionProgress = { stage: ConnectionStage; completed: ConnectionStage[] };
export type ConnectionTestResult = { runtimeId: string; empty: boolean };
export type ConnectionIssue = { title: string; message: string; detail?: string };
const networkHelp = 'Check that Tailscale is connected on this phone and the computer, both use the same tailnet, and its access rules allow this phone. In iOS Settings, allow Local Network access for Whip if it is listed. Then retry.';

export function connectionIssue(error: unknown): ConnectionIssue {
  if (error instanceof ConnectionTestError) return error.issue;
  const kind = error instanceof WhipError ? error.kind : undefined;
  const detail = error instanceof Error ? error.message.slice(0, 300) : undefined;
  switch (kind) {
    case 'timeout': return { title: 'The connection timed out', message: networkHelp, detail };
    case 'disconnected': return { title: 'The live connection could not open', message: `${networkHelp} Use Test Connection to find which step fails.`, detail };
    case 'unsupported_protocol': return { title: 'Whip versions do not match', message: 'Update the Whip daemon and this app to compatible versions, then try again.', detail };
    case 'runtime_changed': return { title: 'This server’s identity changed', message: 'Verify that this is the intended host before removing and adding its saved entry. Existing drafts and delivery records are retained.', detail };
    case 'paused': return { title: 'Connection paused', message: 'Keep Whip open while connecting, then try again.' };
    default: return { title: 'Could not finish connecting', message: 'Run Test Connection to check the server. If it passes, this phone may be unable to save or restore its local data. Keep your saved data while investigating.', detail };
  }
}
class ConnectionTestError extends Error {
  constructor(readonly issue: ConnectionIssue) { super(issue.title); }
}
function fail(title: string, message: string, detail?: string): never { throw new ConnectionTestError({ title, message, detail }); }

/** Read-only probe: no saved host, recovery state, session or provider mutation. */
export async function testConnection(input: string, options: {
  signal: AbortSignal; expectedRuntimeId?: string; onProgress(progress: ConnectionProgress): void;
}): Promise<ConnectionTestResult> {
  const origin = serverOrigin(input, __DEV__);
  const completed: ConnectionStage[] = [];
  let client: WhipClient | undefined;
  async function step<T>(stage: ConnectionStage, work: (signal: AbortSignal) => Promise<T>): Promise<T> {
    options.signal.throwIfAborted();
    options.onProgress({ stage, completed: [...completed] });
    options.signal.throwIfAborted();
    const controller = new AbortController();
    const abort = () => controller.abort(options.signal.reason);
    options.signal.addEventListener('abort', abort, { once: true });
    const timer = setTimeout(() => controller.abort(new WhipError('timeout', 'No reply within 15 seconds.')), 15_000);
    let stop!: () => void;
    const cancelled = new Promise<never>((_, reject) => {
      stop = () => reject(controller.signal.reason);
      controller.signal.addEventListener('abort', stop, { once: true });
    });
    try {
      const result = await Promise.race([work(controller.signal), cancelled]);
      options.signal.throwIfAborted();
      completed.push(stage);
      return result;
    } catch (error) {
      if (options.signal.aborted || error instanceof ConnectionTestError) throw error;
      const issue = connectionIssue(error);
      if (error instanceof WhipError && ['unsupported_protocol', 'runtime_changed'].includes(error.kind)) throw new ConnectionTestError(issue);
      if (stage === 'https') fail('Could not reach the HTTPS API', `${networkHelp} The phone could not complete a secure request; this alone cannot distinguish DNS, VPN, TLS or host availability.`, issue.detail);
      if (stage === 'websocket')
        fail('HTTPS works, but the live connection failed', 'The Whip API is reachable. Check that Tailscale Serve forwards WebSocket upgrades to the same daemon and that WHIP_ALLOWED_HOSTS / WHIP_ALLOWED_ORIGINS include this HTTPS address. Then retry.', issue.detail);
      if (stage === 'sessions') fail('Connected, but sessions could not be read', 'The secure WebSocket handshake passed. Check the daemon status and logs, then retry. An empty session list is valid and does not cause this error.', issue.detail);
      throw new ConnectionTestError(issue);
    } finally {
      clearTimeout(timer);
      options.signal.removeEventListener('abort', abort);
      controller.signal.removeEventListener('abort', stop);
      controller.abort();
    }
  }
  try {
    await step('https', async signal => {
      const response = await fetch(`${origin}/api/v3/web`, { signal, headers: { Accept: 'application/json' }, credentials: 'omit', redirect: 'error' });
      if (response.status === 401 || response.status === 403)
        fail(`The server rejected this address (HTTP ${response.status})`, 'Check the daemon’s exact WHIP_ALLOWED_HOSTS entry and any proxy access rules. This release uses Tailscale access rather than an app login.');
      if (!response.ok) fail(`The Whip API returned HTTP ${response.status}`, 'Check that Tailscale Serve forwards to the running Whip daemon. Use the base HTTPS address, without /api/v3/ws or a web page path.');
      if (!response.headers.get('content-type')?.toLowerCase().includes('application/json'))
        fail('This address did not return the Whip API', 'A web page alone is not enough. The same address must serve /api/v3/web and /api/v3/ws. Check the proxy target and update the daemon if needed.');
      if (Number(response.headers.get('content-length')) > 8192) fail('Unexpected API response', 'The discovery response is too large. Check that this address points to the Whip daemon.');
      const reader = response.body?.getReader();
      if (!reader) fail('The Whip API returned no data', 'Check that the proxy points to a compatible Whip daemon.');
      const bytes = new Uint8Array(8192);
      let length = 0;
      try {
        while (true) {
          const { done, value } = await reader.read();
          if (done) break;
          if (length + value.byteLength > bytes.length) fail('Unexpected API response', 'The discovery response is too large. Check the proxy target.');
          bytes.set(value, length); length += value.byteLength;
        }
      } finally { reader.releaseLock(); }
      const body = new TextDecoder().decode(bytes.subarray(0, length));
      let data;
      try { data = JSON.parse(body); } catch { fail('The Whip API returned invalid JSON', 'Check that the proxy points to a compatible Whip daemon.'); }
      if (!data || typeof data.available !== 'boolean' || data.websocket_path !== '/api/v3/ws')
        fail('The Whip API is unavailable at this address', 'The base URL may serve a different app or an older daemon. Check the proxy target and update Whip.');
      if (data.protocol_major !== manifest.major) throw new WhipError('unsupported_protocol', `App protocol ${manifest.major}; server protocol ${String(data.protocol_major).slice(0, 16)}.`);
    });
    await step('websocket', async signal => {
      client = createWhipClient({ endpoint: origin, clientId: Crypto.randomUUID(), clientKind: 'automation', expectedRuntimeId: options.expectedRuntimeId,
        buildId: '@whip/mobile:connection-test', connectTimeoutMs: 15_000, queryTimeoutMs: 15_000, randomUUID: Crypto.randomUUID });
      await client.connect({ signal });
    });
    const sessions = await step('sessions', signal => client!.sessions.list({ limit: 1 }, { signal }));
    return { runtimeId: client!.requireConnected().runtime_id, empty: !sessions.items?.length };
  } finally { client?.close(); }
}
