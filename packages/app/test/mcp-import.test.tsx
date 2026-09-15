import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { ThemeProvider, UIProvider } from '@whip/ui';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import type { WhipClient } from '@whip/sdk';
import type { MCPImportApplyParams, MCPImportCandidatesResult } from '@whip/protocol';
import type { ReactNode } from 'react';
import { RuntimeContext } from '../src/context';
import type { AppRuntime } from '../src/runtime';
import { MCPImportScreen, caveat, shouldOffer, sortCandidates, type MCPImportCandidate } from '../src/mcp-import';

const disabled = (element: Element) => element.matches('[aria-disabled="true"], [data-disabled], :disabled');
beforeEach(() => vi.stubGlobal('matchMedia', () => ({ matches: false, addEventListener() {}, removeEventListener() {} })));
afterEach(() => vi.unstubAllGlobals());

const candidate = (name: string, state: MCPImportCandidate['state'], source = 'codex', extra: Partial<MCPImportCandidate> = {}): MCPImportCandidate =>
  ({ name, source, source_path: `/home/u/${source}`, transport: 'http', state, ...extra });
const found: MCPImportCandidatesResult = {
  offered: false, config_path: '/home/u/.whipcode/config.json',
  candidates: [
    candidate('paper', 'importable'), candidate('ahrefs', 'native'), candidate('node_repl', 'excluded'),
    candidate('computer-use', 'disabled'), candidate('exa', 'importable', 'claude'),
    candidate('figma', 'unsupported', 'opencode', { note: "needs a sign-in Whip can't do yet" }), candidate('executor', 'native'),
  ],
};

function fixture(data: MCPImportCandidatesResult = found, operations = ['mcp.import.candidates', 'mcp.import.apply']) {
  const queries = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } });
  // The fake host remembers the answer the way the daemon does, so a refetch
  // after apply reports offered: true.
  let server = data;
  const candidates = vi.fn(async () => server);
  const apply = vi.fn(async (params: MCPImportApplyParams) => { server = { ...server, offered: true }; return { imported: params.names, offered: true }; });
  const client = {
    getSnapshot: () => ({ state: 'connected', info: { runtime_id: 'host-a' } }),
    supports: (_surface: string, name: string) => operations.includes(name),
    mcpImport: { candidates, apply },
  } as unknown as WhipClient;
  const runtime = { queries, report: vi.fn(), getSnapshot: () => ({ commands: [] }), subscribe: () => () => {} } as unknown as AppRuntime;
  const onDone = vi.fn();
  function wrapper(children: ReactNode) {
    return <RuntimeContext.Provider value={runtime}><ThemeProvider initialTheme="light"><UIProvider><QueryClientProvider client={queries}>{children}</QueryClientProvider></UIProvider></ThemeProvider></RuntimeContext.Provider>;
  }
  return { candidates, apply, onDone, queries, render: (cwd = '') => render(wrapper(<MCPImportScreen client={client} hostName="Local" cwd={cwd} onDone={onDone} />)) };
}

it('orders importable servers first and already-native servers last, each A to Z', () => {
  expect(sortCandidates(found.candidates).map(c => c.name)).toEqual(['computer-use', 'exa', 'figma', 'node_repl', 'paper', 'ahrefs', 'executor']);
  expect(shouldOffer(found)).toBe(true);
  expect(shouldOffer({ ...found, offered: true })).toBe(false);
  expect(shouldOffer({ ...found, candidates: found.candidates.filter(c => c.state !== 'importable') })).toBe(false);
  expect(caveat(candidate('x', 'disabled'), false)).toBe('Off in Codex');
  expect(caveat(candidate('x', 'excluded'), true)).toBe('');
});

it('checks importable servers by default, leaves off and excluded ones unchecked, and never offers a checkbox for native or unsupported ones', async () => {
  const f = fixture(); f.render();
  const list = await screen.findByRole('list', { name: 'Discovered MCP servers' });
  const rows = within(list).getAllByRole('listitem').map(row => row.textContent);
  expect(rows[0]).toContain('computer-use'); expect(rows[rows.length - 1]).toContain('executor');
  expect(screen.getByRole('checkbox', { name: 'Import paper' }).getAttribute('aria-checked')).toBe('true');
  expect(screen.getByRole('checkbox', { name: 'Import exa' }).getAttribute('aria-checked')).toBe('true');
  expect(screen.getByRole('checkbox', { name: 'Import computer-use' }).getAttribute('aria-checked')).toBe('false');
  expect(screen.getByRole('checkbox', { name: 'Import node_repl' }).getAttribute('aria-checked')).toBe('false');
  expect(disabled(screen.getByRole('checkbox', { name: 'Import node_repl' }))).toBe(true);
  expect(disabled(screen.getByRole('checkbox', { name: 'Import figma' }))).toBe(true);
  expect(screen.queryByRole('checkbox', { name: 'Import ahrefs' })).toBeNull();
  expect(screen.getByLabelText('ahrefs is already in Whip')).toBeTruthy();
  expect(screen.getByText('Off in Codex')).toBeTruthy();
  expect(screen.getByText("needs a sign-in Whip can't do yet")).toBeTruthy();
  expect(screen.getByText(/Whip found 5 servers in Codex, Claude, and OpenCode on Local/)).toBeTruthy();
  expect(screen.getByRole('button', { name: 'Import 2 servers' })).toBeTruthy();
  expect(screen.getByText(/2 selected · saved to Whip's configuration on Local/)).toBeTruthy();
  expect(screen.getByText('/home/u/.whipcode/config.json')).toBeTruthy();
  expect(f.candidates).toHaveBeenCalledWith({}, expect.anything());
});

it('imports exactly the ticked names, including an excluded server after Include', async () => {
  const f = fixture(); f.render('/repo');
  fireEvent.click(await screen.findByRole('checkbox', { name: 'Import exa' }));
  fireEvent.click(screen.getByRole('checkbox', { name: 'Import computer-use' }));
  fireEvent.click(screen.getByRole('button', { name: 'Include' }));
  const nodeRepl = screen.getByRole('checkbox', { name: 'Import node_repl' });
  expect(disabled(nodeRepl)).toBe(false);
  fireEvent.click(nodeRepl);
  expect(screen.getByRole('button', { name: 'Import 3 servers' })).toBeTruthy();
  fireEvent.click(screen.getByRole('button', { name: 'Import 3 servers' }));
  await waitFor(() => expect(f.onDone).toHaveBeenCalledWith({ imported: ['computer-use', 'node_repl', 'paper'], offered: true }));
  expect(f.apply).toHaveBeenCalledExactlyOnceWith({ cwd: '/repo', names: ['computer-use', 'node_repl', 'paper'] });
  expect(f.candidates).toHaveBeenCalledWith({ cwd: '/repo' }, expect.anything());
  expect((f.queries.getQueryData(['mcp-import-candidates', 'host-a', '/repo']) as MCPImportCandidatesResult).offered).toBe(true);
});

it('skips with an empty list and disables Import when nothing is ticked', async () => {
  const f = fixture(); f.render();
  fireEvent.click(await screen.findByRole('checkbox', { name: 'Import paper' }));
  fireEvent.click(screen.getByRole('checkbox', { name: 'Import exa' }));
  expect(disabled(screen.getByRole('button', { name: 'Import 0 servers' }))).toBe(true);
  fireEvent.click(screen.getByRole('button', { name: 'Skip for now' }));
  await waitFor(() => expect(f.onDone).toHaveBeenCalledWith({ imported: [], offered: true }));
  expect(f.apply).toHaveBeenCalledExactlyOnceWith({ names: [] });
});

it('reports unreadable sources quietly and shows an empty state when nothing was found', async () => {
  const f = fixture({ offered: true, config_path: '/x/config.json', candidates: [], errors: { '/home/u/.codex/config.toml': 'toml: line 3: expected key' } });
  f.render();
  expect(await screen.findByText('No MCP servers were found in Codex, Claude, or OpenCode on Local.')).toBeTruthy();
  expect(screen.getByText(/Couldn't read \/home\/u\/.codex\/config.toml: toml: line 3/)).toBeTruthy();
  expect(screen.queryByRole('button', { name: 'Skip for now' })).toBeNull();
  expect(screen.queryByRole('button', { name: /Import/ })).toBeNull();
});

it('shows the import failure and keeps the ticks', async () => {
  const f = fixture();
  f.apply.mockRejectedValueOnce(new Error('config.json: permission denied'));
  f.render();
  fireEvent.click(await screen.findByRole('button', { name: 'Import 2 servers' }));
  await screen.findByText('config.json: permission denied');
  expect(f.onDone).not.toHaveBeenCalled();
  expect(screen.getByRole('checkbox', { name: 'Import paper' }).getAttribute('aria-checked')).toBe('true');
});
