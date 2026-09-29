import { act, fireEvent, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { createHash, webcrypto } from 'node:crypto';
import type { ContentReference } from '@whip/sdk';
import { InputAttachment } from '../src/input-attachment';
import { providerFixture } from './provider-fixture';

beforeEach(() => {
  vi.stubGlobal('crypto', webcrypto);
  vi.stubGlobal('matchMedia', () => ({ matches: false, addEventListener() {}, removeEventListener() {} }));
  vi.stubGlobal('URL', class extends URL { static createObjectURL() { return 'blob:fixture'; } static revokeObjectURL() {} });
});
afterEach(() => vi.unstubAllGlobals());
async function fixture(size = (1 << 20) + 32, image = false) {
  const f = await providerFixture();
  const data = Buffer.alloc(size, 'a');
  const reference = (owner = 'child', id = 'attachment'): ContentReference => ({ id, session_id: owner, media_type: image ? 'image/png' : 'text/plain', digest: createHash('sha256').update(data).digest('hex'), size: String(data.byteLength), created_at: '2026-09-28T00:00:00Z' });
  const download = vi.fn(async (_data: Uint8Array, _name: string, _type: string) => {});
  Object.assign(f.runtime.platform, { download });
  f.data.handlers['content.read'] = request => {
    if (request.method !== 'content.read') throw new Error('Wrong read');
    return { reference: reference(request.params.session_id, request.params.reference_id), data_base64: data.toString('base64') };
  };
  const app = (owner = 'child', connected = true, id = 'attachment', client = f.client) => <InputAttachment client={client} runtimeId={client.runtimeID} rootId="root" agentId={owner} file={reference(owner, id)} name="Attachment 1" image={image} connected={connected} />;
  return { ...f, app, reference, bytes: data, download };
}
it('refuses oversized text preview before reading bytes, then explicitly downloads verified scoped content', async () => {
  const f = await fixture(); f.mount(f.app()); expect(f.count('content.read')).toBe(0);
  fireEvent.click(screen.getByRole('button', { name: 'Preview Attachment 1' }));
  const dialog = await screen.findByRole('dialog', { name: 'Attachment 1' });
  expect(await within(dialog).findByText('Content exceeds read limit')).toBeTruthy();
  expect(f.count('content.read')).toBe(0);
  fireEvent.click(within(dialog).getByRole('button', { name: 'Download' }));
  await waitFor(() => expect(f.download).toHaveBeenCalledOnce());
  expect(Buffer.from(f.download.mock.calls[0]![0])).toEqual(f.bytes);
  expect(f.download.mock.calls[0]!.slice(1)).toEqual(['Attachment 1', 'text/plain']);
  expect(f.calls.find(call => call.method === 'content.read')?.params).toEqual({ session_id: 'child', reference_id: 'attachment' });
});
it('refuses content above4MiB before a download read and keeps failure in the attachment dialog', async () => {
  const f = await fixture((4 << 20) + 1); f.mount(f.app());
  fireEvent.click(screen.getByRole('button', { name: 'Preview Attachment 1' }));
  const dialog = await screen.findByRole('dialog', { name: 'Attachment 1' });
  fireEvent.click(within(dialog).getByRole('button', { name: 'Download' }));
  expect(await within(dialog).findByText('Could not download attachment')).toBeTruthy();
  expect(f.count('content.read')).toBe(0); expect(f.download).not.toHaveBeenCalled();
});
it.each(['owner', 'reference', 'client', 'disconnect', 'close', 'unmount'] as const)('cancels an exact in-flight download on %s change without publishing late bytes', async change => {
  const f = await fixture();
  let release!: () => void, signal: AbortSignal | undefined;
  f.data.handlers['content.read'] = async (_request, value) => {
    signal = value; await new Promise<void>(resolve => { release = resolve; });
    return { reference: f.reference(), data_base64: f.bytes.toString('base64') };
  };
  const mounted = f.mount(f.app());
  fireEvent.click(screen.getByRole('button', { name: 'Preview Attachment 1' }));
  fireEvent.click(await screen.findByRole('button', { name: 'Download' }));
  await waitFor(() => expect(signal).toBeTruthy());
  if (change === 'unmount') mounted.unmount();
  else if (change === 'close') fireEvent.keyDown(screen.getByRole('dialog', { name: 'Attachment 1' }), { key: 'Escape' });
  else {
    const client = change === 'client' ? (await providerFixture()).client : f.client;
    mounted.rerender(f.wrap(f.app(change === 'owner' ? 'root' : 'child', change !== 'disconnect', change === 'reference' ? 'replacement' : 'attachment', client)));
  }
  await waitFor(() => expect(signal?.aborted).toBe(true));
  await act(async () => { release(); await new Promise(resolve => setTimeout(resolve, 0)); });
  expect(f.download).not.toHaveBeenCalled();
});
it('keeps image loading at the existing4MiB ceiling', async () => {
  const f = await fixture((1 << 20) + 32, true); f.mount(f.app());
  const image = await screen.findByAltText('Attachment 1');
  expect(image.getAttribute('src')).toBe('blob:fixture'); expect(f.count('content.read')).toBe(1);
  expect(f.download).not.toHaveBeenCalled();
});
it('does not save bytes when the native response fails immutable digest verification', async () => {
  const f = await fixture(3);
  f.data.handlers['content.read'] = () => ({ reference: f.reference(), data_base64: Buffer.from('bad').toString('base64') });
  f.mount(f.app());
  fireEvent.click(screen.getByRole('button', { name: 'Preview Attachment 1' }));
  const dialog = await screen.findByRole('dialog', { name: 'Attachment 1' });
  await within(dialog).findByText('Content digest mismatch');
  fireEvent.click(within(dialog).getByRole('button', { name: 'Download' }));
  expect(await within(dialog).findByText('Could not download attachment')).toBeTruthy();
  expect(f.download).not.toHaveBeenCalled(); expect(f.count('content.read')).toBe(2);
});
