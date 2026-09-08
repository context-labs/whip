import { useEffect, useRef, useState, type ReactNode } from 'react';
import { useQuery } from '@tanstack/react-query';
import { useSessionView } from '@whip/sdk/react';
import type { DeepReadonly, SessionView } from '@whip/sdk/state';
import type {
  ContentHandle,
  QueryOperation,
  RootSnapshot,
  RuntimeOperations,
} from '@whip/protocol';
import { Alert, Button, CodeBlock } from '@whip/ui';
import * as stylex from '@stylexjs/stylex';
import { useRuntime } from '../context';
import { layout } from '../styles';

export interface InspectorProps {
  view: SessionView;
  root: DeepReadonly<RootSnapshot>;
  connected: boolean;
  agentId: string;
  viewId?: string;
  kind?: 'chat' | 'repl';
}
export type Value = NonNullable<RootSnapshot['blackboard']>[number]['payload'];
export function useDetailQuery<O extends QueryOperation>(
  props: Pick<InspectorProps, 'view' | 'connected'>,
  operation: O,
  params: RuntimeOperations[O]['params'],
  poll = false,
) {
  const { session } = props.view;
  const supported = session.client.supports('runtime', operation);
  const query = useQuery({
    queryKey: [
      'inspector',
      session.client.getSnapshot().info?.runtime_id,
      session.rootId,
      operation,
      params,
    ],
    queryFn: ({ signal }) => session.query(operation, params, { signal }),
    enabled: props.connected && supported,
    gcTime: 0,
    refetchInterval: props.connected && poll ? 3000 : false,
  });
  return { ...query, supported };
}
export function QueryFeedback({
  query,
  view,
}: {
  query: {
    error: Error | null;
    isLoading: boolean;
    supported: boolean;
    data?: { content?: ContentHandle | null };
  };
  view: SessionView;
}) {
  if (!query.supported)
    return (
      <Alert title="Unavailable on this host">
        The execution host does not offer this operation.
      </Alert>
    );
  if (query.error) return <Alert tone="error">{query.error.message}</Alert>;
  if (query.isLoading)
    return (
      <p role="status" {...stylex.props(layout.muted)}>
        Loading from the execution host…
      </p>
    );
  if (query.data?.content)
    return <ContentRead view={view} value={query.data.content} label="Large result" />;
  return null;
}
export function Action({
  children,
  run,
  disabled,
  danger = false,
}: {
  children: ReactNode;
  run: () => Promise<unknown>;
  disabled?: boolean;
  danger?: boolean;
}) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [done, setDone] = useState(false);
  return (
    <div {...stylex.props(layout.column)}>
      <Button
        size="sm"
        variant={danger ? 'danger' : 'secondary'}
        disabled={disabled || busy}
        loading={busy}
        onClick={() => {
          if (disabled || busy) return;
          setBusy(true);
          setError('');
          setDone(false);
          void run()
            .then(
              () => setDone(true),
              (error) => setError(error instanceof Error ? error.message : String(error)),
            )
            .finally(() => setBusy(false));
        }}
      >
        {children}
      </Button>
      {error && (
        <p role="alert" {...stylex.props(layout.error)}>
          {error}
        </p>
      )}
      {done && (
        <span role="status" {...stylex.props(layout.muted)}>
          Applied
        </span>
      )}
    </div>
  );
}
export function Section({
  title,
  description,
  children,
}: {
  title: string;
  description?: string;
  children: ReactNode;
}) {
  return (
    <section {...stylex.props(layout.column)}>
      <h3 {...stylex.props(layout.title)}>{title}</h3>
      {description && <p {...stylex.props(layout.muted)}>{description}</p>}
      {children}
    </section>
  );
}
export function Empty({ children }: { children: ReactNode }) {
  return <p {...stylex.props(layout.muted)}>{children}</p>;
}
export function mergeBy<T>(
  initial: readonly T[],
  extra: readonly T[],
  key: (value: T) => string,
): T[] {
  return [...new Map([...initial, ...extra].map((value) => [key(value), value])).values()];
}
export function useCollection(view: SessionView, name: string) {
  const state = useSessionView(view);
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);
  const page = state.collections[name];
  return {
    view,
    page,
    loading,
    error,
    async load(more: boolean) {
      if (loading) return;
      setLoading(true);
      setError('');
      try {
        await view.loadCollection(name, { more });
      } catch (error) {
        if (
          error &&
          typeof error === 'object' &&
          'kind' in error &&
          error.kind === 'resynchronization_required'
        ) {
          setError('This collection changed. Loaded its current first page.');
          try {
            await view.loadCollection(name);
          } catch (next) {
            setError(next instanceof Error ? next.message : String(next));
          }
        } else setError(error instanceof Error ? error.message : String(error));
      } finally {
        setLoading(false);
      }
    },
  };
}
export function CollectionMore({
  collection,
  omitted,
  connected,
}: {
  collection: ReturnType<typeof useCollection>;
  omitted?: boolean;
  connected: boolean;
}) {
  const more = collection.page ? collection.page.has_more : omitted;
  return (
    <>
      {collection.error && <p role="status">{collection.error}</p>}
      {collection.page?.items?.map(
        (entry) =>
          entry.body && (
            <ContentRead
              key={entry.body.reference_id}
              view={collection.view}
              value={entry.body}
              label="Large collection entry"
            />
          ),
      )}
      {more && (
        <Button
          variant="ghost"
          disabled={!connected || collection.loading}
          loading={collection.loading}
          onClick={() => void collection.load(!!collection.page)}
        >
          Load more
        </Button>
      )}
    </>
  );
}
export function valueText(
  value: DeepReadonly<ContentHandle & Partial<Pick<Value, 'text' | 'inline' | 'binary'>>>,
): string | undefined {
  if (value.text != null) return value.text;
  if (value.inline !== undefined) return JSON.stringify(value.inline, null, 2);
  if (value.binary != null)
    return new TextDecoder('utf-8', { fatal: true }).decode(
      Uint8Array.from(atob(value.binary), (character) => character.charCodeAt(0)),
    );
  return undefined;
}
export function ContentRead({
  view,
  value,
  referenceId,
  agentId = view.session.rootId,
  label = 'Content',
}: {
  view: SessionView;
  value?: DeepReadonly<Value> | ContentHandle;
  referenceId?: string;
  agentId?: string;
  label?: string;
}) {
  const runtime = useRuntime();
  const connected = view.session.client.getSnapshot().state === 'connected';
  const controller = useRef<AbortController | null>(null);
  const [body, setBody] = useState<string>();
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  useEffect(() => () => controller.current?.abort(), []);
  let inline: string | undefined;
  try {
    if (value) inline = valueText(value);
  } catch {
    /* Binary content stays behind an explicit download. */
  }
  const reference = referenceId || value?.reference_id;
  async function read(download: boolean) {
    controller.current?.abort();
    controller.current = new AbortController();
    const signal = controller.current.signal;
    setBusy(true);
    setError('');
    try {
      const scope = { rootId: view.session.rootId, agentId };
      const handle = value?.reference_id
        ? value
        : (
            await view.session.client.call(
              'content.read',
              {
                root_id: scope.rootId,
                agent_id: agentId,
                reference_id: reference!,
                offset: '0',
                limit: 1,
              },
              { signal },
            )
          ).content;
      const content = view.session.client.content(handle, scope);
      if (download)
        await runtime.platform.download(
          await content.readBytes({ maxBytes: 64 << 20, signal }),
          'whip-content',
          handle.media_type ?? 'application/octet-stream',
        );
      else setBody(await content.readText({ maxBytes: 1 << 20, signal }));
    } catch (error) {
      if (!signal.aborted) setError(error instanceof Error ? error.message : String(error));
    } finally {
      if (!signal.aborted) setBusy(false);
    }
  }
  return (
    <div {...stylex.props(layout.column)}>
      {(body ?? inline) !== undefined && (
        <CodeBlock label={label} code={body ?? inline ?? ''} maxBytes={128 << 10} />
      )}
      {reference && (
        <>
          <span {...stylex.props(layout.muted)}>
            {label}
            {value && ` · ${value.size} bytes`}
          </span>
          <div {...stylex.props(layout.row, layout.wrap)}>
            <Button
              variant="ghost"
              size="sm"
              disabled={busy || !connected}
              onClick={() => void read(false)}
            >
              Read {label.toLowerCase()}
            </Button>
            <Button
              variant="ghost"
              size="sm"
              disabled={busy || !connected}
              onClick={() => void read(true)}
            >
              Download
            </Button>
          </div>
        </>
      )}
      {error && <p role="alert">{error}</p>}
    </div>
  );
}
