import { useEffect, useRef, useState, useSyncExternalStore } from 'react';
import { Button } from '@whip/ui';
import * as stylex from '@stylexjs/stylex';
import { useRuntime } from '../context';
import { layout } from '../styles';
import type { AppUpdates } from '../platform';
import { SettingsGroup } from './section-layout';

export function AboutSettings() {
  const { platform } = useRuntime();
  return <SettingsGroup title="Whipcode">
    <p {...stylex.props(layout.muted)}>A workspace for directing coding work across your execution hosts.</p>
    {platform.updates ? <UpdateSettings updates={platform.updates} /> : <p>This browser connects to the web app served by your Whip host. Update whipcode on that host to update the web app.</p>}
  </SettingsGroup>;
}

function UpdateSettings({ updates }: { updates: AppUpdates }) {
  const snapshot = useSyncExternalStore(updates.subscribe, updates.getSnapshot, updates.getSnapshot);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const mounted = useRef(true);
  useEffect(() => { mounted.current = true; return () => { mounted.current = false; }; }, []);
  useEffect(() => setError(''), [snapshot]);
  async function perform(install: boolean) {
    if (busy || (install && updates.getSnapshot().state !== 'downloaded')) return;
    setBusy(true); setError('');
    try { if (install) await updates.install(); else await updates.check(); }
    catch (value) { if (mounted.current) setError(value instanceof Error ? value.message : String(value)); }
    finally { if (mounted.current) setBusy(false); }
  }
  const working = busy || snapshot.state === 'checking' || snapshot.state === 'available';
  const version = snapshot.version ? ` ${snapshot.version}` : '';
  return <section id="updates" tabIndex={-1} {...stylex.props(layout.column)} aria-label="Application updates">
    <h2>Application updates</h2>
    <p {...stylex.props(layout.muted)}>Whip {updates.currentVersion}</p>
    {snapshot.state !== 'idle' && snapshot.state !== 'error' && <p role="status">{
      snapshot.state === 'checking' ? 'Checking for updates…'
      : snapshot.state === 'available' ? `Downloading Whip${version}…`
      : snapshot.state === 'downloaded' ? `Whip${version} is ready. Restart the app to install it.`
      : 'Whip is up to date.'
    }</p>}
    {(error || snapshot.state === 'error') && <p role="alert">{error || snapshot.error || 'Updates could not be checked. Try again.'}</p>}
    <div {...stylex.props(layout.row, layout.wrap)}>
      <Button variant="secondary" disabled={working || snapshot.state === 'downloaded'} onClick={() => void perform(false)}>Check for updates</Button>
      {snapshot.state === 'downloaded' && <Button variant="primary" loading={busy} onClick={() => void perform(true)}>Restart to update</Button>}
    </div>
  </section>;
}

