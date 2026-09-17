import { ErrorNotice } from './error-feedback';
import {
  isValidElement,
  memo,
  useEffect,
  useLayoutEffect,
  useRef,
  useMemo,
  useState,
  useSyncExternalStore,
  type ComponentPropsWithoutRef,
  type ReactNode,
} from 'react';
import { Markdown } from '@tanstack/markdown/react';
import { streamingMarkdownExtension } from '@tanstack/markdown/extensions/streaming';
import type { DeepReadonly, HistoryView } from '@whip/sdk/state';
import type { RootSnapshot, StreamEvent } from '@whip/protocol';
import { Button, CodeBlock, CopyButton, IconButton, Menu } from '@whip/ui';
import {
  ChevronRight,
  Code2,
  MoreHorizontal,
} from 'lucide-react';
import * as stylex from '@stylexjs/stylex';
import { messageMarker } from './timeline.stylex';
import {
  colors,
  typography,
  markdown,
  surface,
  scale,
  appearance,
} from '@whip/ui/tokens.stylex';
import { useRuntime } from './context';
import { layout } from './styles';
import { ReadingList } from './reading-list';
import { ActivityHeader, ActivityStep, ActivityDetail, InlineAgent, type TranscriptAgent } from './transcript-activity';
import { MotionContext, RowMotion, transcriptMotion, useTranscriptMotion, type Arrival } from './transcript-motion';
import { MarkdownBlock, isMarkdownRow, markdownRows, useCoalescedTranscript } from './streaming-markdown';
import { isActivityGroup, isAgentActivity, activityItems, responseCopies, type ActivityItem, type ActivityGroup, type ConversationActivityRow } from './chat-activity-rows';
import { conversationRows, messagePresentation, type ImagePart, type TimelineRow } from './conversation-rows';
export { conversationRows, messagePresentation, timelineRows, type TimelineRow } from './conversation-rows';
import {
  type InboxInput,
  type SubmittedInput,
} from './input-presentation';

export function ImageAttachment({ image }: { image: ImagePart }) {
  const [visible, setVisible] = useState(false);
  const [failed, setFailed] = useState(false);
  const embedded = /^data:image\/(?:png|jpeg|gif|webp);base64,/.test(image.url);
  const remote = /^https?:\/\//.test(image.url);
  if (!embedded)
    return remote ? (
      <a
        href={image.url}
        target="_blank"
        rel="noopener noreferrer"
        {...stylex.props(styles.link)}
      >
        Open external image attachment
      </a>
    ) : (
      <p>Image preview is unavailable for this format.</p>
    );
  if (image.width && image.height && image.width * image.height > 16_000_000)
    return (
      <p>
        Image is too large to preview here. Inspect the original attachment.
      </p>
    );
  return (
    <div {...stylex.props(layout.column)}>
      <Button variant="ghost" onClick={() => setVisible((value) => !value)}>
        {visible ? 'Hide image attachment' : 'View image attachment'}
      </Button>
      {visible &&
        (failed ? (
          <p role="status">This image could not be decoded.</p>
        ) : (
          <img
            src={image.url}
            alt="Attached image"
            loading="lazy"
            decoding="async"
            onError={() => setFailed(true)}
            {...stylex.props(styles.image)}
          />
        ))}
    </div>
  );
}
export function executionCode(args: string): string {
  try {
    const value = JSON.parse(args);
    return typeof value.code === 'string'
      ? value.code
      : JSON.stringify(value, null, 2);
  } catch {
    return args;
  }
}

const styles = stylex.create({
  transcript: { display: 'flex', flexDirection: 'column', flex: 1, minHeight: 0 },
  article: {
    position: 'relative',
    paddingBlock: 8,
    overflowWrap: 'anywhere',
    fontSize: typography.size14,
    lineHeight: 1.65,
    color: colors.foreground,
  },
  authored: { paddingBottom: { default: 28, '@media (pointer: coarse)': 48 } },
  user: {
    display: 'flex',
    flexDirection: 'column',
    alignItems: 'flex-end',
  },
  bubble: {
    maxWidth: { default: '80%', [scale.phone]: '94%' },
    minWidth: 0,
    paddingBlock: 12,
    paddingInline: 16,
    borderRadius: 20,
    backgroundColor: colors.element,
    color: colors.foreground,
  },
  actions: {
    position: 'absolute',
    bottom: 0,
    right: 0,
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'flex-end',
    gap: 8,
    minHeight: { default: 28, '@media (pointer: coarse)': 44 },
    pointerEvents: { default: 'none', [stylex.when.ancestor(':hover', messageMarker)]: 'auto', [stylex.when.ancestor(':focus-within', messageMarker)]: 'auto', '@media (pointer: coarse)': 'auto' },
    fontSize: typography.size12,
    color: surface.secondaryText,
    opacity: {
      default: 0,
      [stylex.when.ancestor(':hover', messageMarker)]: 1,
      [stylex.when.ancestor(':focus-within', messageMarker)]: 1,
      [stylex.when.ancestor(':has([aria-expanded="true"])', messageMarker)]: 1,
      '@media (pointer: coarse)': 1,
    },
  },
  responseActions: { display: 'flex', alignItems: 'center', paddingBlock: '2px 12px', color: surface.secondaryText },
  actionButton: { width: { default: 28, '@media (pointer: coarse)': 44 }, minHeight: { default: 28, '@media (pointer: coarse)': 44 }, padding: 0 },
  delivery: { fontSize: typography.size12, color: surface.secondaryText, marginTop: 4 },
  byline: {
    fontSize: typography.size12,
    fontWeight: 550,
    marginBottom: 8,
    color: surface.secondaryText,
    display: 'flex',
    alignItems: 'center',
    gap: 8,
  },
  disclosure: {
    fontSize: typography.size12,
    lineHeight: 1.65,
    color: surface.secondaryText,
    paddingBlock: 6,
  },
  toolPreview: { flexBasis: '100%', fontFamily: typography.mono, fontSize: typography.codeSize, whiteSpace: 'pre-wrap', overflowWrap: 'anywhere', maxHeight: '5em', overflow: 'hidden', paddingLeft: 21 },
  summary: {
    flexWrap: 'wrap',
    display: 'flex',
    alignItems: 'center',
    gap: 8,
    cursor: 'pointer',
    listStyle: 'none',
    minHeight: 24,
    borderRadius: 6,
  },
  image: {
    maxWidth: '100%',
    width: 420,
    maxHeight: 420,
    objectFit: 'contain',
    borderRadius: 8,
  },
  details: { marginTop: 12, maxHeight: 480, overflow: 'auto' },
  p: { marginTop: 0, marginBottom: { default: 12, ':last-child': 0 } },
  heading: {
    fontSize: typography.size18,
    fontWeight: 560,
    color: markdown.heading,
    marginBlock: '18px 10px',
  },
  code: {
    fontFamily: typography.mono,
    fontSize: '0.85em',
    color: markdown.code,
    backgroundColor: surface.inlineCode,
    paddingInline: 4,
    borderRadius: 4,
  },
  codeBlock: {
    fontFamily: typography.mono,
    fontSize: typography.codeSize,
    padding: 16,
    backgroundColor: colors.element,
    overflow: 'auto',
    borderRadius: 8,
    lineHeight: 1.65,
    whiteSpace: appearance.codeWhiteSpace,
    overflowWrap: appearance.codeOverflowWrap,
    maxHeight: 560,
  },
  link: {
    color: colors.link,
    textDecoration: 'underline',
    textUnderlineOffset: 3,
  },
  quote: {
    color: markdown.quote,
    marginInline: 0,
    paddingLeft: 16,
    borderLeftWidth: 2,
    borderLeftStyle: 'solid',
    borderLeftColor: colors.border,
  },
  strong: { color: markdown.strong, fontWeight: 600 },
  list: { paddingLeft: 24, marginTop: 0, marginBottom: { default: 12, ':last-child': 0 } },
  table: {
    display: 'block',
    overflowX: 'auto',
    maxWidth: '100%',
    borderCollapse: 'collapse',
    fontSize: typography.size13,
    marginBlock: 12,
  },
  cell: {
    padding: 8,
    textAlign: 'left',
    borderBottomWidth: 1,
    borderBottomStyle: 'solid',
    borderBottomColor: colors.border,
  },

});

const markdownComponents = {
  p: (props: ComponentPropsWithoutRef<'p'>) => (
    <p {...props} {...stylex.props(styles.p)} />
  ),
  h1: (props: ComponentPropsWithoutRef<'h1'>) => (
    <h2 {...props} {...stylex.props(styles.heading)} />
  ),
  h2: (props: ComponentPropsWithoutRef<'h2'>) => (
    <h2 {...props} {...stylex.props(styles.heading)} />
  ),
  h3: (props: ComponentPropsWithoutRef<'h3'>) => (
    <h3 {...props} {...stylex.props(styles.heading)} />
  ),
  code: (props: ComponentPropsWithoutRef<'code'>) => (
    <code {...props} {...stylex.props(styles.code)} />
  ),
  pre: ({ children, ...props }: ComponentPropsWithoutRef<'pre'>) => {
    if (
      isValidElement<{ children?: unknown; className?: string }>(children) &&
      typeof children.props.children === 'string'
    )
      return (
        <CodeBlock
          code={children.props.children}
          language={children.props.className?.replace(/^language-/, '')}
        />
      );
    return (
      <pre {...props} {...stylex.props(styles.codeBlock)}>
        {children}
      </pre>
    );
  },
  a: (props: ComponentPropsWithoutRef<'a'>) => (
    <a
      {...props}
      target="_blank"
      rel="noopener noreferrer"
      {...stylex.props(styles.link)}
    />
  ),
  img: ({ alt, src }: ComponentPropsWithoutRef<'img'>) => (
    <a
      href={typeof src === 'string' ? src : undefined}
      target="_blank"
      rel="noopener noreferrer"
      {...stylex.props(styles.link)}
    >
      Image: {alt || 'Open image'}
    </a>
  ),
  blockquote: (props: ComponentPropsWithoutRef<'blockquote'>) => (
    <blockquote {...props} {...stylex.props(styles.quote)} />
  ),
  strong: (props: ComponentPropsWithoutRef<'strong'>) => (
    <strong {...props} {...stylex.props(styles.strong)} />
  ),
  ul: (props: ComponentPropsWithoutRef<'ul'>) => (
    <ul {...props} {...stylex.props(styles.list)} />
  ),
  ol: (props: ComponentPropsWithoutRef<'ol'>) => (
    <ol {...props} {...stylex.props(styles.list)} />
  ),
  table: (props: ComponentPropsWithoutRef<'table'>) => (
    <table {...props} {...stylex.props(styles.table)} />
  ),
  td: (props: ComponentPropsWithoutRef<'td'>) => (
    <td {...props} {...stylex.props(styles.cell)} />
  ),
  th: (props: ComponentPropsWithoutRef<'th'>) => (
    <th {...props} {...stylex.props(styles.cell)} />
  ),
};
const streaming = [streamingMarkdownExtension()];
export const Prose = memo(function Prose({
  text,
  live = false,
}: {
  text: string;
  live?: boolean;
}) {
  return (
    <div data-message-prose><Markdown
      components={markdownComponents}
      extensions={live ? streaming : undefined}
    >
      {text}
    </Markdown></div>
  );
});

function MessageDisclosure({ row, readBody }: { row: TimelineRow; readBody(row: TimelineRow): void }) {
  const runtime = useRuntime();
  const density = useSyncExternalStore(runtime.subscribe, () => runtime.getSnapshot().preferences.toolDensity);
  const [disclosed, setDisclosed] = useState<{ id: string; open: boolean }>();
  const open = disclosed?.id === row.id ? disclosed.open : row.role === 'tool' && density === 'detailed';
  return (
        <details open={open} {...stylex.props(styles.disclosure)}>
          <summary onClick={event => { event.preventDefault(); setDisclosed({ id: row.id, open: !open }); }} {...stylex.props(styles.summary)}>
            <ChevronRight size={13} />
            <Code2 size={13} />
            <span>
              {row.label ||
                (row.role === 'mailbox'
                  ? `Mailbox update${row.deliveries ? ` · ${row.deliveries} deliveries` : ''}`
                  : 'Reasoning')}
            </span>
            {row.live && <span>· in progress</span>}
            {!open && row.role === 'tool' && density === 'comfortable' && <span data-tool-preview {...stylex.props(styles.toolPreview)}>{(row.text || (row.args ? executionCode(row.args) : '')).slice(0, 512).split('\n').slice(0, 3).join('\n')}</span>}
          </summary>
          {open && <div {...stylex.props(styles.details)}>
            {row.args && (
              <CodeBlock
                code={executionCode(row.args)}
                language={
                  row.label === 'Starlark execution' ? 'starlark' : 'json'
                }
              />
            )}
            {row.text && <pre {...stylex.props(layout.pre)}>{row.text}</pre>}
            {row.images?.map((image, index) => (
              <ImageAttachment key={index} image={image} />
            ))}
            {row.body && (
              <Button variant="ghost" onClick={() => readBody(row)}>
                Read stored message · {row.body.size} bytes
              </Button>
            )}
          </div>}
        </details>
  );
}

export const MessageRow = memo(function MessageRow({
  row,
  readBody,
  historyAction,
}: {
  row: TimelineRow;
  readBody(row: TimelineRow): void;
  historyAction?(row: TimelineRow, action: 'fork' | 'rewind'): void;
}) {
  const disclosure = ['tool', 'reasoning', 'mailbox'].includes(row.role);
  const user = row.role === 'user';
  const assistant = row.role === 'assistant';
  const stamp = row.sentAt ? new Date(row.sentAt) : undefined;
  const time = stamp && Number.isFinite(stamp.getTime()) ? stamp : undefined;
  const copy = <MessageCopy key={row.id} owner={row.id} label="Copy message" text={row.text} />;
  return (
    <article
      data-message-id={row.id}
      data-message-role={row.role}
      aria-label={user ? 'Your message' : undefined}
      {...stylex.props(messageMarker, styles.article, user && styles.authored, user && styles.user)}
    >
      {disclosure ? (
        <MessageDisclosure row={row} readBody={readBody} />
      ) : user ? (
        <>
          <div data-user-bubble {...stylex.props(styles.bubble)}>
            {row.body ? (
              <Button variant="ghost" onClick={() => readBody(row)}>
                Read stored message · {row.body.size} bytes
              </Button>
            ) : (
              <>
                <Prose text={row.text} />
                {row.images?.map((image, index) => (
                  <ImageAttachment key={index} image={image} />
                ))}
              </>
            )}
          </div>
          {row.delivery && (
            <div role="status" {...stylex.props(styles.delivery)}>
              {row.delivery}
            </div>
          )}
          <div data-message-actions {...stylex.props(styles.actions)}>
            {time && (
              <time dateTime={row.sentAt} title={time.toLocaleString()}>
                {time.toLocaleTimeString(undefined, {
                  hour: 'numeric',
                  minute: '2-digit',
                })}
              </time>
            )}
            {copy}
            {row.seq !== undefined && historyAction && (
              <Menu
                trigger={
                  <IconButton variant="ghost" xstyle={styles.actionButton} label="Message history actions">
                    <MoreHorizontal size={14} />
                  </IconButton>
                }
                items={[
                  {
                    id: 'fork',
                    label: 'Fork before this message',
                    onSelect: () => historyAction(row, 'fork'),
                  },
                  {
                    id: 'rewind',
                    label: 'Rewind before this message…',
                    onSelect: () => historyAction(row, 'rewind'),
                  },
                ]}
              />
            )}
          </div>
        </>
      ) : (
        <>
          {!assistant && (
            <div {...stylex.props(styles.byline)}>
              <span>Activity</span>
              <span {...stylex.props(layout.grow)} />
              {copy}
            </div>
          )}
          {row.body ? (
            <Button variant="ghost" onClick={() => readBody(row)}>
              Read stored message · {row.body.size} bytes
            </Button>
          ) : (
            <>
              <Prose text={row.text} live={row.live} />
              {row.images?.map((image, index) => (
                <ImageAttachment key={index} image={image} />
              ))}
            </>
          )}
        </>
      )}
    </article>
  );
});

function MessageCopy({ owner, label, text }: { owner: string; label: string; text: string }) {
  const runtime = useRuntime();
  const [error, setError] = useState<unknown>();
  return <div>
    <CopyButton label={label} text={text} xstyle={styles.actionButton}
      copy={async value => { setError(undefined); await runtime.platform.copy(value); }} onError={setError} />
    {error !== undefined && <ErrorNotice type="action" owner={`copy:${owner}`} error={error} title="Could not copy message" onDismiss={() => setError(undefined)} />}
  </div>;
}

export function Timeline({
  rows: incomingRows,
  agents = [],
  activeTurns = {},
  activeTurnId,
  onAgent,
  hasMore,
  loadOlder,
  readBody,
  historyAction,
  bookmarkKey,
  historyRevision,
  historyReady = true,
  canLoadOlder = true,
  loadingHistory = false,
  connected = true,
  onOpenRepl,
  density = 'compact',
  active = false,
  footer,
}: {
  rows: ConversationActivityRow[];
  agents?: readonly TranscriptAgent[];
  activeTurns?: Readonly<Record<string, string>>;
  activeTurnId?: string;
  onAgent?(id: string): void;
  hasMore: boolean;
  loadOlder(): Promise<void>;
  readBody(row: TimelineRow): void;
  historyAction?(row: TimelineRow, action: 'fork' | 'rewind'): void;
  bookmarkKey?: string;
  historyRevision?: string;
  historyReady?: boolean;
  canLoadOlder?: boolean;
  loadingHistory?: boolean;
  connected?: boolean;
  onOpenRepl?(): void;
  density?: 'compact' | 'comfortable' | 'detailed';
  active?: boolean;
  footer?: ReactNode;
}) {
  const rows = useCoalescedTranscript(incomingRows);
  const motion = useTranscriptMotion();
  const copies = useMemo(() => responseCopies(rows, active || !connected || !historyReady, hasMore), [rows, active, connected, historyReady, hasMore]);
  const parsed = useRef<Parameters<typeof markdownRows>[1]>(new Map());
  const blocks = useMemo(() => markdownRows(rows, parsed.current), [rows]);
  const region = useRef<HTMLDivElement>(null);
  const [choices, setChoices] = useState<ReadonlyMap<string, boolean>>(new Map());
  const [protectedGroups, setProtectedGroups] = useState<ReadonlySet<string>>(new Set());
  const [visible, setVisible] = useState<ReadonlySet<string>>(new Set());
  const [closing, setClosing] = useState<ReadonlySet<string>>(new Set());
  const timers = useRef(new Map<string, ReturnType<typeof setTimeout>>());
  const seen = useRef<Set<string> | undefined>(undefined);
  const arrivals = useRef(new Map<string, Arrival>());
  const groups = rows.filter(isActivityGroup);
  const keys = new Set(blocks.flatMap(row => isActivityGroup(row) ? [row.id, ...activityItems(row).map(item => item.id)] : [row.id]));
  for (const id of arrivals.current.keys()) if (!keys.has(id)) arrivals.current.delete(id);
  if (seen.current && active && connected && motion) {
    for (const row of blocks) {
      const items = isActivityGroup(row) ? activityItems(row) : [];
      let stagger = 0;
      for (const id of [row.id, ...items.map(item => item.id)]) {
        if (!seen.current.has(id) && !arrivals.current.has(id)) arrivals.current.set(id, { time: performance.now(), delay: items.length && id !== row.id ? (seen.current.has(row.id) ? 0 : transcriptMotion.first) + stagger++ * transcriptMotion.stagger : 0 });
      }
    }
  }
  useLayoutEffect(() => { seen.current = keys; });
  const wanted = new Set(groups.filter(group => choices.get(group.id) ?? (density === 'detailed' || (!!group.autoOpen && active && (!!group.live || group.turnId === activeTurnId)))).map(group => group.id));
  for (const id of protectedGroups) if (!choices.has(id)) wanted.add(id);
  useEffect(() => {
    const update = () => {
      const selection = document.getSelection();
      const nodes = [document.activeElement, ...(!selection?.isCollapsed ? [selection?.anchorNode, selection?.focusNode] : [])];
      const ids = nodes.flatMap(node => {
        const element = node instanceof Element ? node : node?.parentElement;
        const owner = element?.closest<HTMLElement>('[data-activity-owner]');
        return owner && region.current?.contains(owner) ? [owner.dataset.activityOwner!] : [];
      });
      if (selection && !selection.isCollapsed && selection.rangeCount) {
        const range = selection.getRangeAt(0);
        for (const owner of region.current?.querySelectorAll<HTMLElement>('[data-activity-owner]') ?? [])
          if (range.intersectsNode(owner)) ids.push(owner.dataset.activityOwner!);
      }
      setProtectedGroups(previous => [...previous].join() === ids.join() ? previous : new Set(ids));
    };
    document.addEventListener('selectionchange', update); document.addEventListener('focusin', update); document.addEventListener('focusout', update);
    return () => { document.removeEventListener('selectionchange', update); document.removeEventListener('focusin', update); document.removeEventListener('focusout', update); };
  }, []);
  useLayoutEffect(() => {
    const ids = new Set(groups.map(group => group.id));
    setChoices(previous => [...previous.keys()].every(id => keys.has(id)) ? previous : new Map([...previous].filter(([id]) => keys.has(id))));
    setVisible(previous => {
      const next = new Set([...previous].filter(id => ids.has(id)));
      for (const id of wanted) next.add(id);
      return [...next].join() === [...previous].join() ? previous : next;
    });
    for (const [id, timer] of timers.current) if (wanted.has(id) || !ids.has(id) || !motion) { clearTimeout(timer); timers.current.delete(id); setClosing(previous => new Set([...previous].filter(value => value !== id))); }
    for (const group of groups) {
      if (!visible.has(group.id) || wanted.has(group.id) || timers.current.has(group.id)) continue;
      const finish = () => {
        timers.current.delete(group.id);
        setVisible(previous => new Set([...previous].filter(id => id !== group.id)));
        setClosing(previous => new Set([...previous].filter(id => id !== group.id)));
      };
      if (!motion) { finish(); continue; }
      const ends = activityItems(group).map(item => arrivals.current.get(item.id)).filter((value): value is Arrival => !!value).map(value => value.time + value.delay + transcriptMotion.connector);
      const hold = Math.max(0, ...ends.map(end => end - performance.now()));
      timers.current.set(group.id, setTimeout(() => {
        setClosing(previous => new Set([...previous, group.id]));
        timers.current.set(group.id, setTimeout(finish, transcriptMotion.fold));
      }, hold));
    }
  });
  useEffect(() => () => { for (const timer of timers.current.values()) clearTimeout(timer); timers.current.clear(); }, []);
  const toggle = (id: string, open: boolean) => setChoices(previous => {
    const next = new Map(previous); next.delete(id); next.set(id, !open);
    if (next.size > 128) next.delete(next.keys().next().value!);
    return next;
  });
  type DisplayRow = { id: string; seq?: number; memberIds?: readonly string[]; memberSeqs?: readonly number[]; source: typeof blocks[number]; group?: ActivityGroup; item?: ActivityItem; detail?: boolean; open?: boolean; last?: boolean; copy?: { text: string; label: string } };
  const displayRows: DisplayRow[] = [];
  for (const [blockIndex, row] of blocks.entries()) {
    if (isActivityGroup(row)) {
      const open = wanted.has(row.id) || visible.has(row.id);
      displayRows.push({ id: row.id, seq: row.seq, memberIds: row.memberIds, memberSeqs: row.memberSeqs, source: row, group: row, open: open && !closing.has(row.id) });
      if (open) activityItems(row).forEach((original, index, items) => {
        const reasoningLive = original.kind === 'reasoning' && index === items.length - 1 && !!row.autoOpen && active && !!original.row?.live;
        const item = original.kind === 'reasoning' && original.row ? { ...original, row: { ...original.row, live: reasoningLive } } : original;
        const detail = choices.get(item.id) ?? (item.kind === 'mailbox' ? false : item.kind === 'reasoning' ? reasoningLive : density === 'detailed');
        displayRows.push({ id: item.id, seq: item.row?.seq ?? item.cell?.seq, source: row, group: row, item, open: detail, last: index === items.length - 1 && !detail });
        if (detail) displayRows.push({ id: `${item.id}:detail`, seq: item.row?.seq ?? item.cell?.seq, source: row, group: row, item, detail: true });
      });
    } else displayRows.push({ id: row.id, seq: row.seq, memberIds: row.memberIds, source: row });
    const owner = isMarkdownRow(row) ? row.ownerId : row.id;
    const following = blocks[blockIndex + 1];
    if (!isMarkdownRow(row) || !following || !isMarkdownRow(following) || following.ownerId !== owner) displayRows.at(-1)!.copy = copies.get(owner);
  }
  return <MotionContext.Provider value={motion && connected}><div ref={region} {...stylex.props(styles.transcript)}>
    <ReadingList rows={displayRows} hasMore={hasMore} loadOlder={loadOlder} smoothFollow
      bookmarkKey={bookmarkKey} historyRevision={historyRevision} historyReady={historyReady}
      canLoadOlder={canLoadOlder} loadingHistory={loadingHistory}
      label="Conversation" earlierLabel="Load earlier messages" footer={footer}
      renderRow={row => {
        const source = row.source;
        return <>
          <RowMotion arrival={row.group || isAgentActivity(source) ? arrivals.current.get(row.id) : undefined} closing={!!row.item && closing.has(row.group!.id)}>
            {row.group ? row.item ? row.detail
              ? <ActivityDetail item={row.item} groupId={row.group.id} readBody={readBody} onOpenRepl={onOpenRepl} />
              : <ActivityStep item={row.item} groupId={row.group.id} open={!!row.open} toggle={() => toggle(row.item!.id, !!row.open)} connected={connected} last={!!row.last} />
              : <ActivityHeader group={row.group} open={!!row.open} toggle={() => toggle(row.group!.id, !!row.open)} connected={connected} density={density} />
            : isAgentActivity(source) ? <InlineAgent row={source} agent={agents.find(agent => agent.id === source.agentHost.display?.child_id)} active={!!activeTurns[source.agentHost.display?.child_id ?? '']} connected={connected} onAgent={onAgent} readBody={readBody} onOpenRepl={onOpenRepl} />
            : isMarkdownRow(source) ? <article data-message-role="assistant" data-message-id={source.ownerId} {...stylex.props(messageMarker, styles.article)}><MarkdownBlock row={source} components={markdownComponents} arrival={arrivals.current.get(row.id)} /></article>
            : <MessageRow row={source} readBody={readBody} historyAction={historyAction} />}
          </RowMotion>
          {row.copy && <div data-response-actions {...stylex.props(styles.responseActions)}><MessageCopy key={source.id} owner={source.id} label={row.copy.label} text={row.copy.text} /></div>}
        </>;
      }} />
  </div></MotionContext.Provider>;
}
