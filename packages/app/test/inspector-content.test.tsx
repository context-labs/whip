import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { webcrypto } from 'node:crypto';
import { Client, type ContentReference } from '@whip/sdk';
import { ThemeProvider, UIProvider } from '@whip/ui';
import { RuntimeContext } from '../src/context';
import type { AppRuntime } from '../src/runtime';
import { ContentRead } from '../src/details/content-read';

beforeEach(() => {
  vi.stubGlobal('matchMedia', () => ({ matches: false, addEventListener() {}, removeEventListener() {} }));
  vi.stubGlobal('crypto', webcrypto);
});
afterEach(() => vi.unstubAllGlobals());
async function fixture() {
  const data = new TextEncoder().encode('Exact owned text 🌍');
  const digest = Buffer.from(await webcrypto.subtle.digest('SHA-256', data)).toString('hex');
  const calls: string[] = [];
  const state = { foreign: false, hold: undefined as (() => Promise<void>) | undefined, size: String(data.length) };
  const reference = (owner: string): ContentReference => ({ id: 'same_handle', session_id: owner, size: state.size, digest, media_type: 'text/plain', created_at: '2026-09-27T12:00:00Z' });
  const client = await Client.connect(async request => {
    if (request.method === 'initialize') return { jsonrpc: '2.0', id: request.id, result: { major: 4, minor: 0, runtime_id: 'runtime', process_epoch: 'boot', network_client: false, builtins: [] } };
    calls.push(request.method);
    const p = request.params as { session_id: string };
    if (request.method === 'content.get') return { jsonrpc: '2.0', id: request.id, result: reference(state.foreign ? 'foreign' : p.session_id) };
    if (request.method === 'content.read') { await state.hold?.(); return { jsonrpc: '2.0', id: request.id, result: { reference: reference(p.session_id), data_base64: Buffer.from(data).toString('base64') } }; }
    throw new Error(request.method);
  }, { clientID: 'test' });
  const download = vi.fn(async (_data: Uint8Array, _name: string, _mediaType: string) => {}), runtime = { platform: { download, copy: vi.fn() } } as unknown as AppRuntime;
  const sessions = { root: client.session('root'), child: client.session('child') };
  const app = (owner: keyof typeof sessions = 'child', connected = true) => <RuntimeContext.Provider value={runtime}><UIProvider><ThemeProvider initialTheme="claude-code">
    <ContentRead session={sessions[owner]} connected={connected} reference="same_handle" label="Evidence" />
  </ThemeProvider></UIProvider></RuntimeContext.Provider>;
  return { app, state, calls, data, download };
}
it('content metadata and verified bytes load only on explicit read or download', async () => {
  const f = await fixture(); render(f.app()); expect(f.calls).toEqual([]);
  fireEvent.click(screen.getByRole('button', { name: 'Read evidence' }));
  await screen.findByRole('region', { name: 'Evidence' }); expect(screen.getByRole('region', { name: 'Evidence' }).textContent).toBe('Exact owned text 🌍');
  expect(f.calls).toEqual(['content.get', 'content.read']);
  fireEvent.click(screen.getByRole('button', { name: 'Download' })); await waitFor(() => expect(f.download).toHaveBeenCalledOnce());
  expect([...f.download.mock.calls[0]![0]]).toEqual([...f.data]);
  expect(f.download.mock.calls[0]!.slice(1)).toEqual(['whip-content', 'text/plain']);
});
it('foreign content and oversized metadata fail before a body read', async () => {
  const f = await fixture(); f.state.foreign = true; render(f.app());
  fireEvent.click(screen.getByRole('button', { name: 'Read evidence' })); await screen.findByText('Content identity mismatch');
  expect(f.calls).toEqual(['content.get']);
  f.state.foreign = false; f.state.size = String(5 << 20);
  fireEvent.click(screen.getByRole('button', { name: 'Download' })); await screen.findByText('Content exceeds read limit');
  expect(f.calls).toEqual(['content.get', 'content.get']); expect(f.download).not.toHaveBeenCalled();
});
it('a same-named child reference cannot publish a late body after selection changes', async () => {
  const f = await fixture(); let release!: () => void;
  f.state.hold = () => new Promise(resolve => { release = resolve; });
  const mounted = render(f.app('child'));
  fireEvent.click(screen.getByRole('button', { name: 'Read evidence' })); await waitFor(() => expect(f.calls).toContain('content.read'));
  mounted.rerender(f.app('root'));
  await act(async () => { release(); await new Promise(resolve => setTimeout(resolve, 0)); });
  expect(screen.queryByRole('region', { name: 'Evidence' })).toBeNull(); expect(screen.getByRole('button', { name: 'Read evidence' }).hasAttribute('disabled')).toBe(false);
  mounted.rerender(f.app('root', false)); expect(screen.getByRole('button', { name: 'Download' }).hasAttribute('disabled')).toBe(true);
});
