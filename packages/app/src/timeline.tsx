import {
  isValidElement,
  memo,
  useEffect,
  useMemo,
  useState,
  useSyncExternalStore,
  type ComponentPropsWithoutRef,
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
import { ActivityGroupRow } from './chat-activity';
import { isActivityGroup, responseCopies, type ConversationActivityRow } from './chat-activity-rows';
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
  const runtime = useRuntime();
  const disclosure = ['tool', 'reasoning', 'mailbox'].includes(row.role);
  const user = row.role === 'user';
  const assistant = row.role === 'assistant';
  const stamp = row.sentAt ? new Date(row.sentAt) : undefined;
  const time = stamp && Number.isFinite(stamp.getTime()) ? stamp : undefined;
  const copy = (
    <CopyButton
      label="Copy message"
      xstyle={styles.actionButton}
      text={row.text}
      copy={(text) => runtime.platform.copy(text)}
      onError={(error) => runtime.report(error)}
    />
  );
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

export function Timeline({
  rows,
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
}: {
  rows: ConversationActivityRow[];
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
}) {
  const runtime = useRuntime();
  const copies = useMemo(() => responseCopies(rows, active || !connected || !historyReady, hasMore), [rows, active, connected, historyReady, hasMore]);
  const [choices, setChoices] = useState<ReadonlyMap<string, boolean>>(new Map());
  useEffect(() => {
    const ids = new Set(rows.filter(isActivityGroup).map(row => row.id));
    setChoices(previous => [...previous.keys()].every(id => ids.has(id)) ? previous
      : new Map([...previous].filter(([id]) => ids.has(id))));
  }, [rows]);
  const toggle = (id: string, open: boolean) => setChoices(previous => {
    const next = new Map(previous);
    next.delete(id);
    next.set(id, !open);
    if (next.size > 128) next.delete(next.keys().next().value!);
    return next;
  });
  return <ReadingList rows={rows} hasMore={hasMore} loadOlder={loadOlder}
    bookmarkKey={bookmarkKey} historyRevision={historyRevision} historyReady={historyReady}
    canLoadOlder={canLoadOlder} loadingHistory={loadingHistory}
    label="Conversation" earlierLabel="Load earlier messages"
    renderRow={row => {
      const open = choices.get(row.id) ?? (density === 'detailed' && isActivityGroup(row) && row.cells.length > 0);
      const copy = copies.get(row.id);
      return <>
        {isActivityGroup(row) ? <ActivityGroupRow group={row} open={open} onToggle={() => toggle(row.id, open)} connected={connected}
          density={density} readBody={readBody} onOpenRepl={onOpenRepl} /> : <MessageRow row={row} readBody={readBody} historyAction={historyAction} />}
        {copy && <div data-response-actions {...stylex.props(styles.responseActions)}>
          <CopyButton label={copy.label} text={copy.text} xstyle={styles.actionButton}
            copy={text => runtime.platform.copy(text)} onError={error => runtime.report(error)} />
        </div>}
      </>;
    }} />;
}
