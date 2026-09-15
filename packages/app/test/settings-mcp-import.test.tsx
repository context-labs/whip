import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { ThemeProvider, UIProvider } from '@whip/ui';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import type { WhipClient } from '@whip/sdk';
import type { RuntimeConfiguration } from '@whip/protocol';
import { RuntimeContext } from '../src/context';
import type { AppRuntime } from '../src/runtime';
import { ExecutionSettings } from '../src/settings/configuration';

beforeEach(() => vi.stubGlobal('matchMedia', () => ({ matches: false, addEventListener() {}, removeEventListener() {} })));
afterEach(() => vi.unstubAllGlobals());

const configuration: RuntimeConfiguration = {
  revision: 'v1', default_model: 'model-a', default_provider: 'openrouter', default_effort: 'high', default_execution_engine: 'starlark',
  compact_model: 'small-a', compact_provider: 'inference', compact_percent: 75, goal_max_rounds: 8, max_retries: 2, import_claude: true, import_codex: false,
};

function fixture(operations = ['mcp.import.candidates', 'mcp.import.apply']) {
  const queries = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } });
  let server = { offered: true, config_path: '/Users/sam/.whipcode/config.json', candidates: [
    { name: 'paper', source: 'codex', source_path: '/Users/sam/.codex/config.toml', transport: 'http', state: 'importable' },
    { name: 'exa', source: 'claude', source_path: '/Users/sam/.claude.json', transport: 'http', state: 'importable' },
  ] };
  const mcpImport = {
    candidates: vi.fn(async () => server),
    apply: vi.fn(async ({ names }: { names: string[] }) => { server = { ...server, offered: true }; return { imported: names, offered: true }; }),
  };
  const client = {
    getSnapshot: () => ({ state: 'connected', info: { runtime_id: 'host-a' } }),
    supports: (_surface: string, name: string) => operations.includes(name),
    configuration: { get: vi.fn(async () => configuration), update: vi.fn() }, mcpImport,
  } as unknown as WhipClient;
  const runtime = { queries, report: vi.fn(), getSnapshot: () => ({ commands: [] }), subscribe: () => () => {} } as unknown as AppRuntime;
  render(<RuntimeContext.Provider value={runtime}><ThemeProvider initialTheme="light"><UIProvider><QueryClientProvider client={queries}>
    <ExecutionSettings client={client} enabled hostName="Mac mini" />
  </QueryClientProvider></UIProvider></ThemeProvider></RuntimeContext.Provider>);
  return { mcpImport };
}

it('opens the import screen for the selected host from Settings and reports what was imported', async () => {
  const f = fixture();
  fireEvent.click(await screen.findByRole('button', { name: 'Import servers…' }));
  const dialog = await screen.findByRole('dialog', { name: 'Bring your MCP servers into Whip' });
  const exa = await screen.findByRole('checkbox', { name: 'Import exa' });
  expect(dialog.textContent).toContain('on Mac mini');
  expect(f.mcpImport.candidates).toHaveBeenCalledWith({}, expect.anything());
  // Already answered on this host: no Skip, just the import.
  expect(screen.queryByRole('button', { name: 'Skip for now' })).toBeNull();
  fireEvent.click(exa);
  fireEvent.click(screen.getByRole('button', { name: 'Import 1 server' }));
  await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
  expect(f.mcpImport.apply).toHaveBeenCalledExactlyOnceWith({ names: ['paper'] });
  expect(screen.getByText("Imported 1 MCP server into Whip's configuration on Mac mini.")).toBeTruthy();
  expect(f.mcpImport.candidates).toHaveBeenCalledWith({}, expect.anything());
});

it('explains an older daemon instead of offering an import that cannot happen', async () => {
  fixture([]);
  fireEvent.click(await screen.findByRole('button', { name: 'Import servers…' }));
  expect((await screen.findByRole('status')).textContent).toContain('Mac mini runs an older daemon without MCP import');
});
