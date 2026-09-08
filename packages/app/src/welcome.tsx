import { useEffect, useRef, useState } from 'react';
import { Link, useNavigate, useRouter, useSearch, useLocation } from '@tanstack/react-router';
import { useQuery } from '@tanstack/react-query';
import { useWhipConnection } from '@whip/sdk/react';
import { Button, Combobox, Input } from '@whip/ui';
import { ArrowRight, FolderOpen, Sparkles } from 'lucide-react';
import * as stylex from '@stylexjs/stylex';
import { useAppState, useRuntime } from './context';
import { layout } from './styles';
import type { WhipClient } from '@whip/sdk';
import { DirectoryPicker } from './directory-picker';
import { sessionSearch } from './session-tabs';

export function Welcome() {
  const { client } = useAppState();
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
      {client ? (
        <NewSession client={client} />
      ) : (
        <p>Connect to your execution host to begin.</p>
      )}
    </div>
  );
}
function NewSession({ client }: { client: WhipClient }) {
  const runtime = useRuntime();
  const { connection: profile } = useAppState();
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
  const [picking, setPicking] = useState(false);
  const pickerRequest = useRef(0);
  useEffect(() => {
    ++pickerRequest.current; setPicking(false);
    return () => { ++pickerRequest.current; };
  }, [client, profile.id, connection.info?.runtime_id, creationLocation]);
  async function pickLocalDirectory() {
    if (!runtime.platform.pickDirectory || picking || profile.target.kind !== 'local') return;
    const request = ++pickerRequest.current;
    const startingLocation = router.state.location;
    const runtimeId = connection.info?.runtime_id;
    const active = () => mounted.current && request === pickerRequest.current;
    const current = () => active()
      && router.state.location === startingLocation && runtime.getSnapshot().client === client
      && client.getSnapshot().info?.runtime_id === runtimeId
      && runtime.getSnapshot().connection.id === profile.id && runtime.getSnapshot().connection.target.kind === 'local';
    setPicking(true);
    try {
      const path = await runtime.platform.pickDirectory();
      if (path !== undefined && current()) setCwd(path);
    } catch (error) { if (current()) runtime.report(error); }
    finally { if (active()) setPicking(false); }
  }
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
          if (!mounted.current || router.state.location !== startingLocation || runtime.getSnapshot().client !== client) return;
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
        <FolderOpen size={14} /> Working directory on the host
      </label>
      <Input
        id="workspace-path"
        value={cwd}
        onChange={(event) => setCwd(event.target.value)}
        placeholder="/path/to/your/project"
        required
      />
      {profile.target.kind === 'local' && runtime.platform.pickDirectory ? <Button variant="secondary"
        disabled={!enabled || busy} loading={picking} onClick={() => void pickLocalDirectory()}>
        <FolderOpen size={14} /> Browse this Mac
      </Button> : <DirectoryPicker
        client={client}
        value={cwd}
        onSelect={setCwd}
        disabled={!enabled}
      />}
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
