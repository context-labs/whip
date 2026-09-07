import {
  isValidElement,
  memo,
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  type ComponentPropsWithoutRef,
} from 'react';
import { Markdown } from '@tanstack/markdown/react';
import { streamingMarkdownExtension } from '@tanstack/markdown/extensions/streaming';
import { defaultRangeExtractor, useVirtualizer } from '@tanstack/react-virtual';
import type { DeepReadonly, HistoryView } from '@whip/sdk/state';
import type { RootSnapshot, StreamEvent } from '@whip/protocol';
import { Button, CodeBlock, IconButton, Menu } from '@whip/ui';
import { Copy, ArrowDown, ChevronRight, Code2 } from 'lucide-react';
import * as stylex from '@stylexjs/stylex';
import {
  colors,
  typography,
  markdown,
  surface,
  scale,
} from '@whip/ui/tokens.stylex';
import { useRuntime } from './context';
import { layout } from './styles';

export interface TimelineRow {
  id: string;
  role: string;
  text: string;
  label?: string;
  args?: string;
  live?: boolean;
  deliveries?: number;
  body?: NonNullable<HistoryView['messages'][number]['body']>;
  seq?: number;
  images?: ImagePart[];
}
interface ImagePart {
  url: string;
  width?: number;
  height?: number;
}
/** Also used when inspecting a bounded raw message loaded from content storage. */
export function messagePresentation(content: unknown): {
  text: string;
  images: ImagePart[];
} {
  if (typeof content === 'string') return { text: content, images: [] };
  const text: string[] = [];
  const images: ImagePart[] = [];
  if (Array.isArray(content))
    for (const part of content) {
      if (!part || typeof part !== 'object') continue;
      if (part.type === 'text' && typeof part.text === 'string')
        text.push(part.text);
      else if (
        part.type === 'image_url' &&
        typeof part.image_url?.url === 'string'
      )
        images.push({ url: part.image_url.url, width: part.w, height: part.h });
      else text.push(`[Unsupported content: ${String(part.type)}]`);
    }
  return { text: text.join('\n\n'), images };
}
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
type Presentation = DeepReadonly<RootSnapshot['presentation']>;

/** Presentation grouping never changes authored messages or daemon history. */
export function timelineRows(
  history: DeepReadonly<HistoryView> | undefined,
  presentation: Presentation | undefined,
): TimelineRow[] {
  const rows: TimelineRow[] = [];
  const calls = new Map<string, TimelineRow>();
  for (const entry of history?.messages ?? []) {
    const message = entry.message;
    const id = `h:${history?.revision}:${entry.seq}`;
    if (!message) {
      rows.push({
        id,
        seq: entry.seq,
        role: entry.role || 'notice',
        text: '',
        ...(entry.body ? { body: entry.body } : {}),
      });
      continue;
    }
    const parts = messagePresentation(message.content);
    const digest =
      message.role === 'user' &&
      !message.authored &&
      parts.text.startsWith('Mailbox digest:');
    const previous = rows.at(-1);
    if (
      digest &&
      previous?.role === 'mailbox' &&
      previous.text === parts.text
    ) {
      previous.deliveries = (previous.deliveries ?? 1) + 1;
      continue;
    }
    if (
      message.role === 'tool' &&
      message.tool_call_id &&
      calls.has(message.tool_call_id)
    ) {
      calls.get(message.tool_call_id)!.text = parts.text;
      calls.get(message.tool_call_id)!.images = parts.images;
      continue;
    }
    if (parts.text || parts.images.length || !message.tool_calls?.length)
      rows.push({
        id,
        seq: entry.seq,
        role: digest ? 'mailbox' : message.role,
        text: parts.text,
        images: parts.images,
        ...(message.role === 'tool'
          ? { label: message.name || 'Tool output' }
          : {}),
      });
    for (const call of message.tool_calls ?? []) {
      const row: TimelineRow = {
        id: `${id}:${call.id}`,
        seq: entry.seq,
        role: 'tool',
        label:
          call.function.name === 'rlm_exec'
            ? 'Starlark execution'
            : call.function.name,
        args: call.function.arguments,
        text: '',
      };
      calls.set(call.id, row);
      rows.push(row);
    }
  }
  const liveCalls = new Map<string, TimelineRow>();
  for (const event of presentation ?? []) {
    const payload = event.payload as StreamEvent | undefined;
    if (!payload || typeof payload !== 'object') continue;
    if (event.kind === 'stream.text' || event.kind === 'stream.reasoning') {
      rows.push({
        id: `live:${event.seq}`,
        role: event.kind === 'stream.text' ? 'assistant' : 'reasoning',
        text: payload.text ?? '',
        live: true,
      });
    } else if (event.kind.startsWith('stream.tool.')) {
      const key = payload.id ?? event.seq;
      let row = liveCalls.get(key);
      if (!row) {
        row = {
          id: `live-tool:${key}`,
          role: 'tool',
          label:
            payload.name === 'rlm_exec'
              ? 'Starlark execution'
              : payload.name || 'Tool activity',
          text: '',
          live: true,
        };
        liveCalls.set(key, row);
        rows.push(row);
      }
      if (payload.args !== undefined) row.args = payload.args;
      if (payload.result !== undefined) row.text = payload.result;
      else if (
        event.kind === 'stream.tool.output' &&
        payload.text !== undefined
      )
        row.text = payload.text;
      if (event.kind === 'stream.tool.completed') row.live = false;
    } else if (
      event.kind === 'stream.notice' ||
      event.kind === 'stream.terminal.awaiting'
    ) {
      rows.push({
        id: `live:${event.seq}`,
        role: 'notice',
        text:
          event.kind === 'stream.terminal.awaiting'
            ? 'This tool needs interactive terminal input. Open the session in the TUI to respond, or stop the turn.'
            : (payload.text ?? ''),
      });
    }
  }
  return rows;
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
  viewport: {
    flex: 1,
    overflowY: 'auto',
    minHeight: 0,
    overflowAnchor: 'none',
  },
  inner: {
    maxWidth: 840,
    marginInline: 'auto',
    paddingInline: { default: 32, [scale.phone]: 16 },
    paddingBlock: 24,
  },
  article: {
    paddingBlock: 14,
    overflowWrap: 'anywhere',
    fontSize: 15,
    lineHeight: 1.75,
    color: colors.foreground,
  },
  user: { marginBottom: 8 },
  byline: {
    fontSize: 12,
    fontWeight: 550,
    marginBottom: 8,
    color: surface.secondaryText,
    display: 'flex',
    alignItems: 'center',
    gap: 8,
  },
  disclosure: {
    fontSize: 12,
    lineHeight: 1.65,
    color: surface.secondaryText,
    borderRadius: 8,
    backgroundColor: colors.panel,
    padding: 12,
  },
  summary: {
    display: 'flex',
    alignItems: 'center',
    gap: 8,
    cursor: 'pointer',
    listStyle: 'none',
    minHeight: 24,
  },
  image: {
    maxWidth: '100%',
    width: 420,
    maxHeight: 420,
    objectFit: 'contain',
    borderRadius: 8,
  },
  details: { marginTop: 12, maxHeight: 480, overflow: 'auto' },
  p: { marginBlock: '0 12px' },
  heading: {
    fontSize: 18,
    fontWeight: 560,
    color: markdown.heading,
    marginBlock: '18px 10px',
  },
  code: {
    fontFamily: typography.mono,
    fontSize: '0.85em',
    color: markdown.code,
    backgroundColor: colors.element,
    paddingInline: 4,
    borderRadius: 4,
  },
  codeBlock: {
    fontFamily: typography.mono,
    fontSize: 12,
    padding: 16,
    backgroundColor: colors.element,
    overflow: 'auto',
    borderRadius: 8,
    lineHeight: 1.65,
    whiteSpace: 'pre',
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
  list: { paddingLeft: 24, marginBlock: '0 12px' },
  table: {
    display: 'block',
    overflowX: 'auto',
    maxWidth: '100%',
    borderCollapse: 'collapse',
    fontSize: 13,
    marginBlock: 12,
  },
  cell: {
    padding: 8,
    textAlign: 'left',
    borderBottomWidth: 1,
    borderBottomStyle: 'solid',
    borderBottomColor: colors.border,
  },
  jump: {
    position: 'absolute',
    bottom: 16,
    left: '50%',
    transform: 'translateX(-50%)',
    boxShadow: '0 2px 8px rgb(0 0 0 / 0.08)',
  },
  region: {
    flex: 1,
    position: 'relative',
    display: 'flex',
    flexDirection: 'column',
    minHeight: 0,
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
    <Markdown
      components={markdownComponents}
      extensions={live ? streaming : undefined}
    >
      {text}
    </Markdown>
  );
});

const MessageRow = memo(function MessageRow({
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
  return (
    <article
      data-message-id={row.id}
      {...stylex.props(styles.article, row.role === 'user' && styles.user)}
    >
      {disclosure ? (
        <details {...stylex.props(styles.disclosure)}>
          <summary {...stylex.props(styles.summary)}>
            <ChevronRight size={13} />
            <Code2 size={13} />
            <span>
              {row.label ||
                (row.role === 'mailbox'
                  ? `Mailbox update${row.deliveries ? ` · ${row.deliveries} deliveries` : ''}`
                  : 'Reasoning')}
            </span>
            {row.live && <span>· in progress</span>}
          </summary>
          <div {...stylex.props(styles.details)}>
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
          </div>
        </details>
      ) : (
        <>
          <div {...stylex.props(styles.byline)}>
            <span>
              {row.role === 'user'
                ? 'You'
                : row.role === 'assistant'
                  ? 'WHIP'
                  : 'Activity'}
            </span>
            {row.live && <span>· writing</span>}
            <span {...stylex.props(layout.grow)} />
            {row.role === 'user' && row.seq !== undefined && historyAction && (
              <Menu
                trigger={
                  <Button
                    size="sm"
                    variant="ghost"
                    aria-label="Message history actions"
                  >
                    History
                  </Button>
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
            <IconButton
              label="Copy message"
              onClick={() =>
                void runtime.platform
                  .copy(row.text)
                  .catch((error) => runtime.report(error))
              }
            >
              <Copy size={13} />
            </IconButton>
          </div>
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
}: {
  rows: TimelineRow[];
  hasMore: boolean;
  loadOlder(): Promise<void>;
  readBody(row: TimelineRow): void;
  historyAction?(row: TimelineRow, action: 'fork' | 'rewind'): void;
}) {
  const viewport = useRef<HTMLDivElement>(null);
  const follow = useRef(true);
  const [atEnd, setAtEnd] = useState(true);
  const [loading, setLoading] = useState(false);
  const [pinned, setPinned] = useState<string[]>([]);
  const runtime = useRuntime();
  const virtual = useVirtualizer({
    count: rows.length,
    getScrollElement: () => viewport.current,
    estimateSize: () => 140,
    overscan: 8,
    getItemKey: (index) => rows[index]!.id,
    anchorTo: 'end',
    scrollEndThreshold: 64,
    rangeExtractor: (range) => {
      const indices = defaultRangeExtractor(range);
      const selected = pinned
        .map((id) => rows.findIndex((row) => row.id === id))
        .filter((index) => index >= 0);
      if (selected.length)
        for (
          let index = Math.min(...selected);
          index <= Math.max(...selected);
          index++
        )
          indices.push(index);
      return [...new Set(indices)].sort((left, right) => left - right);
    },
  });
  const total = virtual.getTotalSize();
  useLayoutEffect(() => {
    // A fixed offset lets the next user scroll take over. An indexed scroll can
    // keep reconciling toward the last row after the user starts reading history.
    if (follow.current)
      virtual.scrollToOffset(viewport.current?.scrollHeight ?? 0);
  }, [rows, total, virtual]);
  useEffect(() => {
    const update = () => {
      const root = viewport.current;
      if (!root) return;
      const selection = document.getSelection();
      const nodes = [
        selection?.anchorNode,
        selection?.focusNode,
        document.activeElement,
      ];
      const ids = nodes.flatMap((node) => {
        const element = node instanceof Element ? node : node?.parentElement;
        const row = element?.closest<HTMLElement>('[data-message-id]');
        return row && root.contains(row) ? [row.dataset.messageId!] : [];
      });
      setPinned((previous) =>
        previous.join('\n') === ids.join('\n') ? previous : ids,
      );
    };
    document.addEventListener('selectionchange', update);
    document.addEventListener('focusin', update);
    return () => {
      document.removeEventListener('selectionchange', update);
      document.removeEventListener('focusin', update);
    };
  }, []);
  return (
    <div {...stylex.props(styles.region)}>
      <div
        ref={viewport}
        {...stylex.props(styles.viewport)}
        role="region"
        tabIndex={0}
        aria-label="Conversation"
        onScroll={(event) => {
          const target = event.currentTarget;
          const end =
            target.scrollHeight - target.scrollTop - target.clientHeight < 64;
          follow.current = end;
          setAtEnd(end);
        }}
      >
        <div {...stylex.props(styles.inner)}>
          {hasMore && (
            <Button
              variant="ghost"
              loading={loading}
              onClick={async () => {
                follow.current = false;
                setLoading(true);
                try {
                  await loadOlder();
                } catch (error) {
                  runtime.report(error);
                } finally {
                  setLoading(false);
                }
              }}
            >
              Load earlier messages
            </Button>
          )}
          <div style={{ height: total, position: 'relative' }}>
            {virtual.getVirtualItems().map((item) => (
              <div
                key={item.key}
                data-index={item.index}
                ref={virtual.measureElement}
                style={{
                  position: 'absolute',
                  top: 0,
                  width: '100%',
                  transform: `translateY(${item.start}px)`,
                }}
              >
                <MessageRow
                  row={rows[item.index]!}
                  readBody={readBody}
                  historyAction={historyAction}
                />
              </div>
            ))}
          </div>
        </div>
      </div>
      {!atEnd && (
        <Button
          variant="secondary"
          xstyle={styles.jump}
          onClick={() => {
            follow.current = true;
            virtual.scrollToOffset(viewport.current?.scrollHeight ?? 0);
          }}
        >
          <ArrowDown size={14} /> Latest
        </Button>
      )}
    </div>
  );
}
