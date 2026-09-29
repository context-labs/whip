import * as Crypto from 'expo-crypto';
import { Client, DeliveryError, RemoteError, type GatewayDiscovery } from '@whip/sdk';
import { browserSocket, discoverGateway } from '@whip/sdk/browser';
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
  const detail = error instanceof Error ? error.message.slice(0, 300) : undefined;
  if (error instanceof RemoteError && error.kind === 'IDENTITY') return identityIssue(detail);
  if (error instanceof Error && error.name === 'TimeoutError') return { title: 'The connection timed out', message: networkHelp, detail };
  if (error instanceof DeliveryError) return { title: 'The live connection could not open', message: `${networkHelp} Use Test Connection to find which step fails.`, detail };
  return { title: 'Could not finish connecting', message: 'Run Test Connection to check the server. If it passes, this phone may be unable to save or restore its local data. Keep your saved data while investigating.', detail };
}
function identityIssue(detail?: string): ConnectionIssue {
  return { title: 'This server’s identity changed', message: 'Verify that this is the intended host before removing and adding its saved entry. Existing drafts and delivery records are retained.', detail };
}
class ConnectionTestError extends Error {
  constructor(readonly issue: ConnectionIssue) { super(issue.title); }
}

/** Read-only native probe. Each SDK socket belongs to one request and closes
 * after its response or local cancellation; the probe never owns remote work. */
export async function testConnection(input: string, options: {
  signal: AbortSignal; expectedRuntimeId?: string; onProgress(progress: ConnectionProgress): void;
}): Promise<ConnectionTestResult> {
  const origin = serverOrigin(input, __DEV__);
  const completed: ConnectionStage[] = [];
  async function step<T>(stage: ConnectionStage, work: (signal: AbortSignal) => Promise<T>): Promise<T> {
    options.signal.throwIfAborted();
    options.onProgress({ stage, completed: [...completed] });
    options.signal.throwIfAborted();
    const controller = new AbortController();
    const abort = () => controller.abort(options.signal.reason);
    options.signal.addEventListener('abort', abort, { once: true });
    const timer = setTimeout(() => controller.abort(new DOMException('No reply within 15 seconds.', 'TimeoutError')), 15_000);
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
      if (error instanceof RemoteError && error.kind === 'IDENTITY') throw new ConnectionTestError(issue);
      if (stage === 'https') throw new ConnectionTestError({ title: 'Could not reach the HTTPS API', message: `${networkHelp} Check that the base address serves /api/v4/web. A web page alone is not enough.`, detail: issue.detail });
      if (stage === 'websocket') throw new ConnectionTestError({ title: 'HTTPS works, but the live connection failed', message: 'The Whip API is reachable. Check that the gateway forwards WebSocket upgrades to the same runtime and that its Host and Origin allowlists include this HTTPS address. Then retry.', detail: issue.detail });
      throw new ConnectionTestError({ title: 'Connected, but sessions could not be read', message: 'The secure WebSocket handshake passed. Check the runtime status and logs, then retry. An empty session list is valid and does not cause this error.', detail: issue.detail });
    } finally {
      clearTimeout(timer);
      options.signal.removeEventListener('abort', abort);
      controller.signal.removeEventListener('abort', stop);
      controller.abort();
    }
  }
  const discovery = await step('https', async signal => {
    let gateway: Readonly<GatewayDiscovery>;
    try { gateway = await discoverGateway(origin, { signal }); }
    catch (error) {
      if (!signal.aborted && error instanceof TypeError) throw new ConnectionTestError({
        title: 'Whip versions or gateway metadata do not match',
        message: 'This address must serve the native v4 Whip gateway. Update the host and this app to compatible versions, then try again.',
        detail: error.message.slice(0, 300),
      });
      throw error;
    }
    if (options.expectedRuntimeId && gateway.runtime_id !== options.expectedRuntimeId)
      throw new ConnectionTestError(identityIssue());
    return gateway;
  });
  const client = await step('websocket', signal => Client.connect(browserSocket(origin, {
    expectedRuntimeID: discovery.runtime_id, expectedProcessEpoch: discovery.process_epoch,
  }), { clientID: Crypto.randomUUID(), expectedRuntimeID: discovery.runtime_id, signal, timeoutMs: 15_000 }));
  const sessions = await step('sessions', signal => client.trees.list({ limit: 1 }, { signal, timeoutMs: 15_000 }));
  return { runtimeId: client.runtimeID, empty: sessions.items.length === 0 };
}
