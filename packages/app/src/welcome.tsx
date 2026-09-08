import { useEffect, useRef, useState } from 'react';
import { Link, useNavigate, useRouter, useSearch, useLocation } from '@tanstack/react-router';
import { useQuery } from '@tanstack/react-query';
import { useWhipConnection } from '@whip/sdk/react';
import { Button, Combobox, Input, Select } from '@whip/ui';
import { ArrowRight, FolderOpen, Sparkles } from 'lucide-react';
import * as stylex from '@stylexjs/stylex';
import { useAppState, useRuntime } from './context';
import { layout } from './styles';
import type { WhipClient } from '@whip/sdk';
import { HostDialog } from './host-dialog';
import type { HostConnection } from './hosts';
import { DirectoryPicker } from './directory-picker';
import { sessionSearch } from './session-tabs';

export function Welcome() {
  const runtime = useRuntime();
  const { hosts, selectedHostId } = useAppState();
  const search = useSearch({ from: '/' });
  const locationKey = useLocation({ select: location => location.state.__TSR_key });
  const [selected, setSelected] = useState(search.runtimeId ?? selectedHostId ?? 'local');
  const [adding, setAdding] = useState(false);
  useEffect(() => setSelected(search.runtimeId ?? selectedHostId ?? 'local'), [search.runtimeId, locationKey]);
  const host = hosts.find(host => host.id === selected || host.runtimeId === selected);
  const remote = selected !== 'local' && !host?.local;
  const remotes = hosts.filter(host => !host.local);
  return (
    <div {...stylex.props(layout.empty)}>
      <Sparkles size={28} strokeWidth={1.3} />
      <h1 {...stylex.props(layout.emptyTitle)}>
        What would you like to work on?
      </h1>
      <p {...stylex.props(layout.emptyText)}>
        Start a conversation. Give your agents a direction, follow their
        progress, and step in when they need you.
      </p>
      <div {...stylex.props(layout.column)} style={{ width: 'min(100%, 420px)', textAlign: 'left' }}>
        <div role="group" aria-label="Session location" {...stylex.props(layout.row)}>
          <Button variant={remote ? 'ghost' : 'secondary'} aria-pressed={!remote} onClick={() => setSelected('local')}>Local</Button>
          <Button variant={remote ? 'secondary' : 'ghost'} aria-pressed={remote} onClick={() => setSelected(remotes[0]?.id ?? 'remote')}>Remote</Button>
        </div>
        {remote && <div {...stylex.props(layout.column)}>
          {!!remotes.length && <Select label="Execution host" value={host?.id ?? ''} options={remotes.map(host => ({ value: host.id, label: host.name }))} onValueChange={setSelected} />}
          <Button variant="ghost" onClick={() => setAdding(true)}>Add remote host</Button>
        </div>}
        {host?.client ? <NewSession key={`${host.id}:${host.runtimeId}`} client={host.client} host={host} />
          : host ? <><p>{host.name} is {host.state === 'closed' ? 'disconnected' : host.state}.</p><Button onClick={() => void runtime.connections.connect(host.id).catch(() => {})}>Connect {host.name}</Button></>
          : <p>Select or add a remote host to begin.</p>}
        {host?.error && <p role="status">{host.error}</p>}
      </div>
      <HostDialog open={adding} onOpenChange={setAdding} onSaved={id => { runtime.connections.select(id); setSelected(id); }} />
    </div>
  );
}
function NewSession({ client, host }: { client: WhipClient; host: HostConnection }) {
  const runtime = useRuntime();
  const connection = useWhipConnection(client);
  const navigate = useNavigate();
  const router = useRouter();
  const mounted = useRef(true);
  useEffect(() => { mounted.current = true; return () => { mounted.current = false; }; }, []);
  const search = useSearch({ from: '/' });
  const prefill = search.runtimeId === connection.info?.runtime_id ? search.cwd ?? '' : '';
  const creationLocation = useLocation({ select: location => location.state.__TSR_key });
  const [cwd, setCwd] = useState(prefill);
  useEffect(() => setCwd(prefill), [prefill, connection.info?.runtime_id, creationLocation]);
  const [busy, setBusy] = useState(false);
  const [model, setModel] = useState('');
  const previous = runtime.lastSession();
  const previousView = previous ? runtime.tabs.preferred(previous.runtimeId, previous.rootId) : undefined;
  const enabled = connection.state === 'connected';
  const catalogs = useQuery({
    queryKey: ['provider-catalogs', connection.info?.runtime_id],
    queryFn: ({ signal }) => client.providers.catalogs({ signal }),
    enabled,
  });
  const models = Object.entries(catalogs.data?.result?.models ?? {}).flatMap(
    ([name, info]) =>
      (info.providers ?? []).map((provider) => ({
        value: JSON.stringify([name, provider]),
        label: `${name} · ${provider}`,
      })),
  );
  return (
    <form
      {...stylex.props(layout.column)}
      style={{ width: 'min(100%, 420px)', textAlign: 'left' }}
      onSubmit={async (event) => {
        event.preventDefault();
        const runtimeId = connection.info?.runtime_id;
        if (!runtimeId || !runtime.tabs.canOpen(runtimeId)) {
          runtime.report('There are 32 open session tabs. Close a tab before creating another session.');
          return;
        }
        const startingLocation = router.state.location;
        setBusy(true);
        try {
          const selection: [string, string] = model
            ? JSON.parse(model)
            : ['', ''];
          const outcome = await runtime.run(
            client.sessions.create({
              cwd,
              model: selection[0],
              provider: selection[1],
            }),
            'Create session',
          );
          if (!outcome.result)
            throw new Error('Session creation returned no session');
          if (!mounted.current || router.state.location !== startingLocation || !runtime.connections.isAttached(client)) return;
          await navigate({
            to: '/h/$runtimeId/s/$rootId',
            params: {
              runtimeId,
              rootId: outcome.result.root_id,
            },
            search: {},
          });
        } catch (error) {
          runtime.report(error);
        } finally {
          setBusy(false);
        }
      }}
    >
      {previous && previous.runtimeId === connection.info?.runtime_id && (
        <Link to="/h/$runtimeId/s/$rootId" params={previous} search={sessionSearch(previousView)} state={{ whipViewId: previousView?.id }}>
          Continue your previous session
        </Link>
      )}
      <label
        htmlFor="workspace-path"
        {...stylex.props(layout.row, layout.muted)}
      >
        <FolderOpen size={14} /> Working directory on {host.name}
      </label>
      <Input
        id="workspace-path"
        value={cwd}
        onChange={(event) => setCwd(event.target.value)}
        placeholder="/path/to/your/project"
        required
      />
      <DirectoryPicker
        key={creationLocation}
        client={client}
        native={host.local}
        pickDirectory={host.profile?.target.kind === 'local' ? runtime.platform.pickDirectory : undefined}
        value={cwd}
        onSelect={setCwd}
        disabled={!enabled}
      />
      {!!models.length && (
        <Combobox
          label="Model for new session"
          value={model}
          onValueChange={setModel}
          options={[{ value: '', label: 'Use host default model' }, ...models]}
        />
      )}
      <Button
        variant="primary"
        type="submit"
        loading={busy}
        disabled={!enabled || !cwd.trim()}
      >
        Start a session <ArrowRight size={15} />
      </Button>
      <Link
        to="/settings"
        search={{ section: 'providers' }}
        {...stylex.props(layout.muted)}
      >
        Connect or configure a model provider
      </Link>
    </form>
  );
}
