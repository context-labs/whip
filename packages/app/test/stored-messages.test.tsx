import { StrictMode } from 'react';
import { createHash, webcrypto } from 'node:crypto';
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { Client } from '@whip/sdk';
import type { ContentReference } from '@whip/protocol';
import { UIProvider } from '@whip/ui';
import { RuntimeContext } from '../src/context';
import type { AppRuntime } from '../src/runtime';
import { ImageAttachment, Timeline, type TimelineRow } from '../src/timeline';

vi.mock('../src/reading-list', () => ({
  ReadingList: ({ rows, renderRow }: { rows: TimelineRow[]; renderRow(row: TimelineRow): React.ReactNode }) =>
    <div>{rows.map(row => <div key={row.id}>{renderRow(row)}</div>)}</div>,
}));
const image = { url: 'data:image/png;base64,AAAA', width: 1754, height: 1902 };
const row: TimelineRow = { id: 'message:authored', seq: '9007199254740993', role: 'user', text: 'Please inspect **this screenshot**.', references: ['image'] };
const clients: QueryClient[] = [];
const bytes = Uint8Array.of(0, 0, 0);
const digest = createHash('sha256').update(bytes).digest('hex');
let createURL: ReturnType<typeof vi.fn<(object: Blob | MediaSource) => string>>;
let revokeURL: ReturnType<typeof vi.fn<(url: string) => void>>;
beforeEach(() => {
  let next = 0;
  createURL = vi.fn((_object: Blob | MediaSource) => `blob:verified-${++next}`); revokeURL = vi.fn();
  vi.stubGlobal('URL', class extends URL { static createObjectURL = createURL; static revokeObjectURL = revokeURL; });
  vi.stubGlobal('crypto', webcrypto);
});
afterEach(() => { cleanup(); clients.splice(0).forEach(client => client.clear()); vi.unstubAllGlobals(); });
async function fixture(toolDensity = 'compact') {
  const queries = new QueryClient({ defaultOptions: { queries: { retry: false, networkMode: 'always' } } });
  clients.push(queries);
  const reference = (session: string, id: string): ContentReference => ({ id, session_id: session, size: '3', digest, media_type: 'image/png', created_at: '2026-09-28T00:00:00Z' });
  const metadata = vi.fn(async (owner: string, id: string) => reference(owner, id));
  const read = vi.fn(async (owner: string, id: string, _signal?: AbortSignal) => ({ reference: reference(owner, id), data_base64: 'AAAA' }));
  const client = await Client.connect(async (request, _runtimeID, options) => {
    const params = request.params as { session_id: string; reference_id: string };
    const result = request.method === 'initialize' ? { major: 4, minor: 0, runtime_id: 'host', process_epoch: 'boot', network_client: false, builtins: [] }
      : request.method === 'content.get' ? await metadata(params.session_id, params.reference_id)
      : request.method === 'content.read' ? await read(params.session_id, params.reference_id, options?.signal) : undefined;
    if (!result) throw new Error(`Unexpected method: ${request.method}`);
    return { jsonrpc: '2.0', id: request.id, result };
  }, { clientID: 'window' });
  const copy = vi.fn(async () => {}), readBody = vi.fn(), loadGap = vi.fn(async (_id: string) => {});
  const runtime = { queries, platform: { copy }, report: vi.fn(), subscribe: () => () => {}, getSnapshot: () => ({ preferences: { toolDensity } }) } as unknown as AppRuntime;
  const app = (rows = [row], connected = true, agentId = 'root', revision = '1') => <StrictMode><QueryClientProvider client={queries}><RuntimeContext.Provider value={runtime}><UIProvider>
    <Timeline rows={rows} hasMore={false} loadOlder={async () => {}} loadGap={loadGap} readBody={readBody} connected={connected} historyRevision={revision}
      messageScope={{ client, rootId: 'root', agentId }} />
  </UIProvider></RuntimeContext.Provider></QueryClientProvider></StrictMode>;
  return { app, metadata, read, copy, readBody, loadGap, queries, reference };
}
it('restores canonical historical text, a verified image reference and the original copy text', async () => {
  const f = await fixture(); render(f.app());
  expect(await screen.findByText('this screenshot')).toBeTruthy();
  const img = await screen.findByAltText('Attachment 1');
  expect(img.getAttribute('src')).toMatch(/^blob:verified-/);
  expect((createURL.mock.calls.at(-1)?.[0] as Blob).size).toBe(3);
  expect(screen.queryByText(/Read stored message|View image attachment/)).toBeNull();
  expect(f.metadata).toHaveBeenLastCalledWith('root', 'image');
  expect(f.read).toHaveBeenLastCalledWith('root', 'image', expect.any(AbortSignal));
  fireEvent.click(screen.getByRole('button', { name: 'Copy message' }));
  await waitFor(() => expect(f.copy).toHaveBeenCalledWith('Please inspect **this screenshot**.'));
  expect(f.readBody).not.toHaveBeenCalled();
});
it('shows a retryable content failure in place without opening a stored-message dialog', async () => {
  const f = await fixture(); f.read.mockRejectedValue(new Error('Host temporarily unavailable'));
  render(f.app());
  expect(await screen.findByText('This content could not load')).toBeTruthy();
  f.read.mockImplementation(async (owner, id) => ({ reference: f.reference(owner, id), data_base64: 'AAAA' }));
  fireEvent.click(screen.getByRole('button', { name: 'Retry' }));
  expect(await screen.findByAltText('Attachment 1')).toBeTruthy();
  expect(f.readBody).not.toHaveBeenCalled();
});
it('waits for reconnection and uses the selected child scope for metadata and bytes', async () => {
  const f = await fixture(); const mounted = render(f.app([row], false, 'child'));
  expect(screen.getByText('Reconnect to load attachments.')).toBeTruthy();
  expect(f.read).not.toHaveBeenCalled();
  mounted.rerender(f.app([row], true, 'child'));
  expect(await screen.findByAltText('Attachment 1')).toBeTruthy();
  expect(f.metadata).toHaveBeenLastCalledWith('child', 'image');
  expect(f.read).toHaveBeenLastCalledWith('child', 'image', expect.any(AbortSignal));
});
it('aborts offscreen reads and never displays stale attachment bytes in the next revision', async () => {
  const f = await fixture(); let resolve!: (value: Awaited<ReturnType<typeof f.read>>) => void;
  f.read.mockImplementation(() => new Promise(done => { resolve = done; }));
  const mounted = render(f.app());
  await waitFor(() => expect(f.read).toHaveBeenCalled());
  const signal = f.read.mock.calls.at(-1)![2]!;
  f.read.mockImplementation(async (owner, id) => ({ reference: f.reference(owner, id), data_base64: 'AAAA' }));
  mounted.rerender(f.app([{ ...row, id: 'message:new', text: 'New revision', references: ['new-image'] }], true, 'root', '2'));
  expect(signal.aborted).toBe(true);
  await act(async () => resolve({ reference: f.reference('root', 'image'), data_base64: 'AAAA' }));
  expect(await screen.findByAltText('Attachment 1')).toBeTruthy();
  expect(screen.getByText('New revision')).toBeTruthy();
  expect(screen.queryByText('this screenshot')).toBeNull();
  mounted.unmount();
  await waitFor(() => expect(f.queries.getQueryCache().getAll()).toHaveLength(0));
  expect(revokeURL).toHaveBeenCalled();
});
it('keeps large tool output and typed attachments behind their explicit disclosure', async () => {
  const f = await fixture(); render(f.app([{ ...row, id: 'tool', role: 'tool', label: 'Tool output' }]));
  expect(f.metadata).not.toHaveBeenCalled(); expect(f.read).not.toHaveBeenCalled();
});
it('renders canonical assistant prose through safe Markdown without interpreting a JSON body', async () => {
  const f = await fixture();
  const text = '**Saved response**\n\n<script>bad()</script>';
  const mounted = render(f.app([{ ...row, role: 'assistant', text, references: [] }]));
  expect(await screen.findByText('Saved response')).toBeTruthy();
  expect(mounted.container.querySelector('script')).toBeNull();
  expect(f.read).not.toHaveBeenCalled();
  fireEvent.click(await screen.findByRole('button', { name: 'Copy response' }));
  await waitFor(() => expect(f.copy).toHaveBeenCalledWith(text));
});
it('keeps omitted large messages bounded and opens only the explicit owner-scoped reader', async () => {
  const f = await fixture();
  const gap: TimelineRow = { id: 'message:large', seq: '9007199254740993', role: 'history-gap', text: '', historyGap: { messageID: 'large', sequence: '9007199254740993', bytes: 434627, reason: 'message_too_large' } };
  const mounted = render(f.app([gap], false));
  expect(screen.getByRole('button', { name: 'Read large message' }).hasAttribute('disabled')).toBe(true);
  expect(f.loadGap).not.toHaveBeenCalled();
  mounted.rerender(f.app([gap]));
  fireEvent.click(screen.getByRole('button', { name: 'Read large message' }));
  await waitFor(() => expect(f.loadGap).toHaveBeenCalledExactlyOnceWith('large'));
  expect(f.read).not.toHaveBeenCalled();
  expect(screen.getByLabelText('Large message')).toBeTruthy();
});
it('rejects foreign metadata and corrupted bytes before creating an image URL', async () => {
  const f = await fixture(); f.metadata.mockImplementation(async (_owner, id) => f.reference('foreign', id));
  render(f.app());
  expect(await screen.findByText('This content could not load')).toBeTruthy();
  expect(f.read).not.toHaveBeenCalled(); expect(createURL).not.toHaveBeenCalled();
  f.metadata.mockImplementation(async (owner, id) => f.reference(owner, id));
  f.read.mockImplementation(async (owner, id) => ({ reference: f.reference(owner, id), data_base64: 'AQEB' }));
  fireEvent.click(screen.getByRole('button', { name: 'Retry attachments' }));
  expect(await screen.findByText('Content digest mismatch')).toBeTruthy();
  expect(createURL).not.toHaveBeenCalled();
});
it('shows embedded images immediately, including large dimensions, but does not load external URLs', () => {
  const mounted = render(<ImageAttachment image={{ ...image, width: 5000, height: 4000 }} />);
  expect(screen.getByAltText('Attached image')).toBeTruthy();
  fireEvent.error(screen.getByAltText('Attached image'));
  expect(screen.getByText('This image could not be decoded.')).toBeTruthy();
  mounted.rerender(<ImageAttachment image={{ url: 'https://example.com/private.png' }} />);
  expect(screen.queryByRole('img')).toBeNull();
  expect(screen.getByRole('link', { name: 'Open external image attachment' })).toBeTruthy();
});

it('places multiple thumbnails above the text, each with an independent loading state', async () => {
  const f = await fixture();
  const mounted = render(f.app([{ ...row, body: undefined, references: [], text: 'Compare these images', images: [image, { ...image, url: 'data:image/png;base64,BBBB' }] }]));
  const attachments = screen.getByLabelText('Message attachments');
  expect(attachments.nextElementSibling).toBe(mounted.container.querySelector('[data-user-bubble]'));
  const images = screen.getAllByAltText('Attached image');
  expect(images.map(img => img.getAttribute('src'))).toEqual([image.url, 'data:image/png;base64,BBBB']);
  expect(screen.getByRole('img', { name: 'Loading image 1 of 2' })).toBeTruthy();
  expect(screen.getByRole('img', { name: 'Loading image 2 of 2' })).toBeTruthy();
  fireEvent.load(images[0]);
  expect(screen.queryByRole('img', { name: 'Loading image 1 of 2' })).toBeNull();
  expect(screen.getByRole('img', { name: 'Loading image 2 of 2' })).toBeTruthy();
  fireEvent.error(images[1]);
  expect(screen.queryByRole('img', { name: 'Loading image 2 of 2' })).toBeNull();
  expect(screen.getByRole('button', { name: 'Image 2 of 2 could not be loaded' }).hasAttribute('disabled')).toBe(true);
  expect(screen.getByText('Compare these images')).toBeTruthy();
});

it('opens the selected original image in a dismissible preview and supports image-only messages', async () => {
  const f = await fixture();
  const mounted = render(f.app([{ ...row, body: undefined, references: [], text: '', images: [image, { ...image, url: 'data:image/png;base64,BBBB' }] }]));
  expect(mounted.container.querySelector('[data-user-bubble]')).toBeNull();
  const trigger = screen.getByRole('button', { name: 'Open image 2 of 2' });
  trigger.focus(); fireEvent.click(trigger);
  expect(await screen.findByRole('dialog', { name: 'Image 2 of 2' })).toBeTruthy();
  expect(screen.getByAltText('Image attachment preview').getAttribute('src')).toBe('data:image/png;base64,BBBB');
  fireEvent.click(screen.getByRole('button', { name: 'Close' }));
  await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
  await waitFor(() => expect(document.activeElement).toBe(trigger));
});

it('resets image loading and preview state when a thumbnail source changes', async () => {
  const app = (url: string) => <UIProvider><ImageAttachment image={{ ...image, url }} thumbnail /></UIProvider>;
  const mounted = render(app(image.url));
  fireEvent.load(screen.getByAltText('Attached image'));
  fireEvent.click(screen.getByRole('button', { name: 'Open image attachment' }));
  expect(await screen.findByRole('dialog')).toBeTruthy();
  mounted.rerender(app('data:image/png;base64,CCCC'));
  expect(screen.getByRole('img', { name: 'Loading image attachment' })).toBeTruthy();
  await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
  fireEvent.load(screen.getByAltText('Attached image'));
  expect(screen.queryByRole('img', { name: 'Loading image attachment' })).toBeNull();
});

it('recognizes already decoded thumbnails without waiting for another load event', () => {
  const complete = vi.spyOn(HTMLImageElement.prototype, 'complete', 'get').mockReturnValue(true);
  const width = vi.spyOn(HTMLImageElement.prototype, 'naturalWidth', 'get').mockReturnValue(640);
  try {
    render(<UIProvider><ImageAttachment image={image} thumbnail /></UIProvider>);
    expect(screen.queryByRole('img', { name: 'Loading image attachment' })).toBeNull();
    expect(screen.getByRole('button', { name: 'Open image attachment' }).hasAttribute('aria-busy')).toBe(false);
  } finally { complete.mockRestore(); width.mockRestore(); }
});

it('keeps internal screenshots collapsed even in Detailed mode and never styles them as user input', async () => {
  const f = await fixture('detailed');
  const mounted = render(f.app([{ ...row, role: 'internal', body: undefined, references: [], text: 'Internal screenshot caption', images: [image, image] }]));
  expect(screen.getByText('Activity details')).toBeTruthy();
  expect(screen.queryByText('Internal screenshot caption')).toBeNull();
  expect(screen.queryByAltText('Attached image')).toBeNull();
  expect(mounted.container.querySelector('[data-user-bubble]')).toBeNull();
  expect(screen.queryByLabelText('Your message')).toBeNull();
  expect(screen.queryByRole('button', { name: 'Copy message' })).toBeNull();
  fireEvent.click(screen.getByText('Activity details'));
  expect(screen.getAllByAltText('Attached image')).toHaveLength(2);
  expect(screen.queryByRole('button', { name: /^Open image/ })).toBeNull();
});

it('reads internal screenshot references only while their activity details are explicitly open', async () => {
  const f = await fixture();
  const mounted = render(f.app([{ ...row, role: 'internal' }]));
  expect(f.read).not.toHaveBeenCalled();
  fireEvent.click(screen.getByText('Activity details'));
  expect(await screen.findByAltText('Attachment 1')).toBeTruthy();
  expect(mounted.container.querySelector('[data-user-bubble]')).toBeNull();
  expect(f.read).toHaveBeenLastCalledWith('root', 'image', expect.any(AbortSignal));
  expect(f.readBody).not.toHaveBeenCalled();
  fireEvent.click(screen.getByText('Activity details'));
  expect(screen.queryByAltText('Attachment 1')).toBeNull();
  await waitFor(() => expect(f.queries.getQueryCache().getAll()).toHaveLength(0));
});
it('cancels an internal image transfer when its details close', async () => {
  const f = await fixture(); f.read.mockImplementation(() => new Promise(() => {}));
  render(f.app([{ ...row, role: 'internal' }]));
  fireEvent.click(screen.getByText('Activity details'));
  await waitFor(() => expect(f.read).toHaveBeenCalled());
  const signal = f.read.mock.calls.at(-1)![2]!;
  fireEvent.click(screen.getByText('Activity details'));
  expect(signal.aborted).toBe(true);
  expect(screen.queryByAltText('Attachment 1')).toBeNull();
});
