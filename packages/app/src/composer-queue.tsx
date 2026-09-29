import { memo, useEffect, useRef, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { useVirtualizer } from '@tanstack/react-virtual';
import { RemoteError, type Session } from '@whip/sdk';
import { Button, Dialog, Spinner, Tooltip } from '@whip/ui';
import { CornerDownRight, ListEnd, Paperclip, Trash2 } from 'lucide-react';
import * as stylex from '@stylexjs/stylex';
import { colors, scale, surface, typography } from '@whip/ui/tokens.stylex';
import { useAppState, useRuntime } from './context';
import { type QueuedInputRow } from './input-presentation';
import { ErrorNotice } from './error-feedback';
import { MessageAttachments } from './message-attachments';
import { InputAttachment } from './input-attachment';
import { DesignInputAttachments } from './design-input-attachments';
import { layout } from './styles';
import { composerPanels } from './composer-panels.stylex';

export const ComposerQueue = memo(function ComposerQueue({ rows, session, rootId, runtimeId, activeTurn, connected, hasMore, refresh, loadMore }: {
  rows: readonly QueuedInputRow[]; session: Session; rootId: string; runtimeId: string;
  activeTurn?: string; connected: boolean; hasMore: boolean; refresh(): Promise<unknown>; loadMore(): Promise<unknown>;
}) {
  const runtime = useRuntime();
  const { commands } = useAppState();
  const [preview, setPreview] = useState<QueuedInputRow>();
  const [notice, setNotice] = useState('');
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<unknown>();
  const pending = useRef(new Set<string>());
  const strip = useRef<HTMLDivElement>(null);
  const list = useRef<HTMLOListElement>(null);
  const virtual = useVirtualizer({ count: rows.length, getScrollElement: () => list.current,
    estimateSize: () => 44, getItemKey: index => rows[index]!.id, overscan: 3,
    enabled: rows.length > 16, initialRect: { width: 400, height: 144 } });
  const visible = rows.length > 16 ? virtual.getVirtualItems() : rows.map((row, index) => ({ key: row.id, index, start: index * 44, end: (index + 1) * 44 }));
  const agentId = session.id;
  const owner = useRef<object>({});
  useEffect(() => { owner.current = {}; setPreview(undefined); return () => { owner.current = {}; }; }, [session, rootId, runtimeId]);
  const focusAfterRemoval = (element: HTMLElement | null, index: number) => {
    requestAnimationFrame(() => {
      if (document.activeElement !== document.body && document.activeElement !== element) return;
      const actions = strip.current?.querySelectorAll<HTMLButtonElement>('[data-queue-action]:not(:disabled)');
      const next = actions?.[Math.min(index, actions.length - 1)] ?? strip.current?.closest('form')?.querySelector<HTMLTextAreaElement>('[data-whip-composer]');
      next?.focus({ preventScroll: true });
    });
  };
  async function control(row: QueuedInputRow, remove: boolean) {
    if (!row.item || row.item.session_id !== session.id || row.stale || !connected || !remove && !activeTurn || pending.current.has(row.id)) return;
    pending.current.add(row.id);
    const origin = owner.current;
    const focused = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    const actionIndex = [...(strip.current?.querySelectorAll('[data-queue-action]') ?? [])].indexOf(focused!);
    setNotice('');
    setError(undefined);
    try {
      const signal = runtime.connections.signal(session.client);
      if (remove) {
        const result = await session.inputs.cancel(row.item.id, { signal });
        if (owner.current !== origin) return;
        setNotice(result.state === 'claimed' ? 'That message has already started.' : 'Queued message removed.');
      } else {
        const command = session.inputs.promotion(row.item.id, activeTurn!, crypto.randomUUID(), { journal: runtime.recovery });
        const result = await runtime.run(command, 'Steer queued message', undefined, `${runtimeId}:${rootId}:queue:${agentId}:${row.item.id}`);
        if (owner.current !== origin) return;
        setNotice(result.deleted ? 'The session was deleted.' : result.input?.steering?.consumed ? 'Message consumed by its target turn.' : 'Message will steer at the next available boundary.');
      }
      await refresh();
      if (owner.current === origin && remove && actionIndex >= 0) focusAfterRemoval(focused, actionIndex);
    } catch (failure) {
      if (owner.current !== origin) return;
      if (!remove && failure instanceof RemoteError && failure.kind === 'CONFLICT') {
        setNotice('The queue or target turn changed. Refresh before choosing another action.');
        await refresh().catch(setError);
      } else setError(failure);
    }
    finally { pending.current.delete(row.id); }
  }
  async function load() {
    setLoading(true); setError(undefined);
    try {
      await loadMore();
    } catch (failure) { setError(failure); }
    finally { setLoading(false); }
  }
  // Keep the status live region mounted, including after the final row leaves.
  return <div ref={strip} data-composer-queue {...stylex.props((!!rows.length || hasMore || !!error) && composerPanels.surface)}>
    {!!rows.length && <div role="region" aria-label="Queued messages">
      <ol ref={list} {...stylex.props(styles.list)}>
        {rows.length > 16 && <li aria-hidden {...stylex.props(styles.spacer(visible[0]?.start ?? 0))} />}
        {visible.map(cell => {
          const row = rows[cell.index]!;
          const key = row.item && `${runtimeId}:${rootId}:queue:${agentId}:${row.item.id}`;
          const command = commands.filter(item => item.draftKey === key).at(-1);
          const busy = !!command && (!!command.delivery || ['Submitting', 'queued', 'running', 'waiting'].includes(command.status));
          const files = BigInt(row.item?.attachment_count ?? row.preview?.attachment_count ?? row.preview?.attachments?.length ?? 0);
          const image = row.preview?.attachments?.find(file => file.media_type.startsWith('image/'));
          const label = row.text.trim() || (files === 1n ? '1 attachment' : files ? `${files} attachments` : 'Queued message');
          return <li key={row.id} data-index={cell.index} ref={rows.length > 16 ? virtual.measureElement : undefined}
            aria-posinset={cell.index + 1} aria-setsize={hasMore ? -1 : rows.length} data-queue-row={row.id} {...stylex.props(styles.item)}>
            <div {...stylex.props(styles.row)}>
              <ListEnd aria-hidden size={16} {...stylex.props(styles.icon)} />
              {image && <InputAttachment client={session.client} rootId={rootId} runtimeId={runtimeId} agentId={agentId} file={image}
                name="Attached image" image connected={connected} compact />}
              <Button type="button" variant="ghost" xstyle={styles.preview} aria-label={`Preview queued message: ${label}`} title={label} onClick={() => setPreview(row)}>
                <span {...stylex.props(styles.text)}>{label}</span>
                {!!files && !!row.text.trim() && <span {...stylex.props(styles.files)}><Paperclip aria-hidden size={12} />{files}</span>}
              </Button>
              {row.status !== 'Queued' && <span {...stylex.props(styles.status)}>{row.status}</span>}
              <Button type="button" variant="ghost" xstyle={styles.action} data-queue-action
                aria-label={`Steer queued message: ${label}`} title="Deliver to the current turn at its next available boundary"
                disabled={!connected || !activeTurn || !row.item || row.item.session_id !== session.id || row.stale || busy || !!row.item.steering}
                onClick={() => void control(row, false)}><CornerDownRight size={15} aria-hidden />Steer</Button>
              <Tooltip label="Remove queued message" disableHoverablePopup xstyle={styles.tooltip}>
                <Button type="button" variant="ghost" data-queue-action aria-label={`Remove queued message: ${label}`} xstyle={styles.remove}
                  disabled={!connected || !row.item || row.item.session_id !== session.id || row.stale || busy} onClick={() => void control(row, true)}><Trash2 size={15} aria-hidden /></Button>
              </Tooltip>
            </div>
            {command?.delivery && <div {...stylex.props(styles.recovery)}>
              <span>Checking this action’s delivery.</span>
              <Button type="button" variant="ghost" disabled={!connected} onClick={() => void runtime.checkCommand(command.id).catch(setError)}>Check status</Button>
              {command.delivery === 'absent' && <Button type="button" variant="ghost" disabled={!connected} onClick={() => void runtime.retryCommand(command.id).catch(setError)}>Retry original action</Button>}
            </div>}
          </li>;
        })}
        {rows.length > 16 && <li aria-hidden {...stylex.props(styles.spacer(Math.max(0, virtual.getTotalSize() - (visible.at(-1)?.end ?? 0))))} />}
      </ol>
    </div>}
    {hasMore && <Button type="button" variant="ghost" disabled={!connected || loading} onClick={() => void load()}>{loading ? 'Loading queue…' : 'Load more queued messages'}</Button>}
    <span role="status" aria-live="polite" {...stylex.props(styles.announcement)}>{notice || (rows.length ? `${rows.length} queued ${rows.length === 1 ? 'message' : 'messages'}${hasMore ? ' shown' : ''}` : '')}</span>
    <ErrorNotice type="action" owner={`${runtimeId}:${rootId}:${agentId}:queue`} error={error} />
    <Dialog open={!!preview} onOpenChange={open => { if (!open) setPreview(undefined); }} title="Queued message">
      {preview && <QueueMessagePreview key={preview.id} row={preview} session={session} rootId={rootId} runtimeId={runtimeId} connected={connected} />}
    </Dialog>
  </div>;
});

function QueueMessagePreview({ row, session, rootId, runtimeId, connected }: {
  row: QueuedInputRow; session: Session; rootId: string; runtimeId: string; connected: boolean;
}) {
  const query = useQuery({
    queryKey: ['queued-message', runtimeId, session.id, row.item?.id],
    enabled: connected && row.item?.session_id === session.id, gcTime: 0, retry: false, networkMode: 'always',
    queryFn: ({ signal }) => session.inputs.get(row.item!.id, { signal }),
  });
  const input = query.data;
  const text = input ? input.parts.flatMap(part => part.type === 'text' ? [part.text] : []).join('\n\n') : row.preview?.text ?? row.text;
  const references = input?.parts.flatMap(part => part.type === 'content' ? [part.reference_id] : []) ?? [];
  return <div {...stylex.props(layout.column)}>
    {query.isFetching && <Spinner label="Loading full queued message" />}
    {!connected && row.item && <p>Reconnect to load the full message.</p>}
    <ErrorNotice type="resource" owner={`${row.id}:body`} error={query.error}
      action={<Button type="button" disabled={!connected} onClick={() => void query.refetch()}>Retry</Button>} />
    <div {...stylex.props(styles.body)}>{text}</div>
    {!input && row.item?.preview_truncated && <p>Showing the available preview.</p>}
    <div {...stylex.props(styles.attachments)}>
      {input ? references.length > 0 && <MessageAttachments references={references} designContext={input.design_context} client={session.client} rootId={rootId} agentId={session.id} connected={connected}/>
        : <DesignInputAttachments files={row.preview?.attachments} designContext={row.preview?.design_context} client={session.client} rootId={rootId} runtimeId={runtimeId} agentId={session.id} connected={connected}/>}
    </div>
  </div>;
}


const styles = stylex.create({
  list: { listStyle: 'none', padding: 0, margin: 0, maxHeight: 'min(144px, 16dvh)', overflowY: 'auto', overscrollBehavior: 'contain', scrollbarGutter: 'stable' },
  item: { paddingInline: 8 },
  spacer: (height: number) => ({ height, padding: 0, margin: 0 }),
  row: { display: 'flex', alignItems: 'center', gap: 4, minWidth: 0, minHeight: 44, color: surface.secondaryText },
  icon: { flexShrink: 0, marginInline: 4 },
  preview: { display: 'flex', minWidth: 0, flex: 1, justifyContent: 'flex-start', gap: 6, paddingInline: 4, color: colors.foreground, fontSize: typography.size13, fontWeight: 400, backgroundColor: { default: 'transparent', ':hover': 'transparent' } },
  text: { overflow: 'hidden', whiteSpace: 'nowrap', textOverflow: 'ellipsis' },
  files: { display: 'inline-flex', alignItems: 'center', gap: 2, color: surface.secondaryText, fontSize: typography.size11 },
  action: { flexShrink: 0, gap: 4, paddingInline: 6, color: surface.secondaryText },
  remove: { flexShrink: 0, width: 32, paddingInline: 0, color: surface.secondaryText },
  tooltip: { pointerEvents: 'none' },
  status: { fontSize: typography.size11, maxWidth: { default: 140, [scale.phone]: 74 }, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' },
  recovery: { display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 4, fontSize: typography.size11, paddingInline: 8 },
  announcement: { position: 'absolute', width: 1, height: 1, overflow: 'hidden', clipPath: 'inset(50%)', whiteSpace: 'nowrap' },
  body: { whiteSpace: 'pre-wrap', overflowWrap: 'anywhere', maxHeight: '50dvh', overflowY: 'auto', margin: 0 },
  attachments: { display: 'flex', flexWrap: 'wrap', gap: 8 },
});
