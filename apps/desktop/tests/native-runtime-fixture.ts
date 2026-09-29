import path from 'node:path';
import type { DaemonStatus } from '../src/runtime';

export function nativeRuntimeStatus(socket: string, state: DaemonStatus['state'] = 'running'): DaemonStatus {
  const directory = path.dirname(socket);
  return { state, socket, directory, log: path.join(directory, 'runtime.log'), client_build: 'local-test',
    process: state === 'running' ? { runtime_id: 'existing-runtime', process_epoch: 'epoch-fixture', pid: 123,
      build: 'local-test', started_at: '2026-09-28T00:00:00Z', web_endpoint: '' } : null };
}
