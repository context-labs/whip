import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { ThemeProvider, UIProvider } from '@whip/ui';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import type { WhipClient } from '@whip/sdk';
import type { ConfigurationUpdate, RuntimeConfiguration } from '@whip/protocol';
import { RuntimeContext } from '../src/context';
import type { AppRuntime } from '../src/runtime';
import { MCPImportSettings } from '../src/settings/mcp-import';
import { candidate, fakeMCPImport, supportsImport } from './mcp-import-fake';

beforeEach(() => vi.stubGlobal('matchMedia', () => ({ matches: false, addEventListener() {}, removeEventListener() {} })));
afterEach(() => vi.unstubAllGlobals());

const configuration: RuntimeConfiguration = {
  revision: 'v1', default_model: 'model-a', default_provider: 'openrouter', default_effort: 'high', default_execution_engine: 'starlark',
  compact_model: 'small-a', compact_provider: 'inference', compact_percent: 75, goal_max_rounds: 8, max_retries: 2,
  import_claude: true, import_codex: false, mcp_import_offered: true, brand_icons: true,
};

function fixture(supports = supportsImport) {
  const queries = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } });
  const mcpImport = fakeMCPImport({ offered: true, config_path: '/Users/sam/.whipcode/config.json', candidates: [candidate('paper', 'importable'), candidate('exa', 'importable', 'claude')] });
  let server = configuration;
  const update = vi.fn(async (patch: ConfigurationUpdate) => { server = { ...server, ...patch, revision: 'v2' } as RuntimeConfiguration; return server; });
  const client = {
    getSnapshot: () => ({ state: 'connected', info: { runtime_id: 'host-a' } }), supports, mcpImport,
    configuration: { get: vi.fn(async () => server), update },
  } as unknown as WhipClient;
  const runtime = { queries, report: vi.fn(), getSnapshot: () => ({ commands: [] }), subscribe: () => () => {} } as unknown as AppRuntime;
  render(<RuntimeContext.Provider value={runtime}><ThemeProvider initialTheme="light"><UIProvider><QueryClientProvider client={queries}>
    <MCPImportSettings client={client} enabled hostName="Mac mini" />
  </QueryClientProvider></UIProvider></ThemeProvider></RuntimeContext.Provider>);
  return { mcpImport, update };
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
