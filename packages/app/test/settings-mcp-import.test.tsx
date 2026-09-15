import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { ThemeProvider, UIProvider } from '@whip/ui';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import type { WhipClient } from '@whip/sdk';
import { RuntimeContext } from '../src/context';
import type { AppRuntime } from '../src/runtime';
import { MCPImportSettings } from '../src/settings/mcp-import';
import { candidate, fakeMCPImport, supportsImport } from './mcp-import-fake';

beforeEach(() => vi.stubGlobal('matchMedia', () => ({ matches: false, addEventListener() {}, removeEventListener() {} })));
afterEach(() => vi.unstubAllGlobals());

function fixture(supports = supportsImport) {
  const queries = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } });
  const mcpImport = fakeMCPImport({ offered: true, config_path: '/Users/sam/.whipcode/config.json', candidates: [candidate('paper', 'importable'), candidate('exa', 'importable', 'claude')] });
  const client = { getSnapshot: () => ({ state: 'connected', info: { runtime_id: 'host-a' } }), supports, mcpImport } as unknown as WhipClient;
  const runtime = { queries, report: vi.fn(), getSnapshot: () => ({ commands: [] }), subscribe: () => () => {} } as unknown as AppRuntime;
  render(<RuntimeContext.Provider value={runtime}><ThemeProvider initialTheme="light"><UIProvider><QueryClientProvider client={queries}>
    <MCPImportSettings client={client} enabled hostName="Mac mini" />
  </QueryClientProvider></UIProvider></ThemeProvider></RuntimeContext.Provider>);
  return mcpImport;
}

it('opens the import screen for the selected host from Settings and reports what was imported', async () => {
  const mcpImport = fixture();
  fireEvent.click(screen.getByRole('button', { name: 'Import servers…' }));
  const dialog = await screen.findByRole('dialog', { name: 'Bring your MCP servers into Whip' });
  const exa = await screen.findByRole('checkbox', { name: 'Import exa' });
  expect(dialog.textContent).toContain('on Mac mini');
  // Already answered on this host: no Skip, just the import.
  expect(screen.queryByRole('button', { name: 'Skip for now' })).toBeNull();
  fireEvent.click(exa);
  fireEvent.click(screen.getByRole('button', { name: 'Import 1 server' }));
  await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
  expect(mcpImport.apply).toHaveBeenCalledExactlyOnceWith({ names: ['paper'] });
  expect(screen.getByText("Imported 1 MCP server into Whip's configuration on Mac mini.")).toBeTruthy();
  // One read to show the list; the answer is written into the cache, not refetched.
  expect(mcpImport.candidates).toHaveBeenCalledExactlyOnceWith({}, expect.anything());
});

it('explains an older daemon instead of offering an import that cannot happen', async () => {
  fixture(() => false);
  fireEvent.click(screen.getByRole('button', { name: 'Import servers…' }));
  expect((await screen.findByRole('status')).textContent).toContain('Mac mini runs an older daemon without MCP import');
});
