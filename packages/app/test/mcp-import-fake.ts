import { vi } from 'vitest';
import type { MCPBrandIconsParams, MCPImportApplyParams, MCPImportCandidatesResult } from '@whip/protocol';
import type { MCPImportCandidate } from '../src/mcp-import';

export const candidate = (name: string, state: MCPImportCandidate['state'], source = 'codex', extra: Partial<MCPImportCandidate> = {}): MCPImportCandidate =>
  ({ name, source, state, ...extra });

/** A 1×1 PNG, the kind of data: URI the daemon returns. */
export const tinyPNG = 'data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg==';

/** The smallest interesting host: one importable Codex server and one already in Whip. */
export const twoServers = (offered: boolean): MCPImportCandidatesResult =>
  ({ offered, config_path: '/home/u/.whipcode/config.json', candidates: [candidate('paper', 'importable'), candidate('ahrefs', 'native', 'codex', { brand_key: 'ahrefs.com' })] });

/** `client.supports` for a daemon that has the import and logo operations. */
export const supportsImport = (_surface: string, name: string) => name.startsWith('mcp.import') || name === 'mcp.brand.icons';

/**
 * A fake `client.mcpImport` that remembers the answer the way the daemon does, so a later fetch reports
 * offered: true, and resolves logos from `resolved` by brand key.
 */
export function fakeMCPImport(initial: MCPImportCandidatesResult, resolved: Record<string, string> = {}) {
  let server = initial;
  return {
    candidates: vi.fn(async () => server),
    apply: vi.fn(async (params: MCPImportApplyParams) => { server = { ...server, offered: true }; return { imported: params.names ?? [], skipped: {} }; }),
    brandIcons: vi.fn(async (params: MCPBrandIconsParams) => ({ icons: Object.fromEntries((params.keys ?? []).filter(key => key in resolved).map(key => [key, resolved[key]!])) })),
  };
}
