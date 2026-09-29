import type { MCPConfiguration, MCPImportCandidatesResult, MCPImportParams, ConfigureMCPParams } from '@whip/sdk';
import { providerFixture, revision, nextRevision } from './provider-fixture';
import { SessionTabs } from '../src/session-tabs';
export const candidate = (name: string, state: MCPImportCandidatesResult['candidates'][number]['state'] = 'importable', source: MCPImportCandidatesResult['candidates'][number]['source'] = 'codex'): MCPImportCandidatesResult['candidates'][number] => ({ name, state, source, fingerprint: revision, gated: false, note: '', brand_hint: '', brand_key: '' });
export async function mcpFixture() {
  const f = await providerFixture();
  let configuration: MCPConfiguration = { revision, servers: [], imports: { claude: null, codex: null, project: null, opencode: null, offered: false }, brand_icons: false };
  let candidates: MCPImportCandidatesResult = { revision, candidates: [candidate('paper'), candidate('native', 'native')], source_errors: {} };
  f.data.handlers['mcp.configuration'] = () => configuration;
  f.data.handlers['mcp.configure'] = request => { const p = request.params as ConfigureMCPParams; configuration = { ...configuration, revision: nextRevision, imports: p.imports ?? configuration.imports, brand_icons: p.brand_icons ?? configuration.brand_icons }; return configuration; };
  f.data.handlers['mcp.import.candidates'] = () => candidates;
  f.data.handlers['mcp.import.apply'] = request => { const p = request.params as MCPImportParams; configuration = { ...configuration, revision: nextRevision, imports: { ...configuration.imports, offered: true } }; return { configuration, added: Object.keys(p.fingerprints), skipped: {} }; };
  f.data.handlers['mcp.brand.icons'] = () => ({ icons: {} });
  f.data.handlers['mcp.refresh'] = () => ({ added: ['paper'], existing: [], changed: [], servers: [], blocked: [], source_errors: {} });
  const tabs = new SessionTabs(); Object.assign(f.runtime, { tabs });
  return { ...f, tabs, configuration: () => configuration, changeCandidates: (value: MCPImportCandidatesResult) => { candidates = value; } };
}
