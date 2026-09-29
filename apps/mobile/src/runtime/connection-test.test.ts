/** @jest-environment node */
import './polyfills';
import { connectionIssue, testConnection } from './connection-test';

jest.mock('expo-crypto', () => ({ randomUUID: () => 'probe-client' }));
const discovery = { available: false, major: 4, runtime_id: 'runtime', process_epoch: 'boot', websocket_path: '/api/v4/ws', content_path: '/api/v4/content/', max_content_bytes: 4 << 20 };
const initial = { major: 4, minor: 0, runtime_id: 'runtime', process_epoch: 'boot', network_client: true, builtins: [] };
const savedFetch = globalThis.fetch;
const savedSocket = globalThis.WebSocket;
const mockFetch = jest.fn();
let handle: (request: { id: string; method: string }) => unknown;
let open: (socket: Socket) => void;
class Socket {
  static OPEN = 1; static CLOSING = 2;
  static instances: Socket[] = [];
  readyState = 0; bufferedAmount = 0;
  sent: { method: string; params: unknown }[] = [];
  onopen: (() => void) | null = null;
  onmessage: ((event: { data: string }) => void) | null = null;
  onerror: (() => void) | null = null;
  onclose: ((event: { code: number; reason: string }) => void) | null = null;
  constructor(readonly url: string) { Socket.instances.push(this); Promise.resolve().then(() => open(this)); }
  send(raw: string) {
    const request = JSON.parse(raw); this.sent.push(request);
    const response = handle(request);
    if (response) Promise.resolve().then(() => this.onmessage?.({ data: JSON.stringify(response) }));
  }
  close() { this.readyState = 3; }
}
function response(body: unknown = discovery, status = 200, contentType = 'application/json') {
  return new Response(JSON.stringify(body), { status, headers: { 'content-type': contentType } });
}
function options(signal = new AbortController().signal) { return { signal, onProgress: jest.fn() }; }
beforeEach(() => {
  jest.clearAllMocks(); Socket.instances = [];
  Object.assign(globalThis, { __DEV__: true, fetch: mockFetch, WebSocket: Socket });
  mockFetch.mockResolvedValue(response());
  open = socket => { socket.readyState = 1; socket.onopen?.(); };
  handle = request => ({ jsonrpc: '2.0', id: request.id, result: request.method === 'initialize' ? initial : { revision: '9007199254740993', items: [], next_cursor: null } });
});
afterEach(() => { globalThis.fetch = savedFetch; globalThis.WebSocket = savedSocket; jest.useRealTimers(); });

test('native discovery pins each actual SDK socket and reads one tree without mutations', async () => {
  const opts = options();
  await expect(testConnection('host.example.ts.net', opts)).resolves.toEqual({ runtimeId: 'runtime', empty: true });
  expect(String(mockFetch.mock.calls[0][0])).toBe('https://host.example.ts.net/api/v4/web');
  expect(mockFetch.mock.calls[0][1]).toMatchObject({ credentials: 'omit', redirect: 'error', cache: 'no-store' });
  expect(Socket.instances).toHaveLength(2);
  for (const socket of Socket.instances) {
    expect(socket.url).toBe('wss://host.example.ts.net/api/v4/ws');
    expect(socket.sent[0]).toMatchObject({ method: 'initialize', params: { major: 4, expected_runtime_id: 'runtime', expected_process_epoch: 'boot', network_client: true } });
    expect(socket.readyState).toBe(3); expect(socket.onmessage).toBeNull();
  }
  expect(Socket.instances.flatMap(socket => socket.sent.map(request => request.method))).toEqual(['initialize', 'initialize', 'trees.list']);
  expect(Socket.instances[1].sent[1].params).toEqual({ limit: 1 });
  expect(opts.onProgress.mock.calls.map(([progress]) => progress)).toEqual([
    { stage: 'https', completed: [] }, { stage: 'websocket', completed: ['https'] }, { stage: 'sessions', completed: ['https', 'websocket'] },
  ]);
});

test.each([401, 403, 404, 502])('HTTP %s remains visible instead of pretending the catalog is empty', async status => {
  mockFetch.mockResolvedValue(response({}, status));
  const error = await testConnection('https://host', options()).catch(e => e);
  expect(connectionIssue(error).detail).toContain(String(status)); expect(Socket.instances).toHaveLength(0);
});

test.each([
  { ...discovery, major: 3 }, { ...discovery, process_epoch: '' },
  { ...discovery, websocket_path: '/api/v3/ws' }, { ...discovery, runtime_id: '' },
])('rejects incompatible discovery before connecting: %j', async metadata => {
  mockFetch.mockResolvedValue(response(metadata));
  const error = await testConnection('https://host', options()).catch(e => e);
  expect(connectionIssue(error).title).toBe('Whip versions or gateway metadata do not match');
  expect(Socket.instances).toHaveLength(0);
});

test('a web page is not a successful API probe', async () => {
  mockFetch.mockResolvedValue(response('<html>web app</html>', 200, 'text/html'));
  const error = await testConnection('https://host', options()).catch(e => e);
  expect(connectionIssue(error).detail).toContain('JSON'); expect(Socket.instances).toHaveLength(0);
});

test('WSS failure preserves its native reason and reports that HTTPS passed', async () => {
  open = socket => socket.onclose?.({ code: 1006, reason: 'certificate rejected' });
  const error = await testConnection('https://host', options()).catch(e => e);
  expect(connectionIssue(error)).toMatchObject({ title: 'HTTPS works, but the live connection failed', detail: expect.stringContaining('certificate rejected') });
  expect(Socket.instances).toHaveLength(1); expect(Socket.instances[0].readyState).toBe(3);
});

test('session API failures are distinct from an empty catalog', async () => {
  handle = request => ({ jsonrpc: '2.0', id: request.id, ...(request.method === 'initialize' ? { result: initial } : { error: { code: -32603, kind: 'INTERNAL', message: 'Query failed' } }) });
  const error = await testConnection('https://host', options()).catch(e => e);
  expect(connectionIssue(error).title).toBe('Connected, but sessions could not be read');
  expect(Socket.instances.every(socket => socket.readyState === 3)).toBe(true);
});

test('rejects a replacement saved runtime before opening any socket', async () => {
  const error = await testConnection('https://host', { ...options(), expectedRuntimeId: 'old-runtime' }).catch(e => e);
  expect(connectionIssue(error).title).toBe('This server’s identity changed'); expect(Socket.instances).toHaveLength(0);
});

test.each([{ ...initial, runtime_id: 'replacement' }, { ...initial, process_epoch: 'restarted' }, { ...initial, network_client: false }])('rejects changed handshake metadata without querying or rediscovering: %j', async metadata => {
  handle = request => ({ jsonrpc: '2.0', id: request.id, result: metadata });
  await expect(testConnection('https://host', options())).rejects.toThrow();
  expect(mockFetch).toHaveBeenCalledTimes(1);
  expect(Socket.instances).toHaveLength(1); expect(Socket.instances[0].sent).toHaveLength(1);
  expect(Socket.instances[0].readyState).toBe(3);
});

test('timeout ends even an unresponsive discovery and aborts its request', async () => {
  jest.useFakeTimers(); mockFetch.mockImplementation(() => new Promise(() => {}));
  const outcome = testConnection('https://host', options()).catch(e => e);
  await jest.advanceTimersByTimeAsync(15_000);
  expect(connectionIssue(await outcome)).toMatchObject({ title: 'Could not reach the HTTPS API', detail: 'No reply within 15 seconds.' });
  expect(mockFetch.mock.calls[0][1].signal.aborted).toBe(true); expect(jest.getTimerCount()).toBe(0);
});

test('cancel during WSS closes the transient socket and skips session reads', async () => {
  const controller = new AbortController();
  open = () => controller.abort(new Error('left-screen'));
  await expect(testConnection('https://host', options(controller.signal))).rejects.toThrow('left-screen');
  expect(Socket.instances).toHaveLength(1); expect(Socket.instances[0].sent).toHaveLength(0);
  expect(Socket.instances[0].readyState).toBe(3);
});

test('a pre-cancelled probe never contacts the host', async () => {
  const controller = new AbortController(); controller.abort(new Error('cancelled'));
  await expect(testConnection('https://host', options(controller.signal))).rejects.toThrow('cancelled');
  expect(mockFetch).not.toHaveBeenCalled(); expect(Socket.instances).toHaveLength(0);
});

test('oversized chunked discovery is cancelled before reading an unbounded body', async () => {
  const cancel = jest.fn();
  mockFetch.mockResolvedValue(new Response(new ReadableStream({ start(controller) { controller.enqueue(new Uint8Array(4097)); }, cancel }), { headers: { 'content-type': 'application/json' } }));
  const error = await testConnection('https://host', options()).catch(e => e);
  expect(connectionIssue(error).detail).toContain('4096'); expect(cancel).toHaveBeenCalledTimes(1);
  expect(mockFetch.mock.calls[0][1].signal.aborted).toBe(true); expect(Socket.instances).toHaveLength(0);
});
