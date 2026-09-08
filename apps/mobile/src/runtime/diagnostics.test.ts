/** @jest-environment node */
import type { MobileRuntime } from './runtime';
import { diagnostics } from './diagnostics';

test('diagnostics exports only metadata, even when errors and commands contain private content', () => {
  const state = {
    active: true, ready: false, connecting: false, hosts: [{ url: 'https://private-host.example' }],
    host: { runtimeId: 'runtime-1', clientId: 'secret-client', url: 'https://private-host.example' },
    error: 'PRIVATE PROMPT from a remote error', lastErrorCode: 'delivery_uncertain',
    commands: [{ message: 'PRIVATE PROMPT', outcome: { result: { text: 'PRIVATE PROMPT' } } }],
    client: { getSnapshot: () => ({ state: 'reconnecting', error: new Error('PRIVATE PROMPT'), info: { protocol_major: 3, protocol_minor: 0, build_id: 'PRIVATE PROMPT' } }) },
  } as unknown as ReturnType<MobileRuntime['getSnapshot']>;
  const result = diagnostics(state, 'ios', '26.2');
  expect(result).toMatchObject({ runtime: 'runtime-1', protocol: '3.0', connection: 'reconnecting', lastErrorCode: 'delivery_uncertain', commandRecords: 1 });
  const text = JSON.stringify(result);
  for (const secret of ['PRIVATE PROMPT', 'secret-client', 'private-host.example']) expect(text).not.toContain(secret);
});
