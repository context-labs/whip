import { memo, useRef, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { useVirtualizer } from '@tanstack/react-virtual';
import type { Session } from '@whip/sdk';
import type { SessionView } from '@whip/sdk/state';
import type { SubmitPayload } from '@whip/protocol';
import { Button, Dialog, Spinner, Tooltip } from '@whip/ui';
import { CornerDownRight, ListEnd, Paperclip, Trash2 } from 'lucide-react';
import * as stylex from '@stylexjs/stylex';
import { colors, scale, surface, typography } from '@whip/ui/tokens.stylex';
import { useAppState, useRuntime } from './context';
import { admittedText, type QueuedInputRow } from './input-presentation';
import { ErrorNotice } from './error-feedback';
import { ImageAttachment } from './timeline';
import { InputAttachment } from './input-attachment';
import { DesignInputAttachments } from './design-input-attachments';
import { messagePresentation } from './conversation-rows';
import { layout } from './styles';

export const ComposerQueue = memo(function ComposerQueue({ rows, view, runtimeId, agentId, activeTurn, connected, hasMore }: {
  rows: readonly QueuedInputRow[]; view: SessionView; runtimeId: string; agentId: string;
  activeTurn?: string; connected: boolean; hasMore: boolean;
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
  const { session } = view;
  const focusAfterRemoval = (element: HTMLElement | null, index: number) => {
    requestAnimationFrame(() => {
      if (document.activeElement !== document.body && document.activeElement !== element) return;
      const actions = strip.current?.querySelectorAll<HTMLButtonElement>('[data-queue-action]:not(:disabled)');
      const next = actions?.[Math.min(index, actions.length - 1)] ?? strip.current?.closest('form')?.querySelector<HTMLTextAreaElement>('[data-whip-composer]');
      next?.focus({ preventScroll: true });
    });
  };
  async function control(row: QueuedInputRow, remove: boolean) {
    if (!row.item || row.stale || !connected || !remove && !activeTurn || pending.current.has(row.id)) return;
    pending.current.add(row.id);
    const focused = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    const actionIndex = [...(strip.current?.querySelectorAll('[data-queue-action]') ?? [])].indexOf(focused!);
    setNotice('');
    setError(undefined);
    try {
      const command = remove ? session.inbox.remove(agentId, row.item.seq) : session.inbox.steer(agentId, row.item.seq, activeTurn!);
      const result = await runtime.run(command, remove ? 'Remove queued message' : 'Steer queued message', undefined, `${runtimeId}:${session.rootId}:queue:${agentId}:${row.item.seq}`);
      const status = result.result?.status;
      setNotice(status === 'turn_ended' ? 'That turn has ended. The message keeps its place in the queue.'
        : status === 'already_started' ? 'That message has already started.'
        : remove ? 'Queued message removed.' : 'Message will steer at the next available boundary.');
      await view.refresh();
      if (remove && actionIndex >= 0) focusAfterRemoval(focused, actionIndex);
    } catch (failure) { setError(failure); }
    finally { pending.current.delete(row.id); }
  }
  async function load() {
    setLoading(true); setError(undefined);
    try {
      const state = view.getSnapshot();
      const page = state.collections.inbox;
      await view.loadCollection('inbox', { more: !!page?.has_more && page.revision === state.root?.collection_revision });
    } catch (failure) { setError(failure); }
    finally { setLoading(false); }
  }
  // Keep the status live region mounted, including after the final row leaves.
  return <div ref={strip} data-composer-queue>
    {!!rows.length && <div role="region" aria-label="Queued messages" {...stylex.props(styles.strip)}>
      <ol ref={list} {...stylex.props(styles.list)}>
        {rows.length > 16 && <li aria-hidden {...stylex.props(styles.spacer(visible[0]?.start ?? 0))} />}
        {visible.map(cell => {
          const row = rows[cell.index]!;
          const key = row.item && `${runtimeId}:${session.rootId}:queue:${agentId}:${row.item.seq}`;
          const command = commands.filter(item => item.draftKey === key).at(-1);
          const busy = !!command && (!!command.delivery || ['Submitting', 'queued', 'running', 'waiting'].includes(command.status));
          const files = row.preview?.attachment_count ?? row.preview?.attachments?.length ?? 0;
          const image = row.preview?.attachments?.find(file => file.kind === 'image');
          const label = row.text.trim() || (files === 1 ? '1 attachment' : files ? `${files} attachments` : 'Queued message');
          return <li key={row.id} data-index={cell.index} ref={rows.length > 16 ? virtual.measureElement : undefined}
            aria-posinset={cell.index + 1} aria-setsize={hasMore ? -1 : rows.length} data-queue-row={row.id} {...stylex.props(styles.item)}>
            <div {...stylex.props(styles.row)}>
              <ListEnd aria-hidden size={16} {...stylex.props(styles.icon)} />
              {image && <InputAttachment client={session.client} rootId={session.rootId} runtimeId={runtimeId} agentId={agentId} file={image.content}
                name={image.name || 'Attached image'} image connected={connected} compact />}
              <Button type="button" variant="ghost" xstyle={styles.preview} aria-label={`Preview queued message: ${label}`} title={label} onClick={() => setPreview(row)}>
                <span {...stylex.props(styles.text)}>{label}</span>
                {!!files && !!row.text.trim() && <span {...stylex.props(styles.files)}><Paperclip aria-hidden size={12} />{files}</span>}
              </Button>
              {row.status !== 'Queued' && <span {...stylex.props(styles.status)}>{row.status}</span>}
              <Button type="button" variant="ghost" xstyle={styles.action} data-queue-action
                aria-label={`Steer queued message: ${label}`} title="Deliver to the current turn at its next available boundary"
                disabled={!connected || !activeTurn || !row.item || row.stale || busy || !!row.item.steer_turn_id || row.item.kind.startsWith('steer')}
                onClick={() => void control(row, false)}><CornerDownRight size={15} aria-hidden />Steer</Button>
              <Tooltip label="Remove queued message" disableHoverablePopup xstyle={styles.tooltip}>
                <Button type="button" variant="ghost" data-queue-action aria-label={`Remove queued message: ${label}`} xstyle={styles.remove}
                  disabled={!connected || !row.item || row.stale || busy} onClick={() => void control(row, true)}><Trash2 size={15} aria-hidden /></Button>
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
    <ErrorNotice type="action" owner={`${runtimeId}:${session.rootId}:${agentId}:queue`} error={error} />
    <Dialog open={!!preview} onOpenChange={open => { if (!open) setPreview(undefined); }} title="Queued message">
      {preview && <QueueMessagePreview key={preview.id} row={preview} session={session} runtimeId={runtimeId} agentId={agentId} connected={connected} />}
    </Dialog>
  </div>;
});

function QueueMessagePreview({ row, session, runtimeId, agentId, connected }: {
  row: QueuedInputRow; session: Session; runtimeId: string; agentId: string; connected: boolean;
}) {
  const body = row.item?.payload;
  const query = useQuery({
    queryKey: ['queued-message', runtimeId, session.rootId, agentId, body?.reference_id, body?.digest],
    enabled: connected && !!body?.reference_id, gcTime: 0, retry: false, networkMode: 'always',
    queryFn: ({ signal }) => session.client.content(body!, { rootId: session.rootId, agentId }).readText({ maxBytes: 32 << 20, signal }),
  });
  let text = row.preview?.text ?? row.text;
  let attachments = row.preview?.attachments;
  let designContext = row.preview?.design_context;
  let images = messagePresentation(undefined).images;
  let parseError: unknown;
  const raw = query.data ?? (body && !body.reference_id ? body.text : undefined);
  if (raw != null) {
    if (row.item?.kind.endsWith('.parts')) {
      try {
        const input = JSON.parse(raw) as SubmitPayload;
        designContext = input.design_context;
        const parts = messagePresentation(input.parts);
        text = [input.text, parts.text].filter(Boolean).join('\n\n'); images = parts.images;
        attachments = input.attachments?.map(file => ({ ...file, content: { source: '', media_type: '', ...file.content } }));
      } catch (error) { parseError = error; }
    } else text = admittedText(row.item?.kind ?? 'submit', { ...body!, text: raw });
  }
  return <div {...stylex.props(layout.column)}>
    {query.isFetching && <Spinner label="Loading full queued message" />}
    {!connected && body?.reference_id && <p>Reconnect to load the full message.</p>}
    <ErrorNotice type="resource" owner={`${row.id}:body`} error={query.error || parseError}
      action={<Button type="button" disabled={!connected} onClick={() => void query.refetch()}>Retry</Button>} />
    <div {...stylex.props(styles.body)}>{text}</div>
    {!raw && row.preview?.truncated && <p>Showing the available preview.</p>}
    <div {...stylex.props(styles.attachments)}>
      <DesignInputAttachments files={attachments} designContext={designContext} client={session.client} rootId={session.rootId} runtimeId={runtimeId} agentId={agentId} connected={connected}/>
      {images.map((image, i) => <ImageAttachment key={i} image={image} thumbnail />)}
    </div>
  </div>;
}


const styles = stylex.create({
  strip: { borderWidth: 1, borderStyle: 'solid', borderColor: colors.border, borderBottomWidth: 0, borderTopLeftRadius: 20, borderTopRightRadius: 20, backgroundColor: colors.element, marginInline: 8, paddingTop: 4, paddingBottom: 12, marginBottom: -12 },
  list: { listStyle: 'none', padding: 0, margin: 0, maxHeight: 'min(144px, 24dvh)', overflowY: 'auto', overscrollBehavior: 'contain', scrollbarGutter: 'stable' },
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
