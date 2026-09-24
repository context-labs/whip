import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import type { WhipClient } from '@whip/sdk';
import { Button, Dialog, Switch } from '@whip/ui';
import { useRuntime, useSessionTabs } from '../context';
import { isSessionTab, selectedSessionTab } from '../session-tabs';
import { ErrorNotice } from '../error-feedback';
import { MCPImportScreen } from '../mcp-import';
import { SettingsGroup, SettingRow, settingsSection } from './section-layout';

/** The Settings way into the import screen (the Welcome offer is the other) and the one switch that governs its logo lookups. */
export function MCPImportSettings({ client, enabled, hostName = 'this host' }: { client: WhipClient; enabled: boolean; hostName?: string }) {
  const runtime = useRuntime();
  const runtimeId = client.getSnapshot().info?.runtime_id;
  const selected = selectedSessionTab(useSessionTabs().workspace);
  const refreshRootId = selected && isSessionTab(selected) && selected.runtimeId === runtimeId ? selected.rootId : undefined;
  const configuration = useQuery({ queryKey: ['runtime-configuration', runtimeId], queryFn: ({ signal }) => client.configuration.get({ signal }), enabled });
  const [open, setOpen] = useState(false);
  const [notice, setNotice] = useState('');
  const [refreshError, setRefreshError] = useState<unknown>();
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<unknown>();
  // The switch saves on its own: one boolean, no form to dirty.
  async function setLogos(value: boolean) {
    const current = configuration.data;
    if (!current) return;
    setSaving(true); setError(undefined);
    try {
      const next = await client.configuration.update({ revision: current.revision, brand_icons: value });
      runtime.queries.setQueryData(['runtime-configuration', runtimeId], next);
    } catch (value) { setError(value); } finally { setSaving(false); }
  }
  return <SettingsGroup title="MCP servers">
    <SettingRow id="mcp_import" label="Servers from other agents" description="Pick MCP servers configured for Codex, Claude, or OpenCode on this execution host and add them to Whip's own configuration, where they run without per-call approval.">
      <Button type="button" xstyle={settingsSection.control} disabled={!enabled} onClick={() => { setNotice(''); setRefreshError(undefined); setOpen(true); }}>Import servers…</Button>
    </SettingRow>
    <SettingRow id="mcp_logos" label="Server logos" description="Show each server's logo in that list. A server outside the bundled set is looked up on DuckDuckGo by its domain, once, from this execution host. Turn off to keep every lookup on the host.">
      <Switch aria-label="Look up MCP server logos on DuckDuckGo" checked={configuration.data?.brand_icons ?? true} disabled={!enabled || saving || !configuration.data} onCheckedChange={value => void setLogos(!!value)} />
    </SettingRow>
    {notice && <p role="status">{notice}</p>}
    <ErrorNotice type="action" owner={`${runtimeId}:mcp-refresh`} title="Imported servers were saved; session refresh needs attention" error={refreshError} />
    <ErrorNotice type="action" owner={`${runtimeId}:mcp-logos`} title="Could not save the logo setting" error={error} />
    <Dialog open={open} onOpenChange={setOpen} title="Bring your MCP servers into Whip">
      {open && <MCPImportScreen client={client} hostName={hostName} refreshRootId={refreshRootId} onDone={(result, refresh) => {
        setOpen(false);
        const count = result.imported?.length ?? 0;
        setNotice(count ? `Imported ${count} MCP ${count === 1 ? 'server' : 'servers'} into Whip's configuration on ${hostName}.${refresh ? ` ${refresh.notice}` : ''}` : 'No MCP servers were imported.');
        setRefreshError(refresh?.error);
      }} />}
    </Dialog>
  </SettingsGroup>;
}
