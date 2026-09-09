import { useEffect, useRef, useState, type ReactNode } from 'react';
import { useNavigate, useParams } from '@tanstack/react-router';
import { useHotkey } from '@tanstack/react-hotkeys';
import {
  Button,
  IconButton,
  Sheet,
  CommandPicker,
} from '@whip/ui';
import { workspacePanelId } from '@whip/ui/workspace-layout';
import { selectedSessionTab } from './session-tabs';
import {
  PanelLeft,
  X,
} from 'lucide-react';
import * as stylex from '@stylexjs/stylex';
import { layout } from './styles';
import { Attention, DesktopAttention } from './attention';
import { SessionActionsProvider } from './session-actions';
import { SessionSearchDialog } from './session-search-dialog';
import { SessionSidebar } from './session-sidebar';
import { SidebarResize, useSidebarLayout } from './sidebar-layout';
import { useAppState, useRuntime, useSessionTabs } from './context';
import { inspectorSections, isInspectorSection } from './navigation';
import { HostDialog } from './host-dialog';
import { ConnectionNotice } from './connection-notice';
import { SessionTabStrip, type SessionTabActions } from './session-tab-strip';

export function AppShell({ children }: { children: ReactNode }) {
  const runtime = useRuntime();
  const state = useAppState();
  const navigate = useNavigate();
  const params = useParams({ strict: false });
  const focusedHost = params.runtimeId ? state.hosts.find(host => host.runtimeId === params.runtimeId) : state.home;
  const client = focusedHost?.client;
  useSessionTabs();
  const [compact, setCompact] = useState(() => window.matchMedia('(max-width: 767px)').matches);
  useEffect(() => { const query = window.matchMedia('(max-width: 767px)'); const update = () => setCompact(query.matches); query.addEventListener('change', update); return () => query.removeEventListener('change', update); }, []);
  const [navigation, setNavigation] = useState(false);
  const sidebar = useSidebarLayout();
  const inset = runtime.platform.chrome === 'inset';
  const toggleRef = useRef<HTMLButtonElement>(null);
  const [searchOpen, setSearchOpen] = useState(false);
  const [searchStatus, setSearchStatus] = useState<'active' | 'archived' | 'all'>('active');
  const searchOpener = useRef<HTMLElement | null>(null);
  const openSearch = (status: 'active' | 'archived' | 'all' = 'active') => {
    setSearchStatus(status);
    searchOpener.current = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    setNavigation(false);
    setSearchOpen(true);
  };
  const toggleNavigation = () => {
    if (compact) setNavigation(value => !value);
    else {
      sidebar.setState(value => ({ ...value, hidden: !value.hidden }));
      requestAnimationFrame(() => toggleRef.current?.focus());
    }
  };
  const navigationToggle = <IconButton ref={toggleRef} variant="ghost"
    label={compact || sidebar.state.hidden ? 'Open navigation' : 'Hide navigation'}
    aria-expanded={compact ? navigation : !sidebar.state.hidden} aria-controls="whip-session-navigation"
    onClick={toggleNavigation}><PanelLeft size={18} /></IconButton>;
  const [connection, setConnection] = useState(false);
  const [commands, setCommands] = useState(false);
  const tabActions = useRef<SessionTabActions>(null);
  useEffect(() => runtime.platform.onCloseTab?.(() => {
    if (document.querySelector('[role="dialog"], [role="alertdialog"]')) return;
    if (!tabActions.current?.close()) runtime.platform.hideWindow?.();
  }), [runtime]);
  useEffect(() => {
    runtime.platform.setNotificationsEnabled?.(state.preferences.desktopNotifications);
    return () => runtime.platform.setNotificationsEnabled?.(false);
  }, [runtime, state.preferences.desktopNotifications]);
  const focusComposer = () => {
    const tab = selectedSessionTab(runtime.tabs.workspace());
    const container = tab ? document.getElementById(workspacePanelId(tab.id)) : undefined;
    container?.querySelector<HTMLTextAreaElement>('[data-whip-composer]')?.focus();
  };
  useHotkey(state.preferences.commandShortcut, () =>
    setCommands((value) => !value),
  );
  useHotkey(state.preferences.composerShortcut, focusComposer);
  useEffect(() => {
    const refresh = () => { void runtime.connections.refreshProfiles().catch(() => {}); };
    window.addEventListener('focus', refresh);
    return () => window.removeEventListener('focus', refresh);
  }, [runtime]);
  const notices = <>
{client && <ConnectionNotice client={client} />}
{focusedHost?.progress && <p role="status" {...stylex.props(layout.notice)}>{focusedHost.progress}</p>}
{runtime.connections.getSnapshot().notice && <div role="status" {...stylex.props(layout.notice)}>{runtime.connections.getSnapshot().notice} <Button variant="ghost" onClick={() => setConnection(true)}>Manage execution hosts</Button></div>}
        {state.error && (
          <div role="alert" {...stylex.props(layout.row, layout.notice)}>
            <span {...stylex.props(layout.grow)}>{state.error}</span>
            <IconButton
              label="Dismiss error"
              onClick={() => runtime.clearError()}
            >
              <X size={14} />
            </IconButton>
          </div>
        )}
  </>;
  return (
    <SessionActionsProvider><div {...stylex.props(layout.shell)}>
      {runtime.platform.notify && state.preferences.desktopNotifications && state.hosts.filter(host => host.client).map(host => <DesktopAttention key={`${host.id}:${host.runtimeId}`} client={host.client!} />)}
      {!compact && !sidebar.state.hidden && <aside id="whip-session-navigation" {...stylex.props(layout.sidebar)} style={{ width: sidebar.width }} aria-label="Session navigation">
        <SessionSidebar state={sidebar.state} setState={sidebar.setState} onSearch={openSearch} headerAction={navigationToggle}
          inset={inset} onConnect={() => setConnection(true)} onNavigate={() => {}} />
        <SidebarResize width={sidebar.width} maxWidth={sidebar.maxWidth}
          onResize={width => sidebar.setState(value => ({ ...value, width }))}
          onHide={() => { sidebar.setState(value => ({ ...value, hidden: true })); requestAnimationFrame(() => toggleRef.current?.focus()); }} toggleRef={toggleRef} />
      </aside>}
      <div {...stylex.props(layout.main)}>
        <SessionTabStrip ref={tabActions} compact={compact} onManageHosts={() => setConnection(true)} sidebarHidden={sidebar.state.hidden}
          utilities={<>{(compact || sidebar.state.hidden) && navigationToggle}<Attention /></>} notices={notices}>{children}</SessionTabStrip>
      </div>
      <Sheet xstyle={layout.sidebarSheet} open={compact && navigation} onOpenChange={setNavigation} title="WHIP">
        <div id="whip-session-navigation" {...stylex.props(layout.sidebar, layout.sidebarMobile)}>
          <SessionSidebar state={sidebar.state} setState={sidebar.setState} onSearch={openSearch}
            onConnect={() => {
              setNavigation(false);
              setConnection(true);
            }}
            onNavigate={() => setNavigation(false)}
          />
        </div>
      </Sheet>
      <HostDialog open={connection} onOpenChange={setConnection} />
      <SessionSearchDialog
        open={searchOpen} initialStatus={searchStatus} onOpenChange={setSearchOpen}
        finalFocus={() => searchOpener.current?.isConnected ? searchOpener.current : toggleRef.current} />
      <CommandPicker
        open={commands}
        onOpenChange={setCommands}
        items={[
          { value: 'new', label: 'New session' },
          { value: 'focus', label: 'Focus message composer' },
          { value: 'navigation', label: 'Browse sessions' },
          { value: 'connect', label: 'Manage execution hosts' },
          { value: 'tabs:search', label: 'Search open tabs' },
          { value: 'tabs:next', label: 'Next session tab' },
          { value: 'tabs:previous', label: 'Previous session tab' },
          { value: 'tabs:close', label: 'Close session tab' },
          { value: 'tabs:reopen', label: 'Reopen closed tab' },
          ...(params.rootId ? inspectorSections.map(item => ({ value: `panel:${item.value}`, label: item.label })) : []),
          ...['appearance', 'providers', 'runtime', 'device', 'recovery'].map(
            (value) => ({
              value,
              label: `${value[0]!.toUpperCase()}${value.slice(1)} settings`,
            }),
          ),
        ]}
        onSelect={(action) => {
          if (action === 'new') void navigate({ to: '/', search: {} });
          else if (action === 'focus') requestAnimationFrame(focusComposer);
          else if (action === 'navigation') openSearch();
          else if (action === 'connect') setConnection(true);
          else if (action === 'tabs:search') tabActions.current?.showPicker();
          else if (action === 'tabs:next') tabActions.current?.next(1);
          else if (action === 'tabs:previous') tabActions.current?.next(-1);
          else if (action === 'tabs:close') tabActions.current?.close();
          else if (action === 'tabs:reopen') tabActions.current?.reopen();
          else if (action.startsWith('panel:') && params.runtimeId && params.rootId && isInspectorSection(action.slice(6))) void navigate({ to: '/h/$runtimeId/s/$rootId', params: { runtimeId: params.runtimeId, rootId: params.rootId }, state: { whipViewId: selectedSessionTab(runtime.tabs.workspace())?.id }, search: previous => ({ ...previous, panel: action.slice(6) as import('./navigation').InspectorSection }) });
          else void navigate({ to: '/settings', search: { section: action } });
        }}
      />
    </div></SessionActionsProvider>
  );
}
