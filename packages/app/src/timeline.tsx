import { ErrorNotice } from './error-feedback';
import {
  isValidElement,
  memo,
  useCallback,
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
import { useQuery } from '@tanstack/react-query';
import type { WhipClient } from '@whip/sdk';
import type { DeepReadonly, HistoryView } from '@whip/sdk/state';
import type { RootSnapshot, StreamEvent } from '@whip/protocol';
import { Button, CodeBlock, CopyButton, Dialog, IconButton, Menu, Spinner } from '@whip/ui';
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
import { HistoryGapControl } from './history-gap';
import { ActivityHeader, ActivityStep, ActivityDetail, InlineAgent, type TranscriptAgent } from './transcript-activity';
import { MotionContext, RowMotion, transcriptMotion, useTranscriptMotion, type Arrival } from './transcript-motion';
import { MarkdownBlock, isMarkdownRow, markdownRows, useCoalescedTranscript } from './streaming-markdown';
import { isActivityGroup, isAgentActivity, activityItems, responseCopies, type ActivityItem, type ActivityGroup, type ConversationActivityRow } from './chat-activity-rows';
import { conversationRows, messagePresentation, type ImagePart, type TimelineRow } from './conversation-rows';
import { InputAttachment } from './input-attachment';
export { conversationRows, messagePresentation, timelineRows, type TimelineRow } from './conversation-rows';
import {
  type InboxInput,
  type SubmittedInput,
} from './input-presentation';

export function ImageAttachment({ image, thumbnail, label = 'Image attachment' }: { image: ImagePart; thumbnail?: boolean; label?: string }) {
  const [failed, setFailed] = useState<string>();
  const [loaded, setLoaded] = useState<string>();
  const [preview, setPreview] = useState<string>();
  const trigger = useRef<HTMLButtonElement>(null);
  const measureImage = useCallback((element: HTMLImageElement | null) => {
    // Cached images can finish before React attaches the load listener.
    if (element?.complete && element.naturalWidth > 0) setLoaded(image.url);
  }, [image.url]);
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
  if (thumbnail) return (
    <>
      <Button ref={trigger} aria-label={failed === image.url ? `${label} could not be loaded` : `Open ${label.toLowerCase()}`}
        aria-busy={loaded !== image.url && failed !== image.url || undefined}
        aria-haspopup="dialog" disabled={failed === image.url} xstyle={styles.thumbnail}
        onClick={() => setPreview(image.url)}>
        {failed === image.url ? <span role="status">Image unavailable</span> : <>
          <img ref={measureImage} src={image.url} alt="Attached image" loading="lazy" decoding="async"
            width={image.width} height={image.height}
            onLoad={() => setLoaded(image.url)} onError={() => setFailed(image.url)}
            {...stylex.props(styles.thumbnailImage, loaded !== image.url && styles.imageLoading)} />
          {loaded !== image.url && <Spinner label={`Loading ${label.toLowerCase()}`} size={14} />}
        </>}
      </Button>
      <Dialog open={preview === image.url} onOpenChange={open => setPreview(open ? image.url : undefined)}
        title={label} finalFocus={trigger} xstyle={styles.imageDialog}>
        <img src={image.url} alt="Image attachment preview" width={image.width} height={image.height}
          {...stylex.props(styles.imagePreview)} />
      </Dialog>
    </>
  );
  return (
    <div {...stylex.props(layout.column)}>
      {failed === image.url ? (
          <p role="status">This image could not be decoded.</p>
        ) : (
          <img
            src={image.url}
            alt="Attached image"
            loading="lazy"
            decoding="async"
            width={image.width}
            height={image.height}
            onError={() => setFailed(image.url)}
            {...stylex.props(styles.image)}
          />
        )}
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
  attachments: {
    display: 'flex',
    flexWrap: 'wrap',
    justifyContent: 'flex-end',
    gap: 8,
    maxWidth: { default: '80%', [scale.phone]: '94%' },
    marginBottom: 8,
  },
  thumbnail: {
    position: 'relative',
    flexShrink: 0,
    width: 80,
    height: 80,
    padding: 0,
    borderRadius: 10,
    borderColor: colors.border,
    overflow: 'hidden',
    backgroundColor: { default: 'transparent', ':hover': 'transparent' },
    color: surface.secondaryText,
    fontSize: typography.size12,
    whiteSpace: 'normal',
  },
  thumbnailImage: { position: 'absolute', inset: 0, width: '100%', height: '100%', objectFit: 'cover' },
  imageLoading: { visibility: 'hidden' },
  imageDialog: { width: 'min(960px, 90vw)' },
  imagePreview: { display: 'block', width: '100%', height: 'auto', maxHeight: '75vh', objectFit: 'contain' },
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
    display: 'block',
    maxWidth: '100%',
    width: 420,
    height: 'auto',
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

function MessageDetails({ row, readBody }: { row: TimelineRow; readBody(row: TimelineRow): void }) {
  return <>
    {row.args && <CodeBlock code={executionCode(row.args)} language={row.label === 'Starlark execution' ? 'starlark' : 'json'} />}
    {row.text && <pre {...stylex.props(layout.pre)}>{row.text}</pre>}
    {row.images?.map((image, index) => <ImageAttachment key={index} image={image} />)}
    {row.body && <Button variant="ghost" onClick={() => readBody(row)}>Read stored message · {row.body.size} bytes</Button>}
  </>;
}

function MessageDisclosure({ row, readBody, children }: { row: TimelineRow; readBody(row: TimelineRow): void; children?: ReactNode }) {
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
                  : row.role === 'internal' ? 'Activity details' : 'Reasoning')}
            </span>
            {row.live && <span>· in progress</span>}
            {!open && row.role === 'tool' && density === 'comfortable' && <span data-tool-preview {...stylex.props(styles.toolPreview)}>{(row.text || (row.args ? executionCode(row.args) : '')).slice(0, 512).split('\n').slice(0, 3).join('\n')}</span>}
          </summary>
          {open && <div {...stylex.props(styles.details)}>
            {children ?? <MessageDetails row={row} readBody={readBody} />}
          </div>}
        </details>
  );
}

export interface MessageScope {
  client: WhipClient;
  rootId: string;
  agentId: string;
}

/** A history-page byte limit is a transport detail, not a message disclosure.
 * Only mounted chat rows read bodies; tool/reasoning details still opt in. */
function StoredMessageRow({ row, scope, connected, historyRevision, onProse, detailsOnly, ...props }: {
  row: TimelineRow;
  scope: MessageScope;
  connected: boolean;
  historyRevision?: string;
  detailsOnly?: boolean;
  onProse(row: TimelineRow, text: string): void;
  readBody(row: TimelineRow): void;
  historyAction?(row: TimelineRow, action: 'fork' | 'rewind'): void;
}) {
  const runtime = useRuntime();
  const body = row.body!;
  const query = useQuery({
    queryKey: ['chat-message', scope.client.getSnapshot().info?.runtime_id, scope.rootId, scope.agentId, historyRevision, body.reference_id, body.digest],
    queryFn: async ({ signal }) => {
      const value = await scope.client.content(body, scope).readJSON({ maxBytes: 64 << 20, signal });
      if (!value || typeof value !== 'object' || !('content' in value)) throw new Error('Stored message has no content.');
      return messagePresentation(value.content);
    },
    enabled: connected,
    staleTime: Infinity,
    gcTime: 0,
    retry: false,
    refetchOnWindowFocus: false,
  }, runtime.queries);
  useEffect(() => {
    if (row.role === 'assistant' && query.data) onProse(row, query.data.text);
  }, [row, query.data, onProse]);
  if (query.data) {
    const loaded = { ...row, ...query.data, body: undefined };
    return detailsOnly ? <MessageDetails row={loaded} readBody={props.readBody} /> : <MessageRow {...props} row={loaded} />;
  }
  const content = query.error ? <ErrorNotice type="resource" owner={`${scope.rootId}:${scope.agentId}:${row.id}`} title="Could not load message" error={query.error}
      action={<Button variant="ghost" disabled={!connected} onClick={() => void query.refetch()}>Retry</Button>} />
    : <p role="status" {...stylex.props(layout.muted)}>{connected ? 'Loading message…' : 'Reconnect to load this message.'}</p>;
  return detailsOnly ? content : <MessageRow {...props} row={row} content={content} />;
}

export const MessageRow = memo(function MessageRow({
  row,
  readBody,
  historyAction,
  content,
  details,
  attachments,
}: {
  row: TimelineRow;
  readBody(row: TimelineRow): void;
  historyAction?(row: TimelineRow, action: 'fork' | 'rewind'): void;
  content?: ReactNode;
  details?: ReactNode;
  attachments?: ReactNode;
}) {
  const disclosure = ['tool', 'reasoning', 'mailbox', 'internal'].includes(row.role);
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
        <MessageDisclosure row={row} readBody={readBody}>{details}</MessageDisclosure>
      ) : user ? (
        <>
          {content == null && !row.body && (!!row.images?.length || attachments) && <div role="group" aria-label="Message attachments" {...stylex.props(styles.attachments)}>
            {attachments}
            {row.images?.map((image, index, images) => <ImageAttachment key={index} image={image} thumbnail
              label={`Image ${index + 1} of ${images.length}`} />)}
          </div>}
          {(content != null || row.body || row.text) && <div data-user-bubble {...stylex.props(styles.bubble)}>
            {content ?? (row.body ? (
              <Button variant="ghost" onClick={() => readBody(row)}>
                Read stored message · {row.body.size} bytes
              </Button>
            ) : (
              <Prose text={row.text} />
            ))}
          </div>}
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
          {content ?? (row.body ? (
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
          ))}
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
  loadGap,
  loadLatest,
  latestMissing,
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
  messageScope,
}: {
  rows: ConversationActivityRow[];
  agents?: readonly TranscriptAgent[];
  activeTurns?: Readonly<Record<string, string>>;
  activeTurnId?: string;
  onAgent?(id: string): void;
  hasMore: boolean;
  loadOlder(): Promise<void>;
  loadGap?(toSeq: number): Promise<void>;
  loadLatest?(): Promise<void>;
  latestMissing?: boolean;
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
  messageScope?: MessageScope;
}) {
  const rows = useCoalescedTranscript(incomingRows);
  const availableMotion = useTranscriptMotion() && connected;
  const [wasAvailable, setWasAvailable] = useState(availableMotion);
  useLayoutEffect(() => setWasAvailable(availableMotion), [availableMotion]);
  // Reattachment/visibility resumes with one final-state paint before allowing
  // new arrivals to animate. Work received while absent is already history.
  const motion = availableMotion && wasAvailable;
  // Keep only bounded copy text after a virtual message unmounts, never image
  // bodies. The SDK's history and activity identities remain authoritative.
  const [storedProse, setStoredProse] = useState<ReadonlyMap<string, { digest: string; text: string }>>(new Map());
  const retainProse = useCallback((row: TimelineRow, text: string) => {
    if (text.length > 256 * 1024) return;
    setStoredProse(previous => {
      if (previous.get(row.id)?.digest === row.body?.digest) return previous;
      const next = new Map(previous);
      next.set(row.id, { digest: row.body!.digest, text });
      let size = [...next.values()].reduce((sum, value) => sum + value.text.length, 0);
      for (const [id, value] of next) {
        if (size <= 256 * 1024 && next.size <= 512) break;
        next.delete(id); size -= value.text.length;
      }
      return next;
    });
  }, []);
  useEffect(() => {
    const bodies = new Map(rows.filter(row => row.body).map(row => [row.id, row.body!.digest]));
    setStoredProse(previous => {
      const retained = [...previous].filter(([id, value]) => bodies.get(id) === value.digest);
      return retained.length === previous.size ? previous : new Map(retained);
    });
  }, [rows]);
  const copies = useMemo(() => responseCopies(rows.map(row => {
    const prose = storedProse.get(row.id);
    return prose && prose.digest === row.body?.digest ? { ...row, body: undefined, copyText: prose.text } : row;
  }), active || !connected || !historyReady, hasMore), [rows, storedProse, active, connected, historyReady, hasMore]);
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
  const keys = new Set(blocks.flatMap(row => isActivityGroup(row) ? [row.id, ...activityItems(row).flatMap(item => [item.id, `${item.id}:detail`])] : [row.id]));
  // A prepend or newly known turn boundary can merge existing groups. Keep
  // their bounded aliases so the latest explicit group choice still wins.
  const groupChoices = new Map(groups.map(group => {
    const aliases = new Set([group.id, ...group.memberIds.filter(id => id.startsWith('activity:'))]);
    let choice: boolean | undefined;
    for (const [id, open] of choices) if (aliases.has(id)) choice = open;
    return [group.id, choice];
  }));
  const choiceKeys = new Set([...keys, ...groups.flatMap(group => group.memberIds.filter(id => id.startsWith('activity:')))]);
  for (const id of arrivals.current.keys()) if (!keys.has(id)) arrivals.current.delete(id);
  if (seen.current && active && connected && motion) {
    for (const row of blocks) {
      if (row.historyGap) continue;
      const items = isActivityGroup(row) ? activityItems(row) : [];
      let stagger = 0;
      for (const id of [...(row.seq === undefined ? [row.id] : []), ...items.filter(item => (item.row?.seq ?? item.cell?.seq) === undefined).map(item => item.id)]) {
        if (!seen.current.has(id) && !arrivals.current.has(id)) arrivals.current.set(id, { time: performance.now(), delay: items.length && id !== row.id ? (seen.current.has(row.id) ? 0 : transcriptMotion.first) + stagger++ * transcriptMotion.stagger : 0 });
      }
    }
  }
  useLayoutEffect(() => { seen.current = keys; });
  const wanted = new Set(groups.filter(group => groupChoices.get(group.id) ?? (density === 'detailed' || (!!group.autoOpen && active && (!!group.live || group.turnId === activeTurnId)))).map(group => group.id));
  for (const id of protectedGroups) if (visible.has(id) && groupChoices.get(id) === undefined) wanted.add(id);
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
    setChoices(previous => [...previous.keys()].every(id => choiceKeys.has(id)) ? previous : new Map([...previous].filter(([id]) => choiceKeys.has(id))));
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
  const toggle = (id: string, open: boolean) => {
    if (!open && motion) {
      const group = groups.find(group => group.id === id);
      for (const key of group ? activityItems(group).flatMap(item => [item.id, `${item.id}:detail`]) : [`${id}:detail`])
        arrivals.current.set(key, { time: performance.now(), delay: 0, disclosure: true });
    }
    setChoices(previous => {
    const next = new Map(previous); next.delete(id); next.set(id, !open);
    if (next.size > 128) next.delete(next.keys().next().value!);
    return next;
    });
  };
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
  return <MotionContext.Provider value={motion}><div ref={region} {...stylex.props(styles.transcript)}>
    <ReadingList rows={displayRows} hasMore={hasMore} loadOlder={loadOlder} loadLatest={loadLatest} latestMissing={latestMissing} chatFollow
      bookmarkKey={bookmarkKey} historyRevision={historyRevision} historyReady={historyReady}
      canLoadOlder={canLoadOlder} loadingHistory={loadingHistory}
      label="Conversation" earlierLabel="Load earlier messages" footer={footer}
      renderRow={row => {
        const source = row.source;
        return <>
          <RowMotion arrival={row.group || isAgentActivity(source) ? arrivals.current.get(row.id) : undefined} closing={!!row.item && closing.has(row.group!.id)}>
            {source.historyGap ? <HistoryGapControl gap={source.historyGap} connected={connected} load={() => loadGap?.(source.historyGap!.toSeq) ?? Promise.resolve()} />
            : row.group ? row.item ? row.detail
              ? <ActivityDetail item={row.item} groupId={row.group.id} readBody={readBody} onOpenRepl={onOpenRepl} />
              : <ActivityStep item={row.item} groupId={row.group.id} open={!!row.open} toggle={() => toggle(row.item!.id, !!row.open)} connected={connected} last={!!row.last} />
              : <ActivityHeader group={row.group} open={!!row.open} toggle={() => toggle(row.group!.id, !!row.open)} connected={connected} density={density} />
            : isAgentActivity(source) ? <InlineAgent row={source} agent={agents.find(agent => agent.id === source.agentHost.display?.child_id)} active={!!activeTurns[source.agentHost.display?.child_id ?? '']} connected={connected} onAgent={onAgent} readBody={readBody} onOpenRepl={onOpenRepl} />
            : isMarkdownRow(source) ? <article data-message-role="assistant" data-message-id={source.ownerId} {...stylex.props(messageMarker, styles.article)}><MarkdownBlock row={source} components={markdownComponents} arrival={arrivals.current.get(row.id)} /></article>
            : source.body && messageScope && (source.role === 'user' || source.role === 'assistant')
              ? <StoredMessageRow row={source} scope={messageScope} connected={connected} historyRevision={historyRevision} onProse={retainProse} readBody={readBody} historyAction={historyAction} />
              : <MessageRow row={source} readBody={readBody} historyAction={historyAction}
                  attachments={messageScope && source.inputAttachments?.map((file, index) => <InputAttachment key={`${file.content.reference_id}:${index}`}
                    client={messageScope.client} rootId={messageScope.rootId} agentId={messageScope.agentId}
                    runtimeId={messageScope.client.getSnapshot().info?.runtime_id ?? ''} connected={connected}
                    file={file.content} image={file.kind === 'image'} name={file.name || `Attachment ${index + 1}`} />)}
                  details={source.role === 'internal' && source.body && messageScope
                    ? <StoredMessageRow row={source} scope={messageScope} connected={connected} historyRevision={historyRevision} onProse={retainProse} readBody={readBody} detailsOnly />
                    : undefined} />}
          </RowMotion>
          {row.copy && <div data-response-actions {...stylex.props(styles.responseActions)}><MessageCopy key={source.id} owner={source.id} label={row.copy.label} text={row.copy.text} /></div>}
        </>;
      }} />
  </div></MotionContext.Provider>;
}
