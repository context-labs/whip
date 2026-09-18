import { StrictMode } from 'react';
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient } from '@tanstack/react-query';
import { afterEach, expect, it, vi } from 'vitest';
import type { WhipClient } from '@whip/sdk';
import { UIProvider } from '@whip/ui';
import { RuntimeContext } from '../src/context';
import type { AppRuntime } from '../src/runtime';
import { ImageAttachment, Timeline, type TimelineRow } from '../src/timeline';

vi.mock('../src/reading-list', () => ({
  ReadingList: ({ rows, renderRow }: { rows: TimelineRow[]; renderRow(row: TimelineRow): React.ReactNode }) =>
    <div>{rows.map(row => <div key={row.id}>{renderRow(row)}</div>)}</div>,
}));
const image = { url: 'data:image/png;base64,AAAA', width: 1754, height: 1902 };
const stored = { role: 'user', content: [{ type: 'text', text: 'Please inspect **this screenshot**.' }, { type: 'image_url', image_url: { url: image.url }, w: image.width, h: image.height }] };
const row: TimelineRow = { id: 'h:1:3', seq: 3, role: 'user', text: '', body: { reference_id: 'stored-image', size: '434627', digest: 'a'.repeat(64), media_type: 'application/json' } };
const clients: QueryClient[] = [];
afterEach(() => { cleanup(); clients.splice(0).forEach(client => client.clear()); });
function fixture(toolDensity = 'compact') {
  const queries = new QueryClient({ defaultOptions: { queries: { retry: false, networkMode: 'always' } } });
  clients.push(queries);
  const readJSON = vi.fn(async (_options: { signal?: AbortSignal; maxBytes: number }): Promise<unknown> => stored);
  const content = vi.fn(() => ({ readJSON }));
  const client = { getSnapshot: () => ({ info: { runtime_id: 'host' } }), content } as unknown as WhipClient;
  const copy = vi.fn(async () => {}), readBody = vi.fn();
  const runtime = { queries, platform: { copy }, report: vi.fn(), subscribe: () => () => {}, getSnapshot: () => ({ preferences: { toolDensity } }) } as unknown as AppRuntime;
  const app = (rows = [row], connected = true, agentId = 'root', revision = '1') => <StrictMode><RuntimeContext.Provider value={runtime}><UIProvider>
    <Timeline rows={rows} hasMore={false} loadOlder={async () => {}} readBody={readBody} connected={connected} historyRevision={revision}
      messageScope={{ client, rootId: 'root', agentId }} />
  </UIProvider></RuntimeContext.Provider></StrictMode>;
  return { app, content, readJSON, copy, readBody, queries };
}
it('automatically restores a large historical message, its image and the original copy text', async () => {
  const f = fixture(); render(f.app());
  expect(await screen.findByText('this screenshot')).toBeTruthy();
  const img = screen.getByAltText('Attached image');
  expect(img.getAttribute('src')).toBe(image.url);
  expect(img.getAttribute('width')).toBe('1754');
  expect(img.getAttribute('height')).toBe('1902');
  expect(screen.queryByText(/Read stored message|View image attachment/)).toBeNull();
  expect(f.content).toHaveBeenLastCalledWith(row.body, { client: expect.anything(), rootId: 'root', agentId: 'root' });
  fireEvent.click(screen.getByRole('button', { name: 'Copy message', exact: true }));
  await waitFor(() => expect(f.copy).toHaveBeenCalledWith('Please inspect **this screenshot**.'));
  expect(f.readBody).not.toHaveBeenCalled();
});
it('shows a retryable failure in place and retries without opening a stored-message dialog', async () => {
  const f = fixture(); f.readJSON.mockRejectedValue(new Error('Host temporarily unavailable'));
  render(f.app());
  expect(await screen.findByText('Could not load message')).toBeTruthy();
  f.readJSON.mockResolvedValue(stored);
  fireEvent.click(screen.getByRole('button', { name: 'Retry' }));
  expect(await screen.findByText('this screenshot')).toBeTruthy();
  expect(f.readBody).not.toHaveBeenCalled();
});
it('waits for reconnection and uses the selected child scope', async () => {
  const f = fixture(); const mounted = render(f.app([row], false, 'child'));
  expect(screen.getByText('Reconnect to load this message.')).toBeTruthy();
  expect(f.readJSON).not.toHaveBeenCalled();
  mounted.rerender(f.app([row], true, 'child'));
  expect(await screen.findByText('this screenshot')).toBeTruthy();
  expect(f.content).toHaveBeenLastCalledWith(row.body, expect.objectContaining({ rootId: 'root', agentId: 'child' }));
});
it('aborts offscreen reads and never displays a stale revision in the next message', async () => {
  const f = fixture(); let resolve!: (value: unknown) => void;
  f.readJSON.mockImplementation(() => new Promise(done => { resolve = done; }));
  const mounted = render(f.app());
  const signal = f.readJSON.mock.calls.at(-1)![0].signal!;
  f.readJSON.mockResolvedValue({ role: 'user', content: 'New revision' });
  mounted.rerender(f.app([{ ...row, id: 'h:2:3' }], true, 'root', '2'));
  expect(signal.aborted).toBe(true);
  await act(async () => resolve(stored));
  expect(await screen.findByText('New revision')).toBeTruthy();
  expect(screen.queryByText('this screenshot')).toBeNull();
  mounted.unmount();
  await waitFor(() => expect(f.queries.getQueryCache().getAll()).toHaveLength(0));
});
it('keeps large tool output behind its existing explicit disclosure', () => {
  const f = fixture(); render(f.app([{ ...row, id: 'tool', role: 'tool', label: 'Tool output' }]));
  expect(f.readJSON).not.toHaveBeenCalled();
});
it('also renders referenced assistant prose using the normal safe Markdown renderer', async () => {
  const f = fixture(); f.readJSON.mockResolvedValue({ role: 'assistant', content: '**Saved response**\n\n<script>bad()</script>' });
  const mounted = render(f.app([{ ...row, role: 'assistant' }]));
  expect(await screen.findByText('Saved response')).toBeTruthy();
  expect(mounted.container.querySelector('script')).toBeNull();
  expect(screen.queryByText(/Read stored message/)).toBeNull();
  const copy = await screen.findByRole('button', { name: 'Copy response', exact: true });
  fireEvent.click(copy);
  await waitFor(() => expect(f.copy).toHaveBeenCalledWith('**Saved response**\n\n<script>bad()</script>'));
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

it('places multiple thumbnails above the text, each with an independent loading state', () => {
  const f = fixture();
  const mounted = render(f.app([{ ...row, body: undefined, text: 'Compare these images', images: [image, { ...image, url: 'data:image/png;base64,BBBB' }] }]));
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
  const f = fixture();
  const mounted = render(f.app([{ ...row, body: undefined, text: '', images: [image, { ...image, url: 'data:image/png;base64,BBBB' }] }]));
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

it('keeps internal screenshots collapsed even in Detailed mode and never styles them as user input', () => {
  const f = fixture('detailed');
  const mounted = render(f.app([{ ...row, role: 'internal', body: undefined, text: 'Internal screenshot caption', images: [image, image] }]));
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

it('reads large internal screenshot bodies only while their activity details are explicitly open', async () => {
  const f = fixture();
  const mounted = render(f.app([{ ...row, role: 'internal' }]));
  expect(f.readJSON).not.toHaveBeenCalled();
  expect(screen.queryByText('Loading message…')).toBeNull();
  fireEvent.click(screen.getByText('Activity details'));
  expect(await screen.findByAltText('Attached image')).toBeTruthy();
  expect(mounted.container.querySelector('[data-user-bubble]')).toBeNull();
  expect(f.readJSON).toHaveBeenLastCalledWith(expect.objectContaining({ maxBytes: 64 << 20 }));
  expect(f.readBody).not.toHaveBeenCalled();
  fireEvent.click(screen.getByText('Activity details'));
  expect(screen.queryByAltText('Attached image')).toBeNull();
  await waitFor(() => expect(f.queries.getQueryCache().getAll()).toHaveLength(0));
});

it('cancels an internal image transfer when its details close', async () => {
  const f = fixture(); f.readJSON.mockImplementation(() => new Promise(() => {}));
  render(f.app([{ ...row, role: 'internal' }]));
  fireEvent.click(screen.getByText('Activity details'));
  const signal = f.readJSON.mock.calls.at(-1)![0].signal!;
  fireEvent.click(screen.getByText('Activity details'));
  expect(signal.aborted).toBe(true);
  expect(screen.queryByText('Loading message…')).toBeNull();
});
