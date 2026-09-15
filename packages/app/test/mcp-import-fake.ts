import { vi } from 'vitest';
import type { MCPImportApplyParams, MCPImportCandidatesResult } from '@whip/protocol';
import type { MCPImportCandidate } from '../src/mcp-import';

export const candidate = (name: string, state: MCPImportCandidate['state'], source = 'codex', extra: Partial<MCPImportCandidate> = {}): MCPImportCandidate =>
  ({ name, source, state, ...extra });

/** The smallest interesting host: one importable Codex server and one already in Whip. */
export const twoServers = (offered: boolean): MCPImportCandidatesResult =>
  ({ offered, config_path: '/home/u/.whipcode/config.json', candidates: [candidate('paper', 'importable'), candidate('ahrefs', 'native')] });

/** `client.supports` for a daemon that has the import operations. */
export const supportsImport = (_surface: string, name: string) => name.startsWith('mcp.import');

/** A fake `client.mcpImport` that remembers the answer the way the daemon does, so a later fetch reports offered: true. */
export function fakeMCPImport(initial: MCPImportCandidatesResult) {
  let server = initial;
  return {
    candidates: vi.fn(async () => server),
    apply: vi.fn(async (params: MCPImportApplyParams) => { server = { ...server, offered: true }; return { imported: params.names ?? [], skipped: {} }; }),
  };
}
