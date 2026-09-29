import { useEffect, useRef, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import type { Client, MCPConfiguration } from '@whip/sdk';
import { Button, Dialog, Switch } from '@whip/ui';
import { useRuntime, useSessionTabs } from '../context';
import { isSessionTab, selectedSessionTab } from '../session-tabs';
import { ErrorNotice } from '../error-feedback';
import { MCPImportScreen } from '../mcp-import';
import { mcpRefreshNotice } from '../mcp-refresh';
import { SettingsGroup, SettingRow, settingsSection } from './section-layout';

export function MCPImportSettings({ client, enabled, hostName = 'this host' }: { client: Client; enabled: boolean; hostName?: string }) {
  const runtime = useRuntime();
  const runtimeId = client.runtimeID;
  const selected = selectedSessionTab(useSessionTabs().workspace);
  const selectedRoot = selected && isSessionTab(selected) && selected.runtimeId === runtimeId ? selected.rootId : null;
  const configuration = useQuery({ queryKey: ['mcp-configuration', runtimeId], queryFn: ({ signal }) => client.mcpConfiguration({ signal }), enabled });
  const [importRoot, setImportRoot] = useState<string | null>(null);
  const [open, setOpen] = useState(false);
  const [notice, setNotice] = useState('');
  const [refreshRoot, setRefreshRoot] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>();
  const request = useRef<AbortController | null>(null);
  useEffect(() => { request.current = null; setBusy(false); return () => request.current?.abort(); }, [client]);
  async function action(run: (signal: AbortSignal) => Promise<void>) {
    if (!enabled || request.current) return;
    const controller = new AbortController(); request.current = controller; setBusy(true); setError(undefined);
    try { await run(controller.signal); }
    catch (error) { if (!controller.signal.aborted) { setError(error); await configuration.refetch(); } }
    finally { if (request.current === controller) request.current = null; if (!controller.signal.aborted) setBusy(false); }
  }
  function update(patch: Pick<Parameters<Client['configureMCP']>[0], 'imports' | 'brand_icons'>) {
    const current = configuration.data;
    if (!current) return;
    void action(async signal => { const result = await client.configureMCP({ revision: current.revision, name: '', server: null, remove: false, ...patch }, { signal }); if (signal.aborted) return; runtime.queries.setQueryData(['mcp-configuration', runtimeId], result); await runtime.queries.invalidateQueries({ predicate: query => query.queryKey.includes(runtimeId) && query.queryKey[0] !== 'mcp-configuration' }); });
  }
  function importSource(name: 'claude' | 'codex', checked: boolean) {
    const current = configuration.data;
    if (!current) return;
    const imports: MCPConfiguration['imports'] = { ...current.imports, [name]: { ...current.imports[name], enabled: checked, only: current.imports[name]?.only ?? [], exclude: current.imports[name]?.exclude ?? [] } };
    update({ imports, brand_icons: null });
  }
  return <SettingsGroup title="MCP servers">
    <SettingRow id="mcp_import" label="Servers from other agents" description="Review exact declarations on this execution host and copy selected servers into Whip. Importing never connects a server or grants an operation."><Button xstyle={settingsSection.control} disabled={!enabled} onClick={() => { setImportRoot(selectedRoot); setNotice(''); setOpen(true); }}>Import servers…</Button></SettingRow>
    {(['claude', 'codex'] as const).map(name => <SettingRow key={name} id={`import_${name}`} label={`Import ${name === 'claude' ? 'Claude' : 'Codex'} configuration`} description="Controls this source's inclusion in discovery. Existing connections remain unchanged."><Switch aria-label={`Import ${name === 'claude' ? 'Claude' : 'Codex'} configuration`} checked={configuration.data?.imports[name]?.enabled !== false} disabled={!enabled || busy || !configuration.data} onCheckedChange={value => importSource(name, value)} /></SettingRow>)}
    <SettingRow id="mcp_logos" label="Server logos" description="Allow host logo lookups for domains missing from the bundled marks. Turn off to use bundled marks only."><Switch aria-label="Look up MCP server logos on DuckDuckGo" checked={configuration.data?.brand_icons ?? false} disabled={!enabled || busy || !configuration.data} onCheckedChange={value => update({ imports: null, brand_icons: value })} /></SettingRow>
    {notice && <p role="status">{notice}</p>}
    {refreshRoot && <Button disabled={!enabled || busy} onClick={() => void action(async signal => { const result = await client.refreshMCP(refreshRoot, { signal }); if (!signal.aborted) { setNotice(mcpRefreshNotice(result)); setRefreshRoot(null); } })}>Refresh imported servers in the captured session</Button>}
    <ErrorNotice type="resource" owner={`${runtimeId}:mcp-config`} title="Could not read MCP configuration" error={configuration.error} />
    <ErrorNotice type="action" owner={`${runtimeId}:mcp-settings`} title="MCP settings need attention" error={error} />
    <Dialog open={open} onOpenChange={setOpen} title="Bring your MCP servers into Whip">
      {open && <MCPImportScreen client={client} enabled={enabled} hostName={hostName} sessionID={importRoot} onDone={(result, refresh) => { setOpen(false); setNotice(`Imported ${result.added.length} servers. ${refresh?.notice ?? ''}`); if (result.added.length) setRefreshRoot(importRoot); }} />}
    </Dialog>
  </SettingsGroup>;
}
