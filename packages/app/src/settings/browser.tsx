import { useState } from 'react';
import { AlertDialog, Button } from '@whip/ui';
import { useRuntime, useSessionTabs } from '../context';
import { BrowserPreviewControls } from '../browser-preview-controls';
import { BrowserProviderControls } from '../browser-provider-controls';
import { ErrorNotice } from '../error-feedback';
import { SettingsGroup, SettingRow } from './section-layout';

/** Address metadata is independent of the native website profile and survives older clients. */
export function BrowserSettings() {
  const runtime = useRuntime();
  useSessionTabs();
  const [forget, setForget] = useState(false), [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  const recoverable = runtime.tabs.browserRecovery();
  return <SettingsGroup title="Browser">
    <SettingRow id="browserPreview" label="SSH preview" description="Open a project preview through a saved, connected SSH host. Human browsing does not require an agent or Browser provider association."><BrowserPreviewControls/></SettingRow>
    <SettingRow id="browserAccess" label="Conversation access" description="Manage explicit Browser providers for conversations. Associations are not permission approvals and do not survive reconnect."><BrowserProviderControls/></SettingRow>
    {runtime.platform.browser && <p>Inside a page: Mod+L edits the address, Mod+F finds text, Mod+K opens commands, Mod+T opens a Browser tab, and Control+Tab switches tabs. Page shortcuts use these fixed keys; app shortcuts above apply while app controls are focused.</p>}
    <SettingRow id="browserRecovery" label="Saved Browser addresses" description={runtime.platform.browser ? 'Restore addresses preserved before using an older app version. Only address metadata is recovered, never page access grants.' : 'Browser pages require the desktop app. Saved addresses remain available on this device.'}>
      <Button disabled={!recoverable.length} onClick={() => {
        try { runtime.tabs.recoverBrowserTabs(); setError(''); setNotice('Saved Browser addresses restored to the workspace.'); }
        catch (error) { setError(error instanceof Error ? error.message : String(error)); }
      }}>Restore {recoverable.length || ''} saved addresses</Button>
    </SettingRow>
    <SettingRow id="browserForget" label="Forget closed Browser addresses" description="Remove Browser entries from recently closed tabs and the address recovery copy. Open tabs and website cookies are not affected.">
      <Button onClick={() => setForget(true)}>Forget closed addresses…</Button>
    </SettingRow>
    {notice && <p role="status">{notice}</p>}
    {error && <ErrorNotice type="action" owner="browser-metadata" title="Could not restore Browser addresses" error={error}/>}
    <AlertDialog open={forget} onOpenChange={setForget} title="Forget closed Browser addresses?" description="Recently closed Browser addresses and their recovery copy will be removed. This cannot be undone. Open Browser tabs and site data are retained." confirmLabel="Forget addresses" danger onConfirm={() => { runtime.tabs.forgetBrowserHistory(); setForget(false); setNotice('Closed Browser addresses forgotten.'); }}/>
  </SettingsGroup>;
}
