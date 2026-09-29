import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { createHash } from 'node:crypto';
import { Client } from '@whip/sdk';
import { UIProvider } from '@whip/ui';
import type { Message } from '@whip/protocol';
import { RuntimeContext } from '../src/context';
import type { AppRuntime } from '../src/runtime';
import { MessageRow } from '../src/timeline';
import { MessageAttachments } from '../src/message-attachments';
import { messagePresentation } from '../src/conversation-rows';

afterEach(() => { cleanup(); vi.unstubAllGlobals(); vi.restoreAllMocks(); });
it('renders persisted scoped design references, loads image once, and reads context only on disclosure', async () => {
  const raw = '{"selector":"#heading","styles":{"fontSize":"40px"}}';
  const bodies = { context: Buffer.from(raw), image: Buffer.from([1, 2, 3]) };
  const reads: string[] = [];
  const client = await Client.connect(async request => {
    if (request.method === 'initialize') return { jsonrpc: '2.0', id: request.id, result: { major: 4, minor: 0, runtime_id: 'runtime', process_epoch: 'epoch', network_client: false, builtins: [] } };
    const params = request.params as { session_id: string; reference_id: keyof typeof bodies };
    expect(params.session_id).toBe('child');
    const body = bodies[params.reference_id];
    const reference = { session_id: 'child', id: params.reference_id, digest: createHash('sha256').update(body).digest('hex'), size: String(body.length), media_type: params.reference_id === 'image' ? 'image/png' : 'text/plain', created_at: '2026-01-01T00:00:00Z' };
    if (request.method === 'content.get') return { jsonrpc: '2.0', id: request.id, result: reference };
    expect(request.method).toBe('content.read'); reads.push(params.reference_id);
    return { jsonrpc: '2.0', id: request.id, result: { reference, data_base64: body.toString('base64') } };
  }, { clientID: 'reader' });
  const create = vi.fn(() => 'blob:design'), revoke = vi.fn();
  vi.stubGlobal('URL', class extends URL { static createObjectURL = create; static revokeObjectURL = revoke; });
  const parts: Message['parts'] = [{ type: 'text', text: 'Make this heading smaller' }, { type: 'content', reference_id: 'context' }, { type: 'content', reference_id: 'image' }];
  const presentation = messagePresentation(parts, { context_attachment_id: 'context', screenshot_attachment_id: 'image', elements: [{ label: 'h1 · Heading' }], element_count: 1, context_part_index: 1, screenshot_part_index: 2, page_url: 'https://example.com/settings' });
  const queries = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const { container, unmount } = render(<QueryClientProvider client={queries}><UIProvider><RuntimeContext.Provider value={{ report: vi.fn(), platform: { copy: vi.fn() } } as unknown as AppRuntime}>
    <MessageRow row={{ id: 'persisted', role: 'user', ...presentation }} readBody={vi.fn()}
      attachments={<MessageAttachments {...presentation} client={client} rootId="root" agentId="child" connected />} />
  </RuntimeContext.Provider></UIProvider></QueryClientProvider>);
  expect(container.querySelector('[data-user-bubble]')?.textContent).toBe('Make this heading smaller');
  await screen.findByText('Design Mode');
  await waitFor(() => expect(create).toHaveBeenCalledTimes(1));
  expect(reads).toEqual(['image']);
  expect(screen.queryByText(raw)).toBeNull();
  expect(screen.getAllByRole('img', { name: 'Attachment 1' })).toHaveLength(1);
  fireEvent.click(screen.getByRole('button', { name: 'Design Mode details' }));
  await screen.findByRole('dialog', { name: 'Captured page context' });
  expect(screen.getByText('h1 · Heading')).toBeTruthy();
  fireEvent.click(screen.getByText('Raw context'));
  await screen.findByText(raw);
  expect(reads).toEqual(['image', 'context']);
  unmount(); expect(revoke).toHaveBeenCalledWith('blob:design'); queries.clear();
});
