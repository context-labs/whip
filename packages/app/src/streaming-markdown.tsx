import { Children, cloneElement, isValidElement, memo, useContext, useEffect, useLayoutEffect, useRef, useState, type ReactElement, type ReactNode } from 'react';
import { parseMarkdown } from '@tanstack/markdown/parser';
import { renderBlockReact, type MarkdownReactOptions } from '@tanstack/markdown/react';
import { streamingMarkdownExtension } from '@tanstack/markdown/extensions/streaming';
import type { BlockNode } from '@tanstack/markdown';
import { CodeBlock } from '@whip/ui';
import type { ConversationActivityRow } from './chat-activity-rows';
import type { TimelineRow } from './conversation-rows';
import { MotionContext, fadeDuration, type Arrival } from './transcript-motion';

export interface MarkdownRow extends TimelineRow { block: BlockNode; ownerId: string; blockIndex: number }
export const isMarkdownRow = (row: TimelineRow): row is MarkdownRow => 'block' in row;

/** Display-only completion of unambiguous trailing inline delimiters. Fences
 * already remain code in TanStack's parser; ambiguous link destinations stay text. */
export function streamingSource(text: string): string {
  const fence = text.match(/^\s*(`{3,}|~{3,})/gm);
  if (fence && fence.length % 2) return text;
  const boundary = text.lastIndexOf('\n\n');
  const tail = text.slice(boundary < 0 ? 0 : boundary + 2);
  const markers = tail.matchAll(/(?<!\\)(`+|\*\*|__|(?<!\*)\*(?!\*)|(?<!_)_(?!_))/g);
  const stack: string[] = [];
  for (const match of markers) {
    const marker = match[0], before = tail[match.index - 1] ?? '', after = tail[match.index + marker.length] ?? '';
    if (stack.at(-1) === marker && /\S/.test(before)) stack.pop();
    else if (!stack.at(-1)?.startsWith('`') && /\S/.test(after) && !( /[\p{L}\p{N}]/u.test(before) && /[\p{L}\p{N}]/u.test(after))) stack.push(marker);
  }
  return text + stack.reverse().join('');
}

/** Coalesce rendering, not transport or SDK state. Inputs and settlement publish immediately. */
export function useCoalescedTranscript(rows: readonly ConversationActivityRow[]) {
  const [, publish] = useState(0);
  const inputKey = rows.filter(row => row.role === 'user').map(row => row.id).join('\n');
  const shown = useRef({ rows, time: performance.now(), inputKey });
  const settled = !rows.some(row => row.live);
  const now = performance.now();
  if (rows !== shown.current.rows && !document.hidden && (settled || inputKey !== shown.current.inputKey || now - shown.current.time >= 1000 / 30))
    shown.current = { rows, time: now, inputKey };
  useEffect(() => {
    if (document.hidden || rows === shown.current.rows) return;
    const timer = setTimeout(() => publish(value => value + 1), Math.max(0, 1000 / 30 - (performance.now() - shown.current.time)));
    return () => clearTimeout(timer);
  });
  useEffect(() => {
    const show = () => { if (!document.hidden) publish(value => value + 1); };
    document.addEventListener('visibilitychange', show);
    return () => document.removeEventListener('visibilitychange', show);
  }, []);
  return shown.current.rows;
}

export function markdownRows(rows: readonly ConversationActivityRow[], cache: Map<string, { text: string; live: boolean; rows: MarkdownRow[] }>): (ConversationActivityRow | MarkdownRow)[] {
  const retained = new Set(rows.map(row => row.id));
  for (const id of cache.keys()) if (!retained.has(id)) cache.delete(id);
  const result = rows.flatMap(row => {
    if (row.role !== 'assistant' || row.body || row.images?.length || !row.text.trim()) return [row];
    const previous = cache.get(row.id);
    if (previous?.text === row.text && previous.live === !!row.live) return previous.rows;
    const document = parseMarkdown(row.live ? streamingSource(row.text) : row.text, { extensions: row.live ? [streamingMarkdownExtension()] : undefined });
    const blocks: MarkdownRow[] = document.children.map((block, index) => {
      const prior = previous?.rows[index];
      const same = prior && JSON.stringify(prior.block) === JSON.stringify(block);
      return { ...row, id: `${row.id}:block:${index}`, ownerId: row.id, blockIndex: index, block: same ? prior.block : block, memberIds: [row.id, ...(row.memberIds ?? [])] };
    });
    cache.set(row.id, { text: row.text, live: !!row.live, rows: blocks });
    return blocks;
  });
  // AST caches are an optimization, independent of the SDK's history budget.
  let bytes = [...cache.values()].reduce((total, value) => total + value.text.length * 2, 0);
  for (const [id, value] of cache) {
    if (bytes <= 2 * 1024 * 1024 && cache.size <= 512) break;
    cache.delete(id); bytes -= value.text.length * 2;
  }
  return result;
}

interface Chunk { id: number; start: number; end: number; time: number; duration: number }
function FadingText({ chunk, speed, enabled, children }: { chunk: Chunk; speed: number; enabled: boolean; children: string }) {
  const ref = useRef<HTMLSpanElement>(null);
  const animation = useRef<Animation | undefined>(undefined);
  useLayoutEffect(() => {
    if (!enabled || !ref.current?.animate) return;
    const elapsed = performance.now() - chunk.time;
    if (elapsed >= chunk.duration) return;
    const frames = Array.from({ length: 33 }, (_, i) => ({ offset: i / 32, opacity: 1 - Math.pow(1 - i / 32, 1.6) }));
    const current = ref.current.animate(frames, { duration: chunk.duration });
    current.currentTime = Math.max(0, elapsed);
    animation.current = current;
    return () => { current.cancel(); animation.current = undefined; };
  }, [chunk, enabled]);
  useEffect(() => { animation.current?.updatePlaybackRate?.(speed); }, [speed]);
  return <span ref={ref} data-stream-chunk>{children}</span>;
}

function plain(node: ReactNode): string {
  if (typeof node === 'string' || typeof node === 'number') return String(node);
  if (Array.isArray(node)) return node.map(plain).join('');
  return isValidElement<{ children?: ReactNode }>(node) ? plain(node.props.children) : '';
}

export const MarkdownBlock = memo(function MarkdownBlock({ row, components, arrival }: { row: MarkdownRow; components: MarkdownReactOptions['components']; arrival?: Arrival }) {
  const motion = useContext(MotionContext);
  const root = useRef<HTMLDivElement>(null);
  const displayed = useRef(row);
  const deferred = useRef(false);
  const [, released] = useState(0);
  const selected = () => {
    const selection = document.getSelection();
    return !!root.current && !!selection?.rangeCount && !selection.isCollapsed && selection.getRangeAt(0).intersectsNode(root.current);
  };
  // Keep the selected DOM intact even if Markdown delimiters or code tokens
  // are reclassified. Only this selected block waits; other blocks keep flowing.
  if (selected()) deferred.current ||= displayed.current !== row;
  else displayed.current = row;
  useEffect(() => {
    const release = () => { if (deferred.current && !selected()) released(value => value + 1); };
    document.addEventListener('selectionchange', release);
    return () => document.removeEventListener('selectionchange', release);
  }, []);
  const shown = displayed.current;
  const tree = renderBlockReact(shown.block, { components }, shown.id);
  const text = plain(tree);
  const model = useRef({ text: arrival && !arrival.consumed && motion && performance.now() - arrival.time < 400 ? '' : text, time: undefined as number | undefined, average: 160, serial: 0, chunks: [] as Chunk[] });
  useLayoutEffect(() => { if (arrival) arrival.consumed = true; }, [arrival]);
  const state = model.current;
  const now = performance.now();
  if ((!motion || !shown.live || (deferred.current && !selected())) && !selected()) {
    state.text = text; state.chunks = []; deferred.current = false;
  }
  if (state.text !== text) {
    state.chunks = state.chunks.filter(chunk => now - chunk.time < chunk.duration);
    if (shown.live && motion && text.startsWith(state.text)) {
      const timing = fadeDuration(state.average, state.time === undefined ? state.average : now - state.time);
      state.average = timing.average;
      state.chunks.push({ id: ++state.serial, start: state.text.length, end: text.length, time: now, duration: timing.duration });
      state.chunks = state.chunks.slice(-32);
    } else state.chunks = [];
    state.text = text; state.time = now;
  }
  const speed = 1 + .3 * Math.max(0, state.chunks.length - 2);
  const decorate = (text: string, offset: number): ReactNode => {
    if (!shown.live || !motion || !state.chunks.length) return text;
    const pieces: ReactNode[] = [];
    let cursor = offset;
    for (const chunk of state.chunks) {
      const start = Math.max(offset, chunk.start), end = Math.min(offset + text.length, chunk.end);
      if (end <= start) continue;
      if (cursor < start) pieces.push(text.slice(cursor - offset, start - offset));
      pieces.push(<FadingText key={chunk.id} chunk={chunk} speed={speed} enabled={motion && !!row.live}>{text.slice(start - offset, end - offset)}</FadingText>);
      cursor = end;
    }
    if (cursor < offset + text.length) pieces.push(text.slice(cursor - offset));
    return pieces;
  };
  let offset = 0;
  const visit = (node: ReactNode): ReactNode => {
    if (typeof node === 'string') { const start = offset; offset += node.length; return decorate(node, start); }
    if (!isValidElement<{ children?: ReactNode; className?: string }>(node)) return node;
    if (node.type === components?.pre) {
      const code = node.props.children;
      if (isValidElement<{ children?: ReactNode; className?: string }>(code) && typeof code.props.children === 'string') {
        const start = offset; offset += code.props.children.length;
        return <CodeBlock key={node.key} code={code.props.children} language={code.props.className?.replace(/^language-/, '')} renderText={(value, position) => decorate(value, start + position)} />;
      }
    }
    return cloneElement(node as ReactElement<{ children?: ReactNode }>, {}, Children.map(node.props.children, child => visit(child)));
  };
  return <div ref={root} data-message-prose data-markdown-block={row.id}>{visit(tree)}</div>;
}, (before, after) => before.row.block === after.row.block && before.row.live === after.row.live && before.row.id === after.row.id && before.components === after.components && before.arrival === after.arrival);
