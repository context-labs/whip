import { useEffect, useLayoutEffect, useMemo, useRef, useState, type WheelEvent } from 'react';
import { useVirtualizer } from '@tanstack/react-virtual';
import type { RootSnapshot } from '@whip/protocol';
import { serverNowMs, traceRoots, traceSpans, type DeepReadonly, type SessionView, type SessionViewSnapshot } from '@whip/sdk/state';
import { Badge, Button, CodeBlock, IconButton, Select } from '@whip/ui';
import { ChevronDown, ChevronRight, Maximize2, ZoomIn, ZoomOut } from 'lucide-react';
import * as stylex from '@stylexjs/stylex';
import { ErrorNotice } from './error-feedback';
import { ContentRead } from './details/shared';
import { useTranscriptMotion } from './transcript-motion';
import { styles, TREE_WIDTH } from './trace-view.stylex';
import {
  buildSpanTree, fitView, flattenRows, formatDuration, formatTick, isSpanInFlight, niceTicks, panBy, rollup, ROW_HEIGHT,
  spanCategory, spanDisplayName, spanEndMs, spanZoomView, timelineDomain, traceTotals, xForTime, zoomAt, MIN_BAR_PX, LABEL_MIN_WIDTH,
  type TimelineView, type TraceNode, type TraceSpan,
} from './trace-math';

const ALL_TRACES = 'all';
const usd = (micros: number) => `$${(micros / 1e6).toFixed(micros >= 1_000_000 ? 2 : 4)}`;
const tokens = (value: number) => value >= 1_000_000 ? `${(value / 1e6).toFixed(1)}M` : value >= 10_000 ? `${Math.round(value / 1000)}K` : value >= 1_000 ? `${(value / 1000).toFixed(1)}K` : String(value);
const kb = (bytes: number) => bytes >= 1024 ? `${(bytes / 1024).toFixed(bytes >= 10_240 ? 0 : 1)} KB` : `${bytes} B`;
const statusTone = (span: TraceSpan) => span.status === 'error' ? 'error' : span.status === 'cancelled' || span.status === 'interrupted' ? 'warning' : span.status === 'running' ? 'info' : 'neutral';
const text = (value: unknown): string => typeof value === 'string' ? value : typeof value === 'number' ? String(value) : '';

/**
 * TraceView renders one trace (a root turn and everything it caused) as an
 * execution tree beside a waterfall, with a detail pane for the selected span.
 * All spans come from the SDK's bounded trace evidence; the view adds only
 * selection, expansion, zoom and the live clock.
 */
export function TraceView({ view, state, runtimeId, viewId, connected }: {
  view: SessionView;
  state: DeepReadonly<SessionViewSnapshot>;
  agentId: string;
  runtimeId: string;
  viewId: string;
  connected: boolean;
  lastTurn?: DeepReadonly<NonNullable<RootSnapshot['agents']>[number]['last_turn']>;
}) {
  const evidence = state.trace;
  // The transport identity also catches reconnects whose intermediate stale
  // state was batched away by React. Live events maintain the loaded evidence.
  const connectionId = view.session.client.getSnapshot().info?.connection_id;
  useEffect(() => {
    if (connected) void view.loadTrace().catch(() => {});
  }, [view, connected, connectionId]);
  const roots = useMemo(() => traceRoots(state), [state]);
  const [picked, setPicked] = useState<string>();
  const traceId = picked ?? roots[0]?.traceId ?? '';
  const spans = useMemo(() => traceSpans(state, traceId === ALL_TRACES ? undefined : traceId) as readonly TraceSpan[], [state, traceId]);
  const running = connected && spans.some(isSpanInFlight);
  const motion = useTranscriptMotion();
  const [now, setNow] = useState(() => serverNowMs(evidence));
  // Live evidence still advances the clock when motion is reduced.
  useEffect(() => setNow(serverNowMs(evidence)), [evidence]);
  useEffect(() => {
    if (!running || !motion) return;
    setNow(serverNowMs(view.getSnapshot().trace));
    const interval = 1000 / 30;
    let last = performance.now();
    let frame: number;
    const tick = (time: number) => {
      const elapsed = time - last;
      if (elapsed >= interval) {
        last = time - (elapsed % interval);
        setNow(serverNowMs(view.getSnapshot().trace));
      }
      frame = requestAnimationFrame(tick);
    };
    frame = requestAnimationFrame(tick);
    return () => cancelAnimationFrame(frame);
  }, [running, motion, view]);
  const tree = useMemo(() => buildSpanTree(spans), [spans]);
  const [collapsed, setCollapsed] = useState<ReadonlySet<string>>(new Set());
  const rows = useMemo(() => {
    const expanded = new Set(spans.map(span => span.id).filter(id => !collapsed.has(id)));
    return flattenRows(tree, expanded);
  }, [tree, spans, collapsed]);
  const domain = useMemo(() => timelineDomain(spans, now), [spans, now]);
  const [zoom, setZoom] = useState<TimelineView>();
  useEffect(() => setZoom(undefined), [traceId]);
  const timeline = zoom ?? fitView(domain.dur);
  const [selectedId, setSelectedId] = useState<string>();
  const selected = rows.find(row => row.span.id === selectedId) ?? undefined;
  const totals = useMemo(() => traceTotals(spans), [spans]);
  const [panes, setPanes] = useState({ tree: true, timeline: true, details: false });
  const listRef = useRef<HTMLDivElement>(null);
  const [laneWidth, setLaneWidth] = useState(600);
  useLayoutEffect(() => {
    const element = listRef.current;
    if (!element) return;
    const measure = () => setLaneWidth(Math.max(120, element.clientWidth - (panes.tree ? TREE_WIDTH : 0) - 1));
    measure();
    if (typeof ResizeObserver === 'undefined') return;
    const observer = new ResizeObserver(measure);
    observer.observe(element);
    return () => observer.disconnect();
  }, [panes.tree]);
  const virtual = useVirtualizer({ count: rows.length, getScrollElement: () => listRef.current, estimateSize: () => ROW_HEIGHT, overscan: 12, getItemKey: index => rows[index]!.span.id });
  const rel = (ms: number) => ms - domain.startMs;
  const onWheel = (event: WheelEvent<HTMLDivElement>) => {
    if (!panes.timeline) return;
    if (event.ctrlKey || event.metaKey) {
      event.preventDefault();
      const lane = (event.currentTarget.getBoundingClientRect().left + (panes.tree ? TREE_WIDTH : 0));
      const anchor = timeline.t0 + ((event.clientX - lane) / laneWidth) * (timeline.t1 - timeline.t0);
      setZoom(zoomAt(timeline, Math.exp(event.deltaY * 0.0022), anchor, domain.dur));
    } else if (Math.abs(event.deltaX) > Math.abs(event.deltaY)) {
      event.preventDefault();
      setZoom(panBy(timeline, (event.deltaX / laneWidth) * (timeline.t1 - timeline.t0), domain.dur));
    }
  };
  const loading = !!evidence?.loading && !spans.length;
  const options = [
    ...roots.map((root, index) => ({ value: root.traceId, label: `${roots.length - index}. ${text(root.attrs.input) || spanDisplayName(root)}`.slice(0, 80) })),
    ...(roots.length > 1 ? [{ value: ALL_TRACES, label: 'Whole session' }] : []),
  ];
  return <div {...stylex.props(styles.root)} data-session-view="trace">
    <div {...stylex.props(styles.toolbar)}>
      {options.length > 0 && <Select label="Trace" options={options} value={traceId} onValueChange={value => { setPicked(value); setSelectedId(undefined); }} />}
      {running && <Badge tone="info">running</Badge>}
      <span {...stylex.props(styles.toggles)} role="group" aria-label="Panes">
        {(['tree', 'timeline', 'details'] as const).map(pane => <Button key={pane} variant={panes[pane] ? 'secondary' : 'ghost'} size="sm" aria-pressed={panes[pane]}
          onClick={() => setPanes(current => ({ ...current, [pane]: !current[pane] }))}>{pane === 'tree' ? 'Tree' : pane === 'timeline' ? 'Timeline' : 'Details'}</Button>)}
      </span>
      {panes.timeline && <span {...stylex.props(styles.toggles)} role="group" aria-label="Zoom">
        <IconButton variant="ghost" size="sm" label="Zoom in" onClick={() => setZoom(zoomAt(timeline, 1 / 1.45, (timeline.t0 + timeline.t1) / 2, domain.dur))}><ZoomIn size={14} /></IconButton>
        <IconButton variant="ghost" size="sm" label="Zoom out" onClick={() => setZoom(zoomAt(timeline, 1.45, (timeline.t0 + timeline.t1) / 2, domain.dur))}><ZoomOut size={14} /></IconButton>
        <IconButton variant="ghost" size="sm" label="Fit timeline" onClick={() => setZoom(undefined)}><Maximize2 size={14} /></IconButton>
      </span>}
      <span {...stylex.props(styles.totals)} aria-label="Trace totals">
        <Total label="duration" value={formatDuration(spans.length ? domain.dur : 0)} />
        <Total label="spans" value={String(totals.spans)} />
        <Total label="tokens" value={totals.promptTokens === null && totals.completionTokens === null ? '—' : tokens((totals.promptTokens ?? 0) + (totals.completionTokens ?? 0))} />
        <Total label="cost" value={totals.costMicros === null ? '—' : usd(totals.costMicros)} />
      </span>
    </div>
    {!connected && <p role="status" {...stylex.props(styles.notice)}>Trace updates are paused. Showing the last available spans.</p>}
    {evidence?.truncated && <p role="status" {...stylex.props(styles.notice)}>Older traces were dropped to keep this view within its memory limit. Export the session for the complete trace.</p>}
    {evidence?.hasMore && !evidence.error && <Button variant="ghost" disabled={!connected || evidence.loading} onClick={() => void view.loadTrace().catch(() => {})}>{evidence.loading ? 'Loading trace…' : 'Load more spans'}</Button>}
    {evidence?.error && <ErrorNotice type="session" owner={`${viewId}:trace`} error={evidence.error} action={<Button variant="ghost" onClick={() => void view.loadTrace().catch(() => {})}>Retry</Button>} />}
    <div {...stylex.props(styles.body)}>
      {spans.length ? <div ref={listRef} role="tree" aria-label="Trace spans" tabIndex={0} {...stylex.props(styles.list)} onWheel={onWheel}>
        <div {...stylex.props(styles.columns)}>
          {panes.tree && <span {...stylex.props(styles.columnLabel)} style={{ width: TREE_WIDTH }}>Execution</span>}
          {panes.timeline && <div {...stylex.props(styles.axis)} aria-hidden="true">
            {niceTicks(timeline, laneWidth).map(tick => <span key={tick} {...stylex.props(styles.tick)} style={{ left: xForTime(tick, timeline, laneWidth) }}>{formatTick(tick)}</span>)}
          </div>}
        </div>
        <div {...stylex.props(styles.rows)} style={{ height: virtual.getTotalSize() }}>
          {virtual.getVirtualItems().map(item => {
            const node = rows[item.index]!;
            const span = node.span;
            const category = spanCategory(span);
            const open = isSpanInFlight(span);
            const start = xForTime(rel(span.startMs), timeline, laneWidth);
            const end = xForTime(rel(spanEndMs(span, now)), timeline, laneWidth);
            const width = Math.max(MIN_BAR_PX, end - start);
            const durationLabel = open ? 'running' : formatDuration(spanEndMs(span) - span.startMs);
            return <div key={item.key} role="treeitem" aria-level={node.depth + 1} aria-selected={span.id === selectedId} tabIndex={-1}
              aria-expanded={node.children.length ? !collapsed.has(span.id) : undefined} aria-label={`${spanDisplayName(span)} · ${durationLabel}`}
              {...stylex.props(styles.row, span.id === selectedId && styles.rowSelected)} style={{ top: item.start, height: ROW_HEIGHT }}
              onClick={() => { setSelectedId(span.id); setPanes(current => current.details ? current : { ...current, details: true }); }}
              onDoubleClick={() => setZoom(spanZoomView(rel(span.startMs), rel(spanEndMs(span, now)), domain.dur))}>
              {panes.tree && <div {...stylex.props(styles.tree)} style={{ width: TREE_WIDTH, paddingLeft: 8 + node.depth * 14 }}>
                {node.children.length ? <button type="button" {...stylex.props(styles.toggle)} aria-label={collapsed.has(span.id) ? 'Expand' : 'Collapse'} onClick={event => {
                  event.stopPropagation();
                  setCollapsed(current => { const next = new Set(current); if (next.has(span.id)) next.delete(span.id); else next.add(span.id); return next; });
                }}>{collapsed.has(span.id) ? <ChevronRight size={12} /> : <ChevronDown size={12} />}</button> : <span {...stylex.props(styles.toggleSpacer)} />}
                <span aria-hidden="true" {...stylex.props(styles.dot, styles[category])} />
                <span {...stylex.props(styles.label, span.status === 'error' && styles.labelMuted)} title={spanDisplayName(span)}>{spanDisplayName(span)}</span>
                <span {...stylex.props(styles.duration)}>{durationLabel}</span>
              </div>}
              {panes.timeline && <div {...stylex.props(styles.lane)}>
                <div {...stylex.props(styles.bar, styles[category], open && styles.barRunning, span.status === 'error' && styles.barError)} style={{ left: start, width }} title={`${spanDisplayName(span)} · ${durationLabel}`}>
                  {width > LABEL_MIN_WIDTH && spanDisplayName(span)}
                </div>
              </div>}
            </div>;
          })}
        </div>
      </div> : <div {...stylex.props(styles.empty)}>
        <strong>{loading ? 'Loading trace…' : !evidence?.loaded || evidence.error ? 'Trace unavailable' : 'No recorded spans'}</strong>
        <span>{loading ? 'Reading the session’s recorded spans.' : evidence?.error ? 'Recorded spans could not be loaded. Retry to read them.' : !evidence?.loaded ? connected ? 'Recorded spans have not been loaded.' : 'Reconnect to read the recorded spans.' : 'This conversation has no recorded span data. Older conversations may predate tracing; historical timings are not reconstructed.'}</span>
      </div>}
      {panes.details && spans.length > 0 && <SpanDetails view={view} node={selected} domainStartMs={domain.startMs} now={now} agents={state.root?.agents} />}
    </div>
    {spans.length > 0 && panes.timeline && <p {...stylex.props(styles.hint)}>Click a row to inspect it · ⌘ or Ctrl + scroll to zoom · horizontal scroll to pan · double-click a span to zoom to it</p>}
  </div>;
}

function Total({ label, value }: { label: string; value: string }) {
  return <span {...stylex.props(styles.total)}><span {...stylex.props(styles.totalLabel)}>{label}</span><span {...stylex.props(styles.totalValue)}>{value}</span></span>;
}

function SpanDetails({ view, node, domainStartMs, now, agents }: { view: SessionView; node?: TraceNode; domainStartMs: number; now: number; agents?: DeepReadonly<RootSnapshot['agents']> }) {
  const [raw, setRaw] = useState(false);
  const sums = useMemo(() => node?.span.kind === 'agent' ? rollup(node) : undefined, [node]);
  if (!node) return <aside aria-label="Span details" {...stylex.props(styles.details)}><span {...stylex.props(styles.sectionLabel)}>Details</span><p {...stylex.props(styles.notice)}>Select a span to inspect it.</p></aside>;
  const span = node.span;
  const group = span.kind === 'agent';
  const open = isSpanInFlight(span);
  const attrs = span.attrs;
  const cost = group ? sums!.costMicros : text(attrs.cost_source) === 'reported' || text(attrs.cost_source) === 'estimated' ? Number(attrs.cost_micros ?? 0) : null;
  const promptTokens = group ? sums!.promptTokens : span.kind === 'llm' ? Number(attrs.prompt_tokens ?? 0) : null;
  const completionTokens = group ? sums!.completionTokens : span.kind === 'llm' ? Number(attrs.completion_tokens ?? 0) : null;
  const agentName = agents?.find(agent => agent.id === span.agentId)?.name || text(attrs.agent_name) || span.agentId;
  // Bodies the daemon interned instead of excerpting: the prompt the call
  // sent and the summary a compaction produced, read on demand by reference.
  const references = ([['System prompt', 'system_prompt_ref', 'system_prompt_bytes'], ['Ephemeral notice', 'ephemeral_ref', 'ephemeral_bytes'], ['Compaction summary', 'output_ref', 'output_bytes']] as const)
    .flatMap(([label, refKey, bytesKey]) => text(attrs[refKey]) ? [{ label, reference: text(attrs[refKey]), bytes: Number(attrs[bytesKey] ?? 0) }] : []);
  const stats: [string, string][] = [
    ['duration', open ? `running · ${formatDuration(now - span.startMs)}` : formatDuration(spanEndMs(span) - span.startMs)],
    ['start', `+${formatDuration(span.startMs - domainStartMs)}`],
    ['end', open ? '—' : `+${formatDuration(spanEndMs(span) - domainStartMs)}`],
    [group ? 'cost (rolled up)' : 'cost', cost === null ? '—' : usd(cost)],
    ['tokens in', promptTokens === null ? '—' : tokens(promptTokens)],
    ['tokens out', completionTokens === null ? '—' : tokens(completionTokens)],
    ...(span.kind === 'llm' ? [['model', text(attrs.model) || span.name] as [string, string]] : []),
    ...references.map(({ label, bytes }) => [label.toLowerCase(), bytes > 0 ? kb(bytes) : '—'] as [string, string]),
    ...(text(attrs.raw_cutoff) ? [['folded through seq', text(attrs.raw_cutoff)] as [string, string]] : []),
    ...(group ? [['children', String(sums!.descendants)] as [string, string]] : []),
    ['agent', agentName],
    ['status', span.status],
  ];
  const bodies: [string, string][] = ([['input', 'input'], ['output', 'output'], ['error', 'error']] as const)
    .flatMap(([label, key]) => text(attrs[key]) ? [[label, text(attrs[key])] as [string, string]] : []);
  return <aside aria-label="Span details" {...stylex.props(styles.details)}>
    <div {...stylex.props(styles.detailsHeader)}>
      <span aria-hidden="true" {...stylex.props(styles.dot, styles[spanCategory(span)])} />
      <span {...stylex.props(styles.detailsTitle)}>{spanDisplayName(span)}</span>
      <Badge tone={statusTone(span)}>{span.kind}</Badge>
      <span {...stylex.props(styles.toggles)} role="group" aria-label="Detail mode">
        <Button variant={raw ? 'ghost' : 'secondary'} size="sm" aria-pressed={!raw} onClick={() => setRaw(false)}>Overview</Button>
        <Button variant={raw ? 'secondary' : 'ghost'} size="sm" aria-pressed={raw} onClick={() => setRaw(true)}>Raw</Button>
      </span>
    </div>
    {raw ? <CodeBlock code={JSON.stringify({ id: span.id, trace_id: span.traceId, parent_id: span.parentId, turn_id: span.turnId, kind: span.kind, name: span.name, status: span.status, start_ms: span.startMs, end_ms: span.endMs, links: span.links, attrs }, null, 2)} language="json" label="Raw span" xstyle={styles.code} />
      : <>
        <dl {...stylex.props(styles.stats)}>
          {stats.map(([label, value]) => <div key={label} {...stylex.props(styles.stat)}><dt {...stylex.props(styles.statLabel)}>{label}</dt><dd {...stylex.props(styles.statValue)}>{value}</dd></div>)}
        </dl>
        {bodies.map(([label, value]) => <div key={label} {...stylex.props(styles.section)}>
          <CodeBlock code={value} label={label} xstyle={styles.code} />
        </div>)}
        {references.map(({ label, reference }) => <div key={label} {...stylex.props(styles.section)}>
          <ContentRead view={view} referenceId={reference} label={label} />
        </div>)}
        {!bodies.length && !references.length && <p {...stylex.props(styles.notice)}>This span recorded no excerpt. Export the session for full bodies.</p>}
      </>}
  </aside>;
}
