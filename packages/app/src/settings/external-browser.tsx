import { useEffect, useRef, useState } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import type { Client, ExternalBrowserStatus } from '@whip/sdk';
import { Button, Input, Select, Switch } from '@whip/ui';
import * as stylex from '@stylexjs/stylex';
import { ErrorNotice } from '../error-feedback';
import { layout } from '../styles';
import { SettingsGroup, SettingRow } from './section-layout';
import { useSettingsEdits } from './unsaved';

type Configuration = ExternalBrowserStatus['configuration'];
const modes = ['disabled', 'live', 'dedicated', 'headless', 'extension'] as const;
const labels = { disabled: 'Disabled', live: 'Existing Chrome', dedicated: 'Dedicated Chrome', headless: 'Headless Chrome', extension: 'Chrome extension' };
const key = (client: Client) => ['external-browser', client.runtimeID, client.processEpoch];

/** Configuration reads are passive. The form owns an explicit reviewed CAS,
 * while the host alone owns browser connections and permission authority. */
export function ExternalBrowserSettings({ client, enabled = true }: { client: Client; enabled?: boolean }) {
  const query = useQuery({ queryKey: key(client), queryFn: ({ signal }) => client.hosts.externalBrowser({ signal }), enabled, retry: false, gcTime: 0 });
  return <SettingsGroup title="External Chrome">
    <p>These settings apply to the execution host. Saving does not launch Chrome or grant browser access. Changes retire existing external connections; Desktop offered tabs stay separate.</p>
    {!enabled && <p role="status">Reconnect this host before changing external Chrome settings.</p>}
    <ErrorNotice type="resource" owner={`${client.runtimeID}:external-browser`} title="Could not read external Chrome settings" error={query.error}/>
    {query.data && <ExternalBrowserForm key={`${client.runtimeID}:${client.processEpoch}`} client={client} enabled={enabled} current={query.data} read={async () => { const result = await query.refetch({ throwOnError: true }); if (!result.data) throw new Error('External Chrome settings are unavailable.'); return result.data; }}/>}
    {!query.data && <Button disabled={!enabled || query.isFetching} onClick={() => void query.refetch()}>Read external Chrome settings</Button>}
  </SettingsGroup>;
}

function ExternalBrowserForm({ client, enabled, current, read }: { client: Client; enabled: boolean; current: ExternalBrowserStatus; read(): Promise<ExternalBrowserStatus> }) {
  const queries = useQueryClient();
  const [base, setBase] = useState(current), [draft, setDraft] = useState(current.configuration);
  const [busy, setBusy] = useState(false), [review, setReview] = useState(false), [error, setError] = useState<unknown>(), [notice, setNotice] = useState('');
  const request = useRef<AbortController | null>(null), available = useRef(enabled);
  available.current = enabled;
  useEffect(() => { available.current = enabled; return () => { available.current = false; request.current?.abort(); }; }, [client]);
  useEffect(() => { if (!enabled) request.current?.abort(); }, [enabled]);
  const dirty = JSON.stringify(draft) !== JSON.stringify(base.configuration), stale = base.revision !== current.revision;
  const disabled = !enabled || busy;
  function edit(patch: Partial<Configuration>) { setDraft(value => ({ ...value, ...patch })); setNotice(''); }
  function discard() { setBase(current); setDraft(current.configuration); setError(undefined); setNotice(''); }
  async function save(): Promise<boolean> {
    if (!available.current || request.current || review || stale || !dirty) return false;
    const controller = new AbortController(); request.current = controller; setBusy(true); setReview(true); setError(undefined); setNotice('');
    try {
      const result = await client.hosts.setExternalBrowser(base.revision, draft, { signal: controller.signal });
      controller.signal.throwIfAborted();
      setBase(result); setDraft(result.configuration); setReview(false); queries.setQueryData(key(client), result);
      setNotice('External Chrome settings saved. The next browser request still needs permission.');
      return true;
    } catch (error) {
      if (available.current) { setError(error); setReview(true); }
      return false;
    } finally { if (request.current === controller) request.current = null; setBusy(false); }
  }
  async function refresh() {
    if (!available.current || request.current) return;
    const controller = new AbortController(); request.current = controller; setBusy(true); setError(undefined);
    try { const value = await read(); controller.signal.throwIfAborted(); setBase(value); setDraft(value.configuration); setReview(false); setNotice('Current host settings loaded. No change was replayed.'); }
    catch (error) { if (available.current) setError(error); }
    finally { if (request.current === controller) request.current = null; setBusy(false); }
  }
  useSettingsEdits({ id: 'external-browser', dirty, description: 'Unsaved external Chrome settings on this host.', discard, save });
  return <form {...stylex.props(layout.column)} onSubmit={event => { event.preventDefault(); void save(); }}>
    <SettingRow id="external_browser_mode" label="External browser mode" description="Existing Chrome requires an explicit loopback endpoint or profile. Dedicated modes require an executable on this host.">
      <Select label="External browser mode" value={draft.mode} disabled={disabled} options={modes.map(value => ({ value, label: labels[value] }))} onValueChange={value => { const mode = modes.find(mode => mode === value); if (mode) edit({ mode }); }}/>
    </SettingRow>
    {(draft.mode === 'dedicated' || draft.mode === 'headless') && <SettingRow id="external_browser_executable" label="Chrome executable" description="Absolute path on the execution host; Whip does not download or discover an executable."><Input aria-label="Chrome executable" maxLength={4096} value={draft.executable} disabled={disabled} onChange={event => edit({ executable: event.target.value })}/></SettingRow>}
    {draft.mode === 'live' && <>
      <SettingRow id="external_browser_endpoint" label="Live Chrome endpoint" description="Literal loopback HTTP or browser WebSocket endpoint with a port, without credentials. Use either this field or the profile field."><Input aria-label="Live Chrome endpoint" maxLength={4096} value={draft.live_endpoint} disabled={disabled} onChange={event => edit({ live_endpoint: event.target.value, live_profile: '' })}/></SettingRow>
      <SettingRow id="external_browser_profile" label="Live Chrome profile" description="Absolute profile directory on this host containing DevToolsActivePort. No other profile is scanned."><Input aria-label="Live Chrome profile" maxLength={4096} value={draft.live_profile} disabled={disabled} onChange={event => edit({ live_profile: event.target.value, live_endpoint: '' })}/></SettingRow>
    </>}
    {draft.mode === 'extension' && <p>Run “whipcode browser install” on this host, load the printed extension folder in Chrome, then approve a named browser request and pin the desired tab. Reconnection requires a new human pin.</p>}
    {draft.mode !== 'disabled' && draft.mode !== 'live' && <SettingRow id="external_browser_private" label="Private network destinations" description="Allow private addresses in this mode. Metadata destinations remain blocked; page JavaScript is not a network sandbox."><Switch label="Allow private browser destinations" checked={draft.allow_private_urls} disabled={disabled} onCheckedChange={allow_private_urls => edit({ allow_private_urls })}/></SettingRow>}
    <p>Effective driver: {current.driver}{current.driver_pinned ? ' (pinned by this host process)' : ''}. Browser operations show their exact resource and generation before approval.</p>
    {(review || stale) && <p role="status">Read the current settings before another change. Your previous request may have arrived; it will not be replayed.</p>}
    <ErrorNotice type="action" owner={`${client.runtimeID}:external-browser`} title="Could not save external Chrome settings" error={error}/>
    {notice && <p role="status">{notice}</p>}
    <div {...stylex.props(layout.row, layout.wrap)}><Button type="submit" disabled={disabled || review || stale || !dirty}>Save external Chrome settings</Button><Button variant="ghost" disabled={disabled} onClick={() => void refresh()}>Discard edits and read external Chrome settings</Button></div>
  </form>;
}
