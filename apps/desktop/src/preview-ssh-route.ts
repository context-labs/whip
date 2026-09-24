import { randomBytes } from 'node:crypto';
import { chmod, lstat, rm } from 'node:fs/promises';
import { connect, type Socket } from 'node:net';
import path from 'node:path';

export interface SSHPreviewRequest { remoteHost: '127.0.0.1' | '::1'; port: number; expectedGeneration: string }
export interface SSHPreviewRoute {
  readonly generation: string;
  readonly signal: AbortSignal;
  open(signal: AbortSignal): Promise<Socket>;
  close(): Promise<void>;
}
export interface PreviewSSHMaster {
  generation: string; directory: string; signal: AbortSignal;
  command(args: string[], signal?: AbortSignal): Promise<string>;
}
export function validatePreviewRequest(request: SSHPreviewRequest) {
  if (!request || !['127.0.0.1', '::1'].includes(request.remoteHost) || !Number.isInteger(request.port) ||
    request.port < 1 || request.port > 65535 || !request.expectedGeneration) throw new Error('Invalid preview route');
}
/** The command closure must address only the existing authenticated control master. */
export async function acquireSSHPreviewRoute(master: PreviewSSHMaster, request: SSHPreviewRequest, signal: AbortSignal): Promise<SSHPreviewRoute> {
  validatePreviewRequest(request); signal.throwIfAborted(); master.signal.throwIfAborted();
  if (master.generation !== request.expectedGeneration) throw new Error('SSH preview connection changed');
  const controller = new AbortController();
  const socketPath = path.join(master.directory, `p-${randomBytes(8).toString('hex')}`);
  const host = request.remoteHost === '::1' ? '[::1]' : '127.0.0.1';
  const descriptor = `${socketPath}:${host}:${request.port}`;
  const sockets = new Set<Socket>();
  let closing: Promise<void> | undefined;
  const close = () => {
    if (closing) return closing;
    closing = (async () => {
      await Promise.resolve();
      try {
        if (!master.signal.aborted) await master.command(['-O', 'cancel', '-L', descriptor]);
      } finally { await rm(socketPath, { force: true }); }
    })();
    controller.abort(); for (const socket of sockets) socket.destroy();
    signal.removeEventListener('abort', abort); master.signal.removeEventListener('abort', abort);
    return closing;
  };
  const abort = () => { void close().catch(() => {}); };
  const lifetime = AbortSignal.any([signal, master.signal, controller.signal]);
  try {
    await master.command(['-O', 'forward', '-L', descriptor], lifetime);
    lifetime.throwIfAborted();
    if (!(await lstat(socketPath)).isSocket()) throw new Error('SSH preview forward did not create a socket');
    await chmod(socketPath, 0o600); lifetime.throwIfAborted();
    signal.addEventListener('abort', abort, { once: true }); master.signal.addEventListener('abort', abort, { once: true });
  } catch (error) { await close().catch(() => {}); throw error; }
  return {
    generation: master.generation, signal: lifetime, close,
    async open(operationSignal) {
      lifetime.throwIfAborted(); operationSignal.throwIfAborted();
      if (sockets.size >= 32) throw new Error('SSH preview stream limit reached');
      return await new Promise<Socket>((resolve, reject) => {
        const socket = connect({ path: socketPath, signal: AbortSignal.any([lifetime, operationSignal]) });
        sockets.add(socket); socket.once('close', () => sockets.delete(socket)); socket.on('error', () => {});
        socket.once('error', reject);
        socket.once('connect', () => { socket.removeListener('error', reject); resolve(socket); });
      });
    },
  };
}
