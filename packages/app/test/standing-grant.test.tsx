import userEvent from '@testing-library/user-event';
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { Client, DeliveryError, type Grant, type Operations, type SessionRecord } from '@whip/sdk';
import { assertValid, type Request } from '@whip/protocol';
import { ThemeProvider, UIProvider } from '@whip/ui';
import { webcrypto } from 'node:crypto';
import fixtures from '../../protocol/schema/fixtures.json';
import { StandingGrant } from '../src/details/standing-grant';

beforeEach(() => {
  vi.stubGlobal('crypto', webcrypto);
  vi.stubGlobal('matchMedia', () => ({ matches: false, addEventListener() {}, removeEventListener() {} }));
});
afterEach(() => vi.unstubAllGlobals());
const at = '2026-09-28T12:00:00Z';
function selected(id: string, parent: string | null): SessionRecord {
  const value: unknown = structuredClone(fixtures.find(item => item.type === 'Session' && item.valid)!.value);
  assertValid('Session', value);
  return { ...value, id, parent_id: parent };
}
function grant(params: Operations['grants.create']['params']): Grant {
  return { ...params, issuer_id: params.issuer_id ?? null, operation_id: null, created_at: at, revoked_at: null };
}
async function fixture(owner = 'root', parent: string | null = null) {
  const calls: Request[] = [], handlers: Record<string, (request: Request) => unknown | Promise<unknown>> = {};
  const client = await Client.connect(async request => {
    calls.push(structuredClone(request));
    let result: unknown;
    if (request.method === 'initialize') result = { major: 4, minor: 0, runtime_id: 'runtime', process_epoch: 'epoch', network_client: false, builtins: [] };
    else if (handlers[request.method]) result = await handlers[request.method]!(request);
    else if (request.method === 'grants.list') result = { items: [] };
    else if (request.method === 'grants.create') { assertValid('CreateGrantParams', request.params); result = grant(request.params); }
    else throw new Error('Unexpected method ' + request.method);
    return { jsonrpc: '2.0', id: request.id, result };
  }, { clientID: 'grant-test' });
  const queries = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } });
  afterEach(() => queries.clear());
  const onCreated = vi.fn(async () => {});
  const props = { client, session: client.session(owner), selected: selected(owner, parent), connected: true, onCreated };
  const element = (next = props) => <QueryClientProvider client={queries}><ThemeProvider><UIProvider><StandingGrant {...next} /></UIProvider></ThemeProvider></QueryClientProvider>;
  return { client, props, calls, handlers, onCreated, element };
}
function fill() {
  fireEvent.change(screen.getByLabelText('Exact capability'), { target: { value: 'files.read' } });
  fireEvent.change(screen.getByLabelText('Exact resource'), { target: { value: '/owned/workspace' } });
}
it('creates one exact selected-root standing grant only after explicit confirmation', async () => {
  const f = await fixture(); render(f.element()); fill();
  expect(f.calls.filter(call => call.method === 'grants.create')).toHaveLength(0);
  fireEvent.click(screen.getByRole('button', { name: 'Create standing grant' }));
  await screen.findByText('Standing grant created.');
  const requests = f.calls.filter(call => call.method === 'grants.create');
  expect(requests).toHaveLength(1);
  expect(requests[0]?.params).toEqual({ id: expect.any(String), session_id: 'root', capability: 'files.read', resource: '/owned/workspace' });
  expect(f.onCreated).toHaveBeenCalledTimes(1);
});
it('retains exact identity and payload after uncertain delivery, with no automatic retry', async () => {
  const f = await fixture(); let attempts = 0;
  f.handlers['grants.create'] = request => {
    assertValid('CreateGrantParams', request.params);
    if (++attempts === 1) throw new DeliveryError('Lost acknowledgement');
    return { ...grant(request.params), revoked_at: at };
  };
  render(f.element()); fill(); fireEvent.click(screen.getByRole('button', { name: 'Create standing grant' }));
  await screen.findByText('Standing grant needs checking');
  expect(attempts).toBe(1); expect(screen.getByLabelText('Exact resource').hasAttribute('disabled')).toBe(true);
  fireEvent.click(screen.getByRole('button', { name: 'Retry same grant' }));
  await screen.findByText('This exact grant was created and has since been revoked.');
  const requests = f.calls.filter(call => call.method === 'grants.create');
  expect(requests).toHaveLength(2); expect(requests[1]?.params).toEqual(requests[0]?.params);
});
it('rejects a foreign acknowledgement and preserves the original request for inspection', async () => {
  const f = await fixture();
  f.handlers['grants.create'] = request => { assertValid('CreateGrantParams', request.params); return { ...grant(request.params), session_id: 'foreign' }; };
  render(f.element()); fill(); fireEvent.click(screen.getByRole('button', { name: 'Create standing grant' }));
  await screen.findByText('Standing grant needs checking');
  expect(f.onCreated).not.toHaveBeenCalled(); expect(screen.queryByText('Standing grant created.')).toBeNull();
});
it('permits child delegation only from an unrevoked standing direct-parent grant', async () => {
  const f = await fixture('child', 'parent');
  const issuer = grant({ id: 'parent-grant', session_id: 'parent', capability: 'mcp.call', resource: 'exact_server_tool_hash' });
  f.handlers['grants.list'] = request => {
    expect(request.params).toEqual({ session_id: 'parent', limit: 32 });
    return { items: [issuer, { ...issuer, id: 'revoked', revoked_at: at }, { ...issuer, id: 'one-use', operation_id: 'operation' }] };
  };
  render(f.element());
  const select = await screen.findByRole('combobox', { name: 'Parent standing grant' });
  await waitFor(() => expect(f.calls.filter(call => call.method === 'grants.list')).toHaveLength(1));
  expect(screen.queryByLabelText('Exact capability')).toBeNull();
  const user = userEvent.setup();
  await user.click(select);
  await user.click(await screen.findByRole('option', { name: /mcp.call.*exact_server_tool_hash.*parent-grant/ }));
  expect(screen.queryByRole('option', { name: /revoked|one-use/ })).toBeNull();
  fireEvent.click(screen.getByRole('button', { name: 'Create standing grant' }));
  await screen.findByText('Standing grant created.');
  expect(f.calls.find(call => call.method === 'grants.create')?.params).toEqual({ id: expect.any(String), session_id: 'child', issuer_id: 'parent-grant', capability: 'mcp.call', resource: 'exact_server_tool_hash' });
});
it('refuses a foreign parent page and invalid byte bounds without sending authority', async () => {
  const f = await fixture('child', 'parent');
  f.handlers['grants.list'] = () => ({ items: [grant({ id: 'wrong', session_id: 'foreign', capability: 'files.read', resource: '/workspace' })] });
  const mounted = render(f.element());
  await screen.findByText('Could not load this resource');
  expect(screen.getByRole('button', { name: 'Create standing grant' }).hasAttribute('disabled')).toBe(true);
  mounted.rerender(f.element({ ...f.props, session: f.client.session('root'), selected: selected('root', null) }));
  fill(); fireEvent.change(screen.getByLabelText('Exact resource'), { target: { value: '界'.repeat(1400) } });
  expect(screen.getByRole('button', { name: 'Create standing grant' }).hasAttribute('disabled')).toBe(true);
  expect(f.calls.filter(call => call.method === 'grants.create')).toHaveLength(0);
});
it('retires late acknowledgements when the selected owner changes', async () => {
  const f = await fixture(); let resolve!: (value: Grant) => void, captured!: Operations['grants.create']['params'];
  f.handlers['grants.create'] = request => { assertValid('CreateGrantParams', request.params); captured = request.params; return new Promise(done => { resolve = done; }); };
  const mounted = render(f.element()); fill(); fireEvent.click(screen.getByRole('button', { name: 'Create standing grant' }));
  await waitFor(() => expect(resolve).toBeDefined());
  mounted.rerender(f.element({ ...f.props, session: f.client.session('other'), selected: selected('other', null) }));
  await act(async () => resolve(grant(captured)));
  expect(screen.queryByText('Standing grant created.')).toBeNull(); expect(f.onCreated).not.toHaveBeenCalled();
});
