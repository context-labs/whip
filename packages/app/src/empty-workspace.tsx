import { useCallback, useEffect, useMemo, useRef, useSyncExternalStore } from 'react';
import { useNavigate } from '@tanstack/react-router';
import { formatForDisplay } from '@tanstack/react-hotkeys';
import { Kbd } from '@whip/ui';
import { Plus } from 'lucide-react';
import * as stylex from '@stylexjs/stylex';
import { colors, scale, surface, typography } from '@whip/ui/tokens.stylex';
import { useAppState, useRuntime, useSessionTabs, useShellCommands } from './context';
import { openNewChat } from './session-tab-routing';
import { LocalRuntimeSetup } from './host-dialog';
import { layout } from './styles';

/**
 * The front door. After the last session closes the workspace lands in a New
 * Chat instead (see openAfterLastClose), so this renders on first run, after
 * storage loss, and for URLs whose tab is not open here.
 */
export function EmptyWorkspace({ missing = false, subject = 'draft' }: { missing?: boolean; subject?: 'draft' | 'terminal' | 'browser' }) {
  const runtime = useRuntime();
  const navigate = useNavigate();
  const { hosts, preferences } = useAppState();
  useSessionTabs();
  const commands = useShellCommands();
  const mounted = useRef(true);
  useEffect(() => { mounted.current = true; return () => { mounted.current = false; }; }, []);
  const closed = runtime.tabs.workspace().closed.length > 0;
  // First run: every host that has answered holds an empty catalog. Status is ignored; polls flip it to stale and back.
  const lists = useMemo(() => hosts.flatMap(host => host.list ? [host.list] : []), [hosts]);
  const subscribe = useCallback((listener: () => void) => { const offs = lists.map(list => list.subscribe(listener)); return () => { for (const off of offs) off(); }; }, [lists]);
  const firstRun = useSyncExternalStore(subscribe, () => !missing && lists.length > 0 && lists.every(list => { const page = list.getSnapshot().page; return !!page && !page.items?.length; }));
  const local = hosts.find(host => host.local);
  if (!missing && local?.profile.target.kind === 'local' && runtime.platform.localRuntime && local.state !== 'connected' && !hosts.some(host => !host.local && host.client)) {
    return <div data-empty-workspace="setup" {...stylex.props(layout.setupPage)}><div {...stylex.props(layout.setupColumn)}>
      <LocalRuntimeSetup host={local} onConnected={() => {
        if (mounted.current) openNewChat(runtime, navigate, { hostProfileId: local.id });
      }} />
    </div></div>;
  }
  const run = (action: string) => { if (commands) commands(action); else openNewChat(runtime, navigate); };
  const rows = [
    { action: 'new', label: 'New session', primary: true },
    { action: 'navigation', label: 'Search sessions' },
    { action: 'terminal:new', label: 'New terminal', shortcut: preferences.terminalShortcut },
    { action: 'commands', label: 'Commands', shortcut: preferences.commandShortcut },
    ...(closed ? [{ action: 'tabs:reopen', label: 'Reopen closed tab' }] : []),
  ].filter(row => commands || row.action === 'new');
  const heading = missing ? (subject === 'terminal' ? 'This terminal isn’t open here.' : 'This New Chat isn’t open here.') : 'What do you want to work on?';
  const note = missing
    ? subject === 'terminal' ? 'Its tab may belong to another window, or its shell has ended.' : 'Its tab may belong to another window, or the draft is no longer saved on this device.'
    : firstRun ? 'Choose a project folder on a host, then describe the task.' : undefined;
  return <div data-empty-workspace={missing ? 'missing' : 'frontdoor'} {...stylex.props(layout.empty)}>
    <div {...stylex.props(styles.column)}>
      <div {...stylex.props(styles.header)}>
        <h1 {...stylex.props(styles.heading)}>{heading}</h1>
        {note && <p {...stylex.props(styles.note)}>{note}</p>}
      </div>
      <div {...stylex.props(styles.rows)}>
        {rows.map(row => <button key={row.action} type="button" aria-label={row.label} onClick={() => run(row.action)}
          {...stylex.props(layout.subtleButton, styles.row, row.primary && styles.primary)}>
          <span {...stylex.props(styles.icon)}>{row.primary && <Plus size={16} aria-hidden="true" />}</span>
          <span {...stylex.props(styles.label, layout.ellipsis)}>{row.label}</span>
          {row.shortcut && <span aria-hidden="true"><Kbd xstyle={styles.kbd}>{formatForDisplay(row.shortcut)}</Kbd></span>}
        </button>)}
      </div>
    </div>
  </div>;
}

const styles = stylex.create({
  column: { width: 'min(100%, 380px)', display: 'flex', flexDirection: 'column', gap: scale.space4, textAlign: 'start' },
  header: { display: 'flex', flexDirection: 'column', gap: scale.space2 },
  heading: { fontSize: typography.size24, fontWeight: 550, lineHeight: '32px', letterSpacing: '-0.025em', margin: 0 },
  note: { fontSize: typography.size13, lineHeight: 1.6, color: surface.secondaryText, margin: 0 },
  rows: { display: 'grid', gridAutoRows: '1fr', gap: 2, marginInline: -8 },
  row: { width: '100%', fontSize: typography.size13, textAlign: 'start', color: { default: surface.secondaryText, ':hover': colors.foreground, ':focus-visible': colors.foreground } },
  primary: { color: colors.foreground, fontWeight: 550 },
  label: { flex: 1, minWidth: 0 },
  icon: { width: 16, display: 'inline-flex', justifyContent: 'center', flexShrink: 0 },
  kbd: { fontSize: typography.size12, lineHeight: '1.8', paddingInline: 6 },
});
