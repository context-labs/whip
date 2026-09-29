import { fireEvent, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { DeliveryError } from '@whip/sdk';
import { MCPImportSettings } from '../src/settings/mcp-import';
import { mcpFixture } from './mcp-v4-fixture';
import { revision } from './provider-fixture';
beforeEach(() => vi.stubGlobal('matchMedia', () => ({ matches: false, addEventListener() {}, removeEventListener() {} })));
afterEach(() => vi.unstubAllGlobals());
it('requires a separate explicit session refresh after importing and keeps the captured root', async () => {
  const f = await mcpFixture(); f.tabs.visit('host', 'first-root', {}); f.mount(<MCPImportSettings client={f.client} enabled hostName="Remote" />);
  fireEvent.click(screen.getByRole('button', { name: 'Import servers…' })); await screen.findByRole('button', { name: 'Import 1 server' });
  f.tabs.visit('host', 'later-root', {}); fireEvent.click(screen.getByRole('button', { name: 'Import 1 server' }));
  const refresh = await screen.findByRole('button', { name: 'Refresh imported servers in the captured session' }); expect(f.count('mcp.refresh')).toBe(0);
  fireEvent.click(refresh); await waitFor(() => expect(f.count('mcp.refresh')).toBe(1)); expect(f.calls.find(call => call.method === 'mcp.refresh')?.params).toEqual({ session_id: 'first-root' });
});
it('keeps a saved import distinct from failure of the subsequent explicit refresh', async () => {
  const f = await mcpFixture(); f.tabs.visit('host', 'root', {}); f.data.handlers['mcp.refresh'] = () => { throw new DeliveryError('Connection outcome unknown'); }; f.mount(<MCPImportSettings client={f.client} enabled />);
  fireEvent.click(screen.getByRole('button', { name: 'Import servers…' })); fireEvent.click(await screen.findByRole('button', { name: 'Import 1 server' })); fireEvent.click(await screen.findByRole('button', { name: 'Refresh imported servers in the captured session' }));
  await screen.findByText(/Connection outcome unknown/); expect(f.count('mcp.import.apply')).toBe(1); expect(screen.getByText(/Imported 1 servers/)).toBeTruthy();
});
it('saves logo policy alone against the observed host revision', async () => {
  const f = await mcpFixture(); f.mount(<MCPImportSettings client={f.client} enabled />); const control = await screen.findByRole('switch', { name: 'Look up MCP server logos on DuckDuckGo' }); await waitFor(() => expect(control.hasAttribute('disabled')).toBe(false));
  fireEvent.click(control); await waitFor(() => expect(f.count('mcp.configure')).toBe(1)); expect(f.calls.find(call => call.method === 'mcp.configure')?.params).toEqual({ revision, name: '', server: null, remove: false, imports: null, brand_icons: true });
});
it('import source edits preserve the other source filters and never connect', async () => {
  const f = await mcpFixture(); f.mount(<MCPImportSettings client={f.client} enabled />); const control = await screen.findByRole('switch', { name: 'Import Claude configuration' }); await waitFor(() => expect(control.hasAttribute('disabled')).toBe(false));
  fireEvent.click(control); await waitFor(() => expect(f.count('mcp.configure')).toBe(1)); expect(f.calls.find(call => call.method === 'mcp.configure')?.params).toMatchObject({ imports: { claude: { enabled: false, only: [], exclude: [] }, codex: null, opencode: null, project: null, offered: false }, brand_icons: null }); expect(f.count('mcp.refresh')).toBe(0);
});
