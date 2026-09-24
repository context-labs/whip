import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { ThemeProvider, UIProvider } from '@whip/ui';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import type { WhipClient } from '@whip/sdk';
import type { ConfigurationUpdate, RuntimeConfiguration } from '@whip/protocol';
import { RuntimeContext } from '../src/context';
import type { AppRuntime } from '../src/runtime';
import { SessionTabs } from '../src/session-tabs';
import { MCPImportSettings } from '../src/settings/mcp-import';
import { candidate, fakeMCPImport, supportsImport } from './mcp-import-fake';

beforeEach(() => vi.stubGlobal('matchMedia', () => ({ matches: false, addEventListener() {}, removeEventListener() {} })));
afterEach(() => vi.unstubAllGlobals());

const configuration: RuntimeConfiguration = {
  revision: 'v1', default_model: 'model-a', default_provider: 'openrouter', default_effort: 'high', default_execution_engine: 'starlark',
  compact_model: 'small-a', compact_provider: 'inference', compact_percent: 75, goal_max_rounds: 8, max_retries: 2,
  import_claude: true, import_codex: false, mcp_import_offered: true, brand_icons: true,
};

function fixture(supports = supportsImport, sessionHost?: string) {
  const tabs = new SessionTabs();
  if (sessionHost) tabs.visit(sessionHost, 'current-root', {});
  const refresh = vi.fn(() => ({ refresh: true }));
  const session = vi.fn(() => ({ mcp: { refresh } }));
  const run = vi.fn(async () => ({ status: 'succeeded', result: {} }));
  const queries = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } });
  const mcpImport = fakeMCPImport({ offered: true, config_path: '/Users/sam/.whipcode/config.json', candidates: [candidate('paper', 'importable'), candidate('exa', 'importable', 'claude')] });
  let server = configuration;
  const update = vi.fn(async (patch: ConfigurationUpdate) => { server = { ...server, ...patch, revision: 'v2' } as RuntimeConfiguration; return server; });
  const client = {
    getSnapshot: () => ({ state: 'connected', info: { runtime_id: 'host-a' } }), supports, mcpImport,
    configuration: { get: vi.fn(async () => server), update }, session,
  } as unknown as WhipClient;
  const runtime = { queries, tabs, run, report: vi.fn(), getSnapshot: () => ({ commands: [] }), subscribe: () => () => {} } as unknown as AppRuntime;
  render(<RuntimeContext.Provider value={runtime}><ThemeProvider initialTheme="light"><UIProvider><QueryClientProvider client={queries}>
    <MCPImportSettings client={client} enabled hostName="Mac mini" />
  </QueryClientProvider></UIProvider></ThemeProvider></RuntimeContext.Provider>);
  return { mcpImport, update, tabs, run, session, refresh };
}

it('opens the import screen for the selected host from Settings and reports what was imported', async () => {
  const f = fixture();
  fireEvent.click(screen.getByRole('button', { name: 'Import servers…' }));
  const dialog = await screen.findByRole('dialog', { name: 'Bring your MCP servers into Whip' });
  const exa = await screen.findByRole('checkbox', { name: 'Import exa' });
  expect(dialog.textContent).toContain('on Mac mini');
  // Already answered on this host: no Skip, just the import.
  expect(screen.queryByRole('button', { name: 'Skip for now' })).toBeNull();
  fireEvent.click(exa);
  fireEvent.click(screen.getByRole('button', { name: 'Import 1 server' }));
  await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
  expect(f.mcpImport.apply).toHaveBeenCalledExactlyOnceWith({ names: ['paper'] });
  expect(screen.getByText("Imported 1 MCP server into Whip's configuration on Mac mini.")).toBeTruthy();
  // One read to show the list; the answer is written into the cache, not refetched.
  expect(f.mcpImport.candidates).toHaveBeenCalledExactlyOnceWith({}, expect.anything());
});

it('saves the logo lookup switch on its own against the current revision', async () => {
  const f = fixture();
  const toggle = await screen.findByRole('switch', { name: 'Look up MCP server logos on DuckDuckGo' });
  await waitFor(() => expect(toggle.getAttribute('aria-checked')).toBe('true'));
  fireEvent.click(toggle);
  await waitFor(() => expect(f.update).toHaveBeenCalledExactlyOnceWith({ revision: 'v1', brand_icons: false }));
  await waitFor(() => expect(screen.getByRole('switch', { name: 'Look up MCP server logos on DuckDuckGo' }).getAttribute('aria-checked')).toBe('false'));
});

it('explains an older daemon instead of offering an import that cannot happen', async () => {
  fixture(() => false);
  fireEvent.click(screen.getByRole('button', { name: 'Import servers…' }));
  expect((await screen.findByRole('status')).textContent).toContain('Mac mini runs an older daemon without MCP import');
});
const supportsRefresh = (surface: string, name: string) => supportsImport(surface, name) || (surface === 'runtime' && name === 'mcp.refresh');
async function importServers() {
  fireEvent.click(screen.getByRole('button', { name: 'Import servers…' }));
  fireEvent.click(await screen.findByRole('button', { name: 'Import 2 servers' }));
}

it('refreshes only the current session on the imported host after persistence', async () => {
  const f = fixture(supportsRefresh, 'host-a');
  await importServers();
  await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
  expect(f.session).toHaveBeenCalledExactlyOnceWith('current-root');
  expect(f.refresh).toHaveBeenCalledOnce();
  expect(f.run).toHaveBeenCalledExactlyOnceWith({ refresh: true }, 'MCP configuration refreshed');
  expect(screen.getByRole('status').textContent).toContain('MCP configuration refreshed for this session');
  expect(f.mcpImport.apply.mock.invocationCallOrder[0]).toBeLessThan(f.refresh.mock.invocationCallOrder[0]!);
});

it.each([undefined, 'host-b'])('does not invent or redirect a session when the current host is %s', async host => {
  const f = fixture(supportsRefresh, host);
  await importServers();
  await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
  expect(f.session).not.toHaveBeenCalled();
  expect(f.run).not.toHaveBeenCalled();
  expect(screen.getByRole('status').textContent).toContain('Imported 2 MCP servers');
});

it('keeps import success distinct from a failed live refresh', async () => {
  const f = fixture(supportsRefresh, 'host-a');
  f.run.mockRejectedValueOnce(new Error('configuration unreadable'));
  await importServers();
  await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
  expect(screen.getByRole('status').textContent).toContain('Imported 2 MCP servers');
  expect(screen.getByRole('status').textContent).toContain('refreshing the current session did not complete');
  expect(screen.getByText('Imported servers were saved; session refresh needs attention')).toBeTruthy();
  expect(screen.queryByText('Could not import')).toBeNull();
  expect(f.mcpImport.apply).toHaveBeenCalledOnce();
});

it('does not call an unadvertised refresh action on an older host', async () => {
  const f = fixture(supportsImport, 'host-a');
  await importServers();
  await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
  expect(f.session).not.toHaveBeenCalled();
  expect(screen.getByRole('status').textContent).toContain('This host cannot refresh an existing session');
});

it('captures the session at import submission, before navigation during persistence', async () => {
  const f = fixture(supportsRefresh, 'host-a');
  let finish!: (value: { imported: string[]; skipped: {} }) => void;
  f.mcpImport.apply.mockImplementationOnce(() => new Promise(resolve => { finish = resolve; }));
  await importServers();
  expect(f.refresh).not.toHaveBeenCalled();
  act(() => f.tabs.visit('host-b', 'other-root', {}));
  await act(async () => finish({ imported: ['paper'], skipped: {} }));
  await waitFor(() => expect(f.session).toHaveBeenCalledExactlyOnceWith('current-root'));
});

it('never refreshes after a failed import or a no-op import', async () => {
  const f = fixture(supportsRefresh, 'host-a');
  f.mcpImport.apply.mockRejectedValueOnce(new Error('disk full'));
  await importServers();
  await screen.findByText('Could not import');
  expect(f.refresh).not.toHaveBeenCalled();
  f.mcpImport.apply.mockResolvedValueOnce({ imported: [], skipped: {} });
  fireEvent.click(screen.getByRole('button', { name: 'Import 2 servers' }));
  await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
  expect(f.refresh).not.toHaveBeenCalled();
});

it('reports retained changed configs and partial discovery instead of claiming every server is ready', async () => {
  const f = fixture(supportsRefresh, 'host-a');
  f.run.mockResolvedValueOnce({ status: 'succeeded', result: {
    changed: ['paper'], blocked: [{ name: 'blocked', status: 'blocked' }], source_errors: [{ name: 'file', status: 'unreadable' }],
  } });
  await importServers();
  await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
  const notice = screen.getByRole('status').textContent;
  expect(notice).toContain('runtime reload is required');
  expect(notice).toContain('1 server(s) remain blocked');
  expect(notice).toContain('1 configuration source(s) could not be read');
  expect(notice).toContain('connections may still be starting');
});
