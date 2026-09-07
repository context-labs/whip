import { useEffect, useRef, useState, type ReactNode } from 'react';
import { Link, useLocation, useNavigate, useParams } from '@tanstack/react-router';
import { useQuery } from '@tanstack/react-query';
import type { SessionCatalogPage } from '@whip/protocol';
import type { DeepReadonly } from '@whip/sdk/state';
import { useVirtualizer } from '@tanstack/react-virtual';
import { useHotkey } from '@tanstack/react-hotkeys';
import { useWhipConnection, useSessionListView } from '@whip/sdk/react';
import type { WhipClient } from '@whip/sdk';
import type { SessionListView } from '@whip/sdk/state';
import {
  Button,
  IconButton,
  Input,
  Dialog,
  Sheet,
  Badge,
  ThemePicker,
  CommandPicker,
} from '@whip/ui';
import {
  MessageSquare,
  Plus,
  Search,
  Settings2,
  PanelLeft,
  Plug,
  X,
  ArrowUpRight,
} from 'lucide-react';
import * as stylex from '@stylexjs/stylex';
import { layout } from './styles';
import { useAppState, useRuntime } from './context';
import { inspectorSections, isInspectorSection } from './navigation';
import { Attention } from './attention';

export function AppShell({ children }: { children: ReactNode }) {
  const runtime = useRuntime();
  const state = useAppState();
  const navigate = useNavigate();
  const params = useParams({ strict: false });
  const [compact, setCompact] = useState(() => window.matchMedia('(max-width: 767px)').matches);
  useEffect(() => { const query = window.matchMedia('(max-width: 767px)'); const update = () => setCompact(query.matches); query.addEventListener('change', update); return () => query.removeEventListener('change', update); }, []);
  const [navigation, setNavigation] = useState(false);
  const [connection, setConnection] = useState(false);
  const [commands, setCommands] = useState(false);
  const [endpoint, setEndpoint] = useState(state.endpoint);
  const [connecting, setConnecting] = useState(false);
  const focusComposer = () =>
    document
      .querySelector<HTMLTextAreaElement>('[data-whip-composer]')
      ?.focus();
  useHotkey(state.preferences.commandShortcut, () =>
    setCommands((value) => !value),
  );
  useHotkey(state.preferences.composerShortcut, focusComposer);
  useEffect(() => setEndpoint(state.endpoint), [state.endpoint]);
  return (
    <div {...stylex.props(layout.shell)}>
      {!compact && <aside {...stylex.props(layout.sidebar)} aria-label="Session navigation">
        <Navigation
          onConnect={() => setConnection(true)}
          onNavigate={() => {}}
        />
      </aside>}
      <div {...stylex.props(layout.main)}>
        <header {...stylex.props(layout.header)}>
          <IconButton
            label="Open navigation"
            xstyle={layout.mobileOnly}
            onClick={() => setNavigation(true)}
          >
            <PanelLeft size={18} />
          </IconButton>
          {state.client ? (
            <ConnectionBadge
              client={state.client}
              onConnect={() => setConnection(true)}
            />
          ) : (
            <Button variant="ghost" onClick={() => setConnection(true)}>
              <Plug size={14} /> Connect to host
            </Button>
          )}
          <span {...stylex.props(layout.grow)} />
          {state.client && <Attention client={state.client} />}
          <ThemePicker compact />
          <IconButton label="Commands" xstyle={layout.desktopOnly} onClick={() => setCommands(true)}>
            <Search size={16} />
          </IconButton>
          <Link
            to="/settings"
            aria-label="Settings"
            {...stylex.props(layout.subtleButton, layout.desktopOnly)}
          >
            <Settings2 size={16} />
          </Link>
        </header>
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
        {children}
      </div>
      <Sheet open={navigation} onOpenChange={setNavigation} title="WHIP">
        <div {...stylex.props(layout.sidebar, layout.sidebarMobile)}>
          <Navigation
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
            } catch (error) {
              runtime.report(error);
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
      <CommandPicker
        open={commands}
        onOpenChange={setCommands}
        items={[
          { value: 'new', label: 'New session' },
          { value: 'focus', label: 'Focus message composer' },
          { value: 'navigation', label: 'Browse sessions' },
          { value: 'connect', label: 'Change execution host' },
          ...(params.rootId ? inspectorSections.map(item => ({ value: `panel:${item.value}`, label: item.label })) : []),
          ...['appearance', 'providers', 'runtime', 'device', 'recovery'].map(
            (value) => ({
              value,
              label: `${value[0]!.toUpperCase()}${value.slice(1)} settings`,
            }),
          ),
        ]}
        onSelect={(action) => {
          if (action === 'new') void navigate({ to: '/' });
          else if (action === 'focus') requestAnimationFrame(focusComposer);
          else if (action === 'navigation') setNavigation(true);
          else if (action === 'connect') setConnection(true);
          else if (action.startsWith('panel:') && params.runtimeId && params.rootId && isInspectorSection(action.slice(6))) void navigate({ to: '/h/$runtimeId/s/$rootId', params: { runtimeId: params.runtimeId, rootId: params.rootId }, search: previous => ({ ...previous, panel: action.slice(6) as import('./navigation').InspectorSection }) });
          else void navigate({ to: '/settings', search: { section: action } });
        }}
      />
    </div>
  );
}

function ConnectionBadge({
  client,
  onConnect,
}: {
  client: WhipClient;
  onConnect(): void;
}) {
  const connection = useWhipConnection(client);
  return (
    <button
      {...stylex.props(layout.subtleButton)}
      onClick={onConnect}
      aria-label="Connection settings"
    >
      <Badge tone={connection.state === 'connected' ? 'success' : 'warning'}>
        {connection.state === 'connected' ? 'Connected' : connection.state}
      </Badge>
      <span {...stylex.props(layout.muted, layout.desktopOnly)}>
        {connection.info?.host_platform ?? 'Execution host'}
      </span>
    </button>
  );
}

function Navigation({
  onConnect,
  onNavigate,
}: {
  onConnect(): void;
  onNavigate(): void;
}) {
  const state = useAppState();
  return (
    <>
      <div {...stylex.props(layout.sidebarHeader)}>
        <Link
          to="/"
          onClick={onNavigate}
          {...stylex.props(layout.brand, layout.subtleButton)}
        >
          WHIP
        </Link>
        <span {...stylex.props(layout.grow)} />
        <Link
          to="/"
          onClick={onNavigate}
          aria-label="New session"
          {...stylex.props(layout.subtleButton)}
        >
          <Plus size={18} />
        </Link>
      </div>
      {state.client && state.list ? (
        <SessionNavigation
          client={state.client}
          list={state.list}
          onNavigate={onNavigate}
        />
      ) : (
        <Button onClick={onConnect}>Connect to host</Button>
      )}
      <div {...stylex.props(layout.column)}>
        <Link
          to="/settings"
          onClick={onNavigate}
          {...stylex.props(layout.subtleButton)}
        >
          <Settings2 size={15} /> Settings
        </Link>
        <button {...stylex.props(layout.subtleButton)} onClick={onConnect}>
          <Plug size={15} />
          <span {...stylex.props(layout.ellipsis)}>{state.endpoint}</span>
          <ArrowUpRight size={13} />
        </button>
      </div>
    </>
  );
}

function SessionNavigation({
  client,
  list,
  onNavigate,
}: {
  client: WhipClient;
  list: SessionListView;
  onNavigate(): void;
}) {
  const [search, setSearch] = useState('');
  const [term, setTerm] = useState('');
  useEffect(() => {
    const timer = setTimeout(() => setTerm(search.trim()), 200);
    return () => clearTimeout(timer);
  }, [search]);
  return (
    <>
      <Input
        aria-label="Search sessions on this host"
        placeholder="Find a session…"
        value={search}
        onChange={(event) => setSearch(event.target.value)}
      />
      <span {...stylex.props(layout.eyebrow)}>SESSIONS</span>
      {term ? (
        <SearchSessions
          key={term}
          term={term}
          client={client}
          onNavigate={onNavigate}
        />
      ) : (
        <ObservedSessions client={client} list={list} onNavigate={onNavigate} />
      )}
    </>
  );
}
function ObservedSessions({
  client,
  list,
  onNavigate,
}: {
  client: WhipClient;
  list: SessionListView;
  onNavigate(): void;
}) {
  const catalog = useSessionListView(list);
  const runtime = useRuntime();
  return (
    <SessionRows
      client={client}
      page={catalog.page}
      loading={catalog.status === 'loading'}
      error={catalog.error?.message}
      onNavigate={onNavigate}
      loadMore={() =>
        void list.loadMore().catch((error) => runtime.report(error))
      }
    />
  );
}
function SearchSessions({
  client,
  term,
  onNavigate,
}: {
  client: WhipClient;
  term: string;
  onNavigate(): void;
}) {
  const connection = useWhipConnection(client);
  const [cursor, setCursor] = useState<SessionCatalogPage['next_cursor']>();
  const query = useQuery({
    queryKey: ['session-search', term, cursor],
    queryFn: ({ signal }) =>
      client.sessions.list(
        {
          search: term,
          ...(cursor ? { cursor } : {}),
          limit: 64,
          max_bytes: 256 << 10,
        },
        { signal },
      ),
    enabled: connection.state === 'connected',
  });
  return (
    <>
      <SessionRows
        client={client}
        page={query.data}
        loading={query.isFetching}
        error={query.error?.message}
        onNavigate={onNavigate}
        loadMore={() => setCursor(query.data?.next_cursor)}
      />
      {cursor && (
        <Button variant="ghost" onClick={() => setCursor(undefined)}>
          First results
        </Button>
      )}
      {query.error && (
        <Button
          variant="ghost"
          onClick={() => {
            if (cursor) setCursor(undefined);
            else void query.refetch();
          }}
        >
          Refresh search
        </Button>
      )}
    </>
  );
}
function SessionRows({
  client,
  page,
  loading,
  error,
  onNavigate,
  loadMore,
}: {
  client: WhipClient;
  page?: DeepReadonly<SessionCatalogPage>;
  loading: boolean;
  error?: string;
  onNavigate(): void;
  loadMore(): void;
}) {
  const connection = useWhipConnection(client);
  const scroll = useRef<HTMLDivElement>(null);
  const location = useLocation();
  const items = page?.items ?? [];
  const virtual = useVirtualizer({
    count: items.length,
    getScrollElement: () => scroll.current,
    estimateSize: () => 54,
    overscan: 5,
    getItemKey: (index) => items[index]!.id,
  });
  return (
    <div ref={scroll} {...stylex.props(layout.sessionList)}>
      <div style={{ height: virtual.getTotalSize(), position: 'relative' }}>
        {virtual.getVirtualItems().map((row) => {
          const session = items[row.index]!;
          return (
            <div
              key={session.id}
              data-index={row.index}
              ref={virtual.measureElement}
              style={{
                position: 'absolute',
                width: '100%',
                top: 0,
                transform: `translateY(${row.start}px)`,
              }}
            >
              <Link
                to="/h/$runtimeId/s/$rootId"
                params={{
                  runtimeId: connection.info?.runtime_id ?? '',
                  rootId: session.id,
                }}
                search={{}}
                preload={false}
                onClick={onNavigate}
                {...stylex.props(
                  layout.sessionLink,
                  location.pathname.endsWith(`/s/${session.id}`) &&
                    layout.selected,
                )}
              >
                <MessageSquare size={15} strokeWidth={1.5} />
                <div {...stylex.props(layout.grow)}>
                  <div {...stylex.props(layout.ellipsis)}>
                    {session.title || 'Untitled session'}
                  </div>
                  <div {...stylex.props(layout.muted, layout.ellipsis)}>
                    {session.cwd}
                  </div>
                </div>
              </Link>
            </div>
          );
        })}
      </div>
      {!items.length && (
        <p {...stylex.props(layout.muted)}>
          {loading ? 'Loading sessions…' : 'No matching sessions.'}
        </p>
      )}
      {page?.has_more && (
        <Button variant="ghost" disabled={loading} onClick={loadMore}>
          Load more sessions
        </Button>
      )}
      {error && <p role="alert">{error}</p>}
    </div>
  );
}
