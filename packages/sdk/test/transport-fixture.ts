import { manifest, type InitializeResult } from '@whip/protocol';
import type { TransportFactory } from '../src/transport.js';

export interface FixtureRequest { id: string; method: string; params: Record<string, unknown>; raw: string }
export interface FixtureConnection {
  requests: FixtureRequest[];
  reply(request: FixtureRequest, result: unknown): void;
  error(request: FixtureRequest, kind: string): void;
  fail(): void;
}

/** Real client protocol processing with controlled acknowledgements and failures. */
export function transportFixture(options: {
  kind?: 'unix' | 'websocket';
  initialize?: Partial<InitializeResult>;
  request?: (request: FixtureRequest, connection: FixtureConnection) => void;
} = {}) {
  const connections: FixtureConnection[] = [];
  const info: InitializeResult = {
    protocol_major: 4, protocol_minor: 0, runtime_id: 'fixture-runtime', connection_id: 'fixture-connection',
    generation: '9007199254740993', build_id: 'fixture', host_platform: 'darwin', host_architecture: 'arm64',
    capabilities: [], negotiated_capabilities: [],
    operations: manifest.operations.map(operation => ({ ...operation })),
    limits: { frame_bytes: 1 << 20, connections: 64, in_flight_requests: 32, outbound_messages: 1024,
      outbound_bytes: String(8 << 20), root_subscriptions: 16, content_chunk_bytes: 4, upload_bytes: String(64 << 20) },
    ...options.initialize,
  };
  const factory: TransportFactory = async handlers => {
    const connection: FixtureConnection = {
      requests: [],
      reply: (request, result) => handlers.message(JSON.stringify({ jsonrpc: '2.0', id: request.id, result })),
      error: (request, kind) => handlers.message(JSON.stringify({ jsonrpc: '2.0', id: request.id,
        error: { code: -32000, message: kind, data: { kind } } })),
      fail: () => handlers.close(new Error('Fixture connection lost')),
    };
    connections.push(connection);
    return {
      kind: options.kind ?? 'unix', httpEndpoint: 'http://fixture.invalid', bufferedAmount: 0,
      send(raw) {
        const request = { ...JSON.parse(raw), raw } as FixtureRequest;
        connection.requests.push(request);
        if (request.method === 'initialize') connection.reply(request, { ...info, connection_id: `${info.connection_id}-${connections.length}` });
        else options.request?.(request, connection);
      },
      close() {},
    };
  };
  return { info, factory, connections, get current() { return connections.at(-1)!; } };
}
