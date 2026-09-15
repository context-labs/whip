import { useState } from 'react';
import type { WhipClient } from '@whip/sdk';
import { Button, Dialog } from '@whip/ui';
import { MCPImportScreen } from '../mcp-import';
import { SettingsGroup, SettingRow, settingsSection } from './section-layout';

/** The Settings way into the import screen; the Welcome offer is the other. */
export function MCPImportSettings({ client, enabled, hostName = 'this host' }: { client: WhipClient; enabled: boolean; hostName?: string }) {
  const [open, setOpen] = useState(false);
  const [notice, setNotice] = useState('');
  return <SettingsGroup title="MCP servers">
    <SettingRow id="mcp_import" label="Servers from other agents" description="Pick MCP servers configured for Codex, Claude, or OpenCode on this execution host and add them to Whip's own configuration, where they run without per-call approval.">
      <Button type="button" xstyle={settingsSection.control} disabled={!enabled} onClick={() => setOpen(true)}>Import servers…</Button>
    </SettingRow>
    {notice && <p role="status">{notice}</p>}
    <Dialog open={open} onOpenChange={setOpen} title="Bring your MCP servers into Whip">
      {open && <MCPImportScreen client={client} hostName={hostName} onDone={result => {
        setOpen(false);
        const count = result.imported?.length ?? 0;
        setNotice(count ? `Imported ${count} MCP ${count === 1 ? 'server' : 'servers'} into Whip's configuration on ${hostName}.` : 'No MCP servers were imported.');
      }} />}
    </Dialog>
  </SettingsGroup>;
}
