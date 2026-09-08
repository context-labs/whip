import './polyfills';
import { WhipError } from '@whip/sdk';
import { connectionIssue, testConnection } from './connection-test';

const mockConnect = jest.fn();
const mockList = jest.fn();
const mockClose = jest.fn();
const mockCreate = jest.fn();
jest.mock('expo-crypto', () => ({ randomUUID: () => 'probe-client' }));
jest.mock('@whip/sdk', () => ({ ...jest.requireActual('@whip/sdk'), createWhipClient: (...args: unknown[]) => mockCreate(...args) }));
const mockFetch = jest.fn();
jest.mock('expo/fetch', () => ({ fetch: (...args: unknown[]) => mockFetch(...args) }));
function response(body: unknown = { available: true, protocol_major: 4, websocket_path: '/api/v3/ws' }, status = 200, contentType = 'application/json') {
  return { ok: status >= 200 && status < 300, status, headers: { get: (name: string) => name === 'content-type' ? contentType : null }, body: { getReader: () => { let read = false; return { read: async () => { if (read) return { done: true }; read = true; return { done: false, value: new TextEncoder().encode(JSON.stringify(body)) }; }, releaseLock: jest.fn() }; } } };
}
function options(signal = new AbortController().signal) { return { signal, onProgress: jest.fn() }; }
beforeEach(() => {
  jest.clearAllMocks();
  mockFetch.mockResolvedValue(response()); mockConnect.mockResolvedValue(undefined); mockList.mockResolvedValue({ items: [] });
  mockCreate.mockReturnValue({ connect: mockConnect, close: mockClose, requireConnected: () => ({ runtime_id: 'runtime' }), sessions: { list: mockList } });
});
afterEach(() => { jest.useRealTimers(); });

test('tests HTTPS, SDK handshake and one session without commands or recovery storage; closes on success', async () => {
  const opts = options();
  await expect(testConnection('host.example.ts.net', opts)).resolves.toEqual({ runtimeId: 'runtime', empty: true });
  expect(mockFetch).toHaveBeenCalledWith('https://host.example.ts.net/api/v3/web', expect.objectContaining({ credentials: 'omit', redirect: 'error' }));
  expect(mockCreate).toHaveBeenCalledWith(expect.objectContaining({ endpoint: 'https://host.example.ts.net', clientKind: 'automation' }));
  expect(mockCreate.mock.calls[0][0].recoveryStorage).toBeUndefined();
  expect(mockList).toHaveBeenCalledWith({ limit: 1 }, expect.objectContaining({ signal: expect.anything() }));
  expect(opts.onProgress.mock.calls.map(([progress]) => progress)).toEqual([
    { stage: 'https', completed: [] }, { stage: 'websocket', completed: ['https'] }, { stage: 'sessions', completed: ['https', 'websocket'] },
  ]);
  expect(mockClose).toHaveBeenCalledTimes(1);
});

test.each([401, 403, 404, 502])('shows HTTP %s instead of pretending the server is offline', async status => {
  mockFetch.mockResolvedValue(response({}, status));
  const error = await testConnection('https://host', options()).catch(e => e);
  expect(connectionIssue(error).title).toContain(`HTTP ${status}`); expect(mockCreate).not.toHaveBeenCalled();
});

test('a web page is not a successful API probe', async () => {
  mockFetch.mockResolvedValue(response('<html>web app</html>', 200, 'text/html'));
  const error = await testConnection('https://host', options()).catch(e => e);
  expect(connectionIssue(error).title).toBe('This address did not return the Whip API');
  expect(mockCreate).not.toHaveBeenCalled();
});

test('protocol mismatch remains distinct from a network failure', async () => {
  mockFetch.mockResolvedValue(response({ available: true, protocol_major: 99, websocket_path: '/api/v3/ws' }));
  const error = await testConnection('https://host', options()).catch(e => e);
  expect(connectionIssue(error)).toMatchObject({ title: 'Whip versions do not match', detail: 'App protocol 4; server protocol 99.' });
});

test('WSS failure preserves the native reason and reports that HTTPS passed', async () => {
  mockConnect.mockRejectedValue(new WhipError('disconnected', 'WebSocket closed (1006): certificate rejected'));
  const error = await testConnection('https://host', options()).catch(e => e);
  expect(connectionIssue(error)).toMatchObject({ title: 'HTTPS works, but the live connection failed', detail: expect.stringContaining('certificate rejected') });
  expect(mockClose).toHaveBeenCalledTimes(1); expect(mockList).not.toHaveBeenCalled();
});

test('session API failures are not reported as empty sessions', async () => {
  mockList.mockRejectedValue(new WhipError('timeout', 'Query timed out'));
  const error = await testConnection('https://host', options()).catch(e => e);
  expect(connectionIssue(error).title).toBe('Connected, but sessions could not be read'); expect(mockClose).toHaveBeenCalledTimes(1);
});

test('rejects a replacement runtime without querying its sessions', async () => {
  mockConnect.mockRejectedValue(new WhipError('runtime_changed', 'The endpoint now serves a different runtime'));
  const error = await testConnection('https://host', { ...options(), expectedRuntimeId: 'old-runtime' }).catch(e => e);
  expect(mockCreate).toHaveBeenCalledWith(expect.objectContaining({ expectedRuntimeId: 'old-runtime' }));
  expect(connectionIssue(error).title).toBe('This server’s identity changed'); expect(mockList).not.toHaveBeenCalled(); expect(mockClose).toHaveBeenCalledTimes(1);
});

test('timeout ends even an unresponsive fetch and aborts its request', async () => {
  jest.useFakeTimers(); mockFetch.mockImplementation(() => new Promise(() => {}));
  const outcome = testConnection('https://host', options()).catch(e => e);
  await jest.advanceTimersByTimeAsync(15_000);
  expect(connectionIssue(await outcome)).toMatchObject({ title: 'Could not reach the HTTPS API', detail: 'No reply within 15 seconds.' });
  expect(mockFetch.mock.calls[0][1].signal.aborted).toBe(true); expect(jest.getTimerCount()).toBe(0);
});

test('cancel during WSS closes the transient client and skips session reads', async () => {
  const controller = new AbortController();
  mockConnect.mockImplementation(async () => { controller.abort(new Error('left-screen')); return new Promise(() => {}); });
  await expect(testConnection('https://host', options(controller.signal))).rejects.toThrow('left-screen');
  expect(mockClose).toHaveBeenCalledTimes(1); expect(mockList).not.toHaveBeenCalled();
});

test('a pre-cancelled probe never contacts the host', async () => {
  const controller = new AbortController(); controller.abort(new Error('cancelled'));
  await expect(testConnection('https://host', options(controller.signal))).rejects.toThrow('cancelled');
  expect(mockFetch).not.toHaveBeenCalled(); expect(mockCreate).not.toHaveBeenCalled();
});


test('a headless daemon without embedded web assets can serve the mobile API', async () => {
  mockFetch.mockResolvedValue(response({ available: false, protocol_major: 4, websocket_path: '/api/v3/ws' }));
  await expect(testConnection('https://host', options())).resolves.toEqual({ runtimeId: 'runtime', empty: true });
});

test('oversized chunked discovery is stopped before reading an unbounded body', async () => {
  const read = jest.fn(async () => ({ done: false, value: new Uint8Array(4097) }));
  const releaseLock = jest.fn();
  mockFetch.mockResolvedValue({ ...response(), body: { getReader: () => ({ read, releaseLock }) } });
  const error = await testConnection('https://host', options()).catch(e => e);
  expect(connectionIssue(error).title).toBe('Unexpected API response');
  expect(read).toHaveBeenCalledTimes(2); expect(releaseLock).toHaveBeenCalled();
  expect(mockFetch.mock.calls[0][1].signal.aborted).toBe(true);
  expect(mockCreate).not.toHaveBeenCalled();
});
