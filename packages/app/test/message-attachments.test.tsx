import type { ReactNode } from 'react';
import { act, fireEvent, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { createHash, webcrypto } from 'node:crypto';
import type { ContentReference } from '@whip/sdk';
import { Timeline, type TimelineRow } from '../src/timeline';
import { authoredInputId } from '../src/input-presentation';
import { providerFixture } from './provider-fixture';

// The production timeline owns attachment reconciliation; viewport geometry is
// irrelevant to whether the same exact input keeps its open dialog mounted.
vi.mock('../src/reading-list', () => ({
  ReadingList: ({ rows, renderRow }: { rows: { id: string }[]; renderRow(row: { id: string }): ReactNode }) =>
    <div>{rows.map(row => <div key={row.id}>{renderRow(row)}</div>)}</div>,
}));
beforeEach(() => {
  vi.stubGlobal('crypto', webcrypto);
  vi.stubGlobal('matchMedia', () => ({ matches: false, addEventListener() {}, removeEventListener() {} }));
});
afterEach(() => vi.unstubAllGlobals());
async function fixture() {
  const f = await providerFixture();
  const bytes = Buffer.from('Original scoped attachment');
  const reference: ContentReference = { id: 'reference', session_id: 'child', media_type: 'text/plain', digest: createHash('sha256').update(bytes).digest('hex'), size: String(bytes.length), created_at: '2026-09-28T00:00:00Z' };
  f.data.handlers['content.get'] = () => reference;
  f.data.handlers['content.read'] = () => ({ reference, data_base64: bytes.toString('base64') });
  const id = authoredInputId('child', { client_id: 'human', request_id: 'original' });
  const local: TimelineRow = { id, role: 'user', text: 'Read this', inputAttachments: [reference], delivery: 'Accepted' };
  const committed: TimelineRow = { id, role: 'user', text: 'Read this', references: [reference.id], seq: '9007199254740993', memberIds: ['message:committed'] };
  const app = (rows: TimelineRow[], owner = 'child', client = f.client) => <Timeline rows={rows} hasMore={false} loadOlder={async () => {}} readBody={() => {}}
    messageScope={{ client, rootId: 'root', agentId: owner }} />;
  return { ...f, reference, bytes, local, committed, app };
}
it('keeps the same-input attachment dialog and verified bytes across canonical confirmation', async () => {
  const f = await fixture(); const mounted = f.mount(f.app([f.local]));
  fireEvent.click(screen.getByRole('button', { name: 'Preview Attachment 1' }));
  const dialog = await screen.findByRole('dialog', { name: 'Attachment 1' });
  await within(dialog).findByText(f.bytes.toString());
  expect(f.count('content.read')).toBe(1);
  mounted.rerender(f.wrap(f.app([f.committed])));
  await waitFor(() => expect(screen.getByRole('dialog', { name: 'Attachment 1' })).toBe(dialog));
  expect(within(dialog).getByText(f.bytes.toString())).toBeTruthy();
  expect(f.count('content.read')).toBe(1);
  expect(f.count('content.get')).toBe(0);
});
it('keeps an in-flight exact scoped preview alive while its input is confirmed', async () => {
  const f = await fixture();
  let release!: () => void, signal: AbortSignal | undefined;
  f.data.handlers['content.read'] = async (_request, value) => {
    signal = value; await new Promise<void>(resolve => { release = resolve; });
    return { reference: f.reference, data_base64: f.bytes.toString('base64') };
  };
  const mounted = f.mount(f.app([f.local]));
  fireEvent.click(screen.getByRole('button', { name: 'Preview Attachment 1' }));
  const dialog = await screen.findByRole('dialog', { name: 'Attachment 1' });
  await waitFor(() => expect(signal).toBeTruthy());
  mounted.rerender(f.wrap(f.app([f.committed])));
  expect(signal?.aborted).toBe(false);
  await act(async () => { release(); });
  await within(dialog).findByText(f.bytes.toString());
  expect(screen.getByRole('dialog', { name: 'Attachment 1' })).toBe(dialog);
  expect(f.count('content.read')).toBe(1);
});
it.each(['owner', 'reference', 'client', 'retired'] as const)('closes an explicit preview on %s replacement instead of transferring it to another scope', async change => {
  const f = await fixture(); const mounted = f.mount(f.app([f.local]));
  fireEvent.click(screen.getByRole('button', { name: 'Preview Attachment 1' }));
  await within(await screen.findByRole('dialog', { name: 'Attachment 1' })).findByText(f.bytes.toString());
  const client = change === 'client' ? (await providerFixture({ runtimeID: 'foreign-host' })).client : f.client;
  f.data.handlers['content.get'] = request => {
    if (request.method !== 'content.get') throw new Error('Wrong metadata request');
    return { ...f.reference, id: request.params.reference_id, session_id: request.params.session_id };
  };
  const rows = change === 'retired' ? [] : [{ ...f.committed, references: [change === 'reference' ? 'replacement' : f.reference.id] }];
  mounted.rerender(f.wrap(f.app(rows, change === 'owner' ? 'foreign-child' : 'child', client)));
  await waitFor(() => expect(screen.queryByRole('dialog', { name: 'Attachment 1' })).toBeNull());
  expect(f.count('content.read')).toBe(1);
});
it('does not seed attachment metadata belonging to another owner', async () => {
  const f = await fixture();
  f.data.handlers['content.get'] = () => { throw new Error('Foreign content unavailable'); };
  f.mount(f.app([f.local], 'foreign-child'));
  expect(await screen.findByText('Foreign content unavailable')).toBeTruthy();
  expect(screen.queryByRole('button', { name: 'Preview Attachment 1' })).toBeNull();
  expect(f.count('content.read')).toBe(0);
  expect(f.calls.find(call => call.method === 'content.get')?.params).toEqual({ session_id: 'foreign-child', reference_id: 'reference' });
});
it('retires a pending preview read when the exact authored input leaves the retained window', async () => {
  const f = await fixture();
  let release!: () => void, signal: AbortSignal | undefined;
  f.data.handlers['content.read'] = async (_request, value) => {
    signal = value; await new Promise<void>(resolve => { release = resolve; });
    return { reference: f.reference, data_base64: f.bytes.toString('base64') };
  };
  const mounted = f.mount(f.app([f.local]));
  fireEvent.click(screen.getByRole('button', { name: 'Preview Attachment 1' }));
  await waitFor(() => expect(signal).toBeTruthy());
  mounted.rerender(f.wrap(f.app([])));
  await waitFor(() => expect(signal?.aborted).toBe(true));
  await act(async () => { release(); });
  expect(screen.queryByRole('dialog', { name: 'Attachment 1' })).toBeNull();
  expect(screen.queryByText(f.bytes.toString())).toBeNull();
});
