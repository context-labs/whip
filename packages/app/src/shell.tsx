import { useEffect, useRef, useState, type ReactNode } from 'react';
import { useNavigate, useParams } from '@tanstack/react-router';
import { useHotkey } from '@tanstack/react-hotkeys';
import {
  Button,
  IconButton,
  Input,
  Dialog,
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
import { SessionSearchDialog } from './session-search-dialog';
import { SessionSidebar } from './session-sidebar';
import { SidebarResize, useSidebarLayout } from './sidebar-layout';
import { useAppState, useRuntime, useSessionTabs } from './context';
import { inspectorSections, isInspectorSection } from './navigation';
import { ConnectionNotice } from './connection-notice';
import { SessionTabStrip, type SessionTabActions } from './session-tab-strip';

export function AppShell({ children }: { children: ReactNode }) {
  const runtime = useRuntime();
  const state = useAppState();
  const navigate = useNavigate();
  const params = useParams({ strict: false });
  useSessionTabs();
  const [compact, setCompact] = useState(() => window.matchMedia('(max-width: 767px)').matches);
  useEffect(() => { const query = window.matchMedia('(max-width: 767px)'); const update = () => setCompact(query.matches); query.addEventListener('change', update); return () => query.removeEventListener('change', update); }, []);
  const [navigation, setNavigation] = useState(false);
  const sidebar = useSidebarLayout();
  const toggleRef = useRef<HTMLButtonElement>(null);
  const [searchOpen, setSearchOpen] = useState(false);
  const searchOpener = useRef<HTMLElement | null>(null);
  const openSearch = () => {
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
  const [endpoint, setEndpoint] = useState(state.endpoint);
  const [connecting, setConnecting] = useState(false);
  const tabActions = useRef<SessionTabActions>(null);
  const focusComposer = () => {
    const id = state.client?.getSnapshot().info?.runtime_id;
    const tab = id ? selectedSessionTab(runtime.tabs.workspace(id)) : undefined;
    const container = tab ? document.getElementById(workspacePanelId(tab.id)) : undefined;
    container?.querySelector<HTMLTextAreaElement>('[data-whip-composer]')?.focus();
  };
  useHotkey(state.preferences.commandShortcut, () =>
    setCommands((value) => !value),
  );
  useHotkey(state.preferences.composerShortcut, focusComposer);
  useEffect(() => setEndpoint(state.endpoint), [state.endpoint]);
  const notices = <>
{state.client && <ConnectionNotice client={state.client} />}
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
    <div {...stylex.props(layout.shell)}>
      {!compact && !sidebar.state.hidden && <aside id="whip-session-navigation" {...stylex.props(layout.sidebar)} style={{ width: sidebar.width }} aria-label="Session navigation">
        <SessionSidebar state={sidebar.state} setState={sidebar.setState} onSearch={openSearch} headerAction={navigationToggle}
          onConnect={() => setConnection(true)} onNavigate={() => {}} />
        <SidebarResize width={sidebar.width} maxWidth={sidebar.maxWidth}
          onResize={width => sidebar.setState(value => ({ ...value, width }))}
          onHide={() => { sidebar.setState(value => ({ ...value, hidden: true })); requestAnimationFrame(() => toggleRef.current?.focus()); }} toggleRef={toggleRef} />
      </aside>}
      <div {...stylex.props(layout.main)}>
        {state.client ? <SessionTabStrip ref={tabActions} client={state.client} compact={compact}
          utilities={compact || sidebar.state.hidden ? navigationToggle : null} notices={notices}>{children}</SessionTabStrip>
          : <>{(compact || sidebar.state.hidden) && <div {...stylex.props(layout.row)}>{navigationToggle}</div>}{notices}{children}</>}
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
      <Dialog
        open={connection}
        onOpenChange={setConnection}
        title="Connect to an execution host"
        description="Your sessions and work run on this host."
      >
        <form
          {...stylex.props(layout.column)}
          onSubmit={async (event) => {
            event.preventDefault();
            setConnecting(true);
            try {
              await runtime.connect(endpoint);
              setConnection(false);
            } catch {
              // Setup and connection errors are displayed by their owners.
            } finally {
              setConnecting(false);
            }
          }}
        >
          {state.hosts.length > 0 && (
            <div
              {...stylex.props(layout.column)}
              aria-label="Saved execution hosts"
            >
              {state.hosts.map((host) => (
                <div key={host} {...stylex.props(layout.row)}>
                  <Button
                    type="button"
                    variant="ghost"
                    onClick={() => setEndpoint(host)}
                  >
                    {host}
                  </Button>
                  <IconButton
                    label={`Forget ${host}`}
                    onClick={() => {
                      try {
                        runtime.forgetHost(host);
                      } catch (error) {
                        runtime.report(error);
                      }
                    }}
                  >
                    <X size={13} />
                  </IconButton>
                </div>
              ))}
            </div>
          )}
          <label htmlFor="host-endpoint">Daemon address</label>
          <Input
            id="host-endpoint"
            value={endpoint}
            onChange={(event) => setEndpoint(event.target.value)}
            placeholder="http://localhost:8080"
            required
            autoFocus
          />
          <p {...stylex.props(layout.muted)}>
            Use the endpoint shown by <code>whip daemon status</code>. A phone
            or remote browser needs the host’s HTTPS address.
          </p>
          <Button type="submit" variant="primary" loading={connecting}>
            Connect
          </Button>
        </form>
      </Dialog>
      {state.client && state.list && <SessionSearchDialog key={state.endpoint} client={state.client} list={state.list}
        open={searchOpen} onOpenChange={setSearchOpen}
        finalFocus={() => searchOpener.current?.isConnected ? searchOpener.current : toggleRef.current} />}
      <CommandPicker
        open={commands}
        onOpenChange={setCommands}
        items={[
          { value: 'new', label: 'New session' },
          { value: 'focus', label: 'Focus message composer' },
          { value: 'navigation', label: 'Browse sessions' },
          { value: 'connect', label: 'Change execution host' },
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
          else if (action.startsWith('panel:') && params.runtimeId && params.rootId && isInspectorSection(action.slice(6))) void navigate({ to: '/h/$runtimeId/s/$rootId', params: { runtimeId: params.runtimeId, rootId: params.rootId }, state: { whipViewId: selectedSessionTab(runtime.tabs.workspace(params.runtimeId))?.id }, search: previous => ({ ...previous, panel: action.slice(6) as import('./navigation').InspectorSection }) });
          else void navigate({ to: '/settings', search: { section: action } });
        }}
      />
    </div>
  );
}
