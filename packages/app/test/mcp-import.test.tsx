import { fireEvent, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { DeliveryError } from '@whip/sdk';
import { MCPImportScreen, sortCandidates, shouldOffer } from '../src/mcp-import';
import { mcpFixture, candidate } from './mcp-v4-fixture';
import { revision, nextRevision } from './provider-fixture';
beforeEach(() => vi.stubGlobal('matchMedia', () => ({ matches: false, addEventListener() {}, removeEventListener() {} })));
afterEach(() => vi.unstubAllGlobals());
it('imports exact fingerprints and captured owner without automatically connecting or granting', async () => {
  const f = await mcpFixture(); const done = vi.fn(); f.mount(<MCPImportScreen client={f.client} hostName="Remote" sessionID="root" onDone={done} />);
  await screen.findByRole('checkbox', { name: 'Import paper' }); expect(f.count('mcp.import.apply')).toBe(0); expect(f.count('mcp.refresh')).toBe(0);
  fireEvent.click(screen.getByRole('button', { name: 'Import 1 server' })); await waitFor(() => expect(done).toHaveBeenCalledOnce());
  expect(f.calls.find(call => call.method === 'mcp.import.apply')?.params).toEqual({ revision, session_id: 'root', fingerprints: { paper: revision } }); expect(f.count('mcp.refresh')).toBe(0); expect(f.count('permissions.resolve')).toBe(0);
});
it('uncertain import refreshes candidates without replaying publication', async () => {
  const f = await mcpFixture(); f.data.handlers['mcp.import.apply'] = () => { f.changeCandidates({ revision: nextRevision, candidates: [candidate('paper', 'native')], source_errors: {} }); throw new DeliveryError('Import acknowledgement lost'); };
  f.mount(<MCPImportScreen client={f.client} hostName="Remote" />); fireEvent.click(await screen.findByRole('button', { name: 'Import 1 server' })); await screen.findByText(/Import acknowledgement lost/); await screen.findByLabelText('paper is already in Whip'); expect(f.count('mcp.import.apply')).toBe(1); expect(f.count('mcp.import.candidates')).toBe(2);
});
it('skip saves the offered decision with no selected fingerprints and no connection', async () => {
  const f = await mcpFixture(); f.mount(<MCPImportScreen client={f.client} hostName="Remote" />); fireEvent.click(await screen.findByRole('button', { name: 'Skip for now' })); await waitFor(() => expect(f.count('mcp.import.apply')).toBe(1)); expect(f.calls.find(call => call.method === 'mcp.import.apply')?.params).toEqual({ revision, session_id: null, fingerprints: {} }); expect(f.count('mcp.refresh')).toBe(0);
});
it('exposes source errors and requires opt-in before selecting excluded entries', async () => {
  const f = await mcpFixture(); f.changeCandidates({ revision, candidates: [candidate('excluded', 'excluded'), candidate('disabled', 'disabled')], source_errors: { codex: 'Could not read source' } });
  f.mount(<MCPImportScreen client={f.client} hostName="Remote" />); const checkbox = await screen.findByRole('checkbox', { name: 'Import excluded' }); expect(checkbox.getAttribute('aria-disabled')).toBe('true'); expect(screen.getByText(/Could not read source/)).toBeTruthy();
  fireEvent.click(checkbox); expect(checkbox.getAttribute('aria-checked')).toBe('false');
  fireEvent.click(screen.getByRole('button', { name: 'Include' })); expect(checkbox.getAttribute('aria-disabled')).not.toBe('true'); expect(checkbox.getAttribute('aria-checked')).toBe('false');
});
it('offers from native saved policy and sorts native entries last without guessing membership', async () => {
  const f = await mcpFixture(); const data = { revision, candidates: [candidate('paper')], source_errors: {} };
  expect(shouldOffer(data, f.configuration())).toBe(true); expect(shouldOffer(data, { ...f.configuration(), imports: { ...f.configuration().imports, offered: true } })).toBe(false);
  expect(sortCandidates([candidate('a', 'native'), candidate('z')]).map(item => item.name)).toEqual(['z', 'a']);
});
