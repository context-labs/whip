import { useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';
import { useVirtualizer } from '@tanstack/react-virtual';
import type { Client } from '@whip/sdk';
import { traceNowNS, type TraceView as TraceObserver } from '@whip/sdk/state';
import { useTraceView } from '@whip/sdk/react';
import { Badge, Button, CodeBlock, IconButton, Select, ToggleGroup } from '@whip/ui';
import { ChevronDown, ChevronRight, Maximize2, ZoomIn, ZoomOut } from 'lucide-react';
import * as stylex from '@stylexjs/stylex';
import { ErrorNotice } from './error-feedback';
import { ModelRequestInspection } from './details/model-inspection';
import { useRuntime } from './context';
import { useTranscriptMotion } from './transcript-motion';
import { TraceResizeHandle } from './trace-resize-handle';
import { styles, TREE_WIDTH, DETAILS_WIDTH } from './trace-view.stylex';
import {
  buildSpanTree,
  fitView,
  flattenRows,
  formatDuration,
  formatTick,
  isSpanInFlight,
  niceTicks,
  panBy,
  rollup,
  ROW_HEIGHT,
  projectTraceRows,
  formatCount,
  formatNanoUSD,
  spanCategory,
  spanDisplayName,
  spanEndMs,
  spanZoomView,
  timelineDomain,
  traceTotals,
  xForTime,
  zoomAt,
  MIN_BAR_PX,
  LABEL_MIN_WIDTH,
  type TimelineView,
  type TraceNode,
  type TraceSpan,
} from './trace-math';

const statusTone = (span: TraceSpan) =>
  span.status === 'failed' || span.status === 'denied'
    ? 'error'
    : span.status === 'cancelled' || span.status === 'interrupted' || span.status === 'uncertain'
      ? 'warning'
      : isSpanInFlight(span)
        ? 'info'
        : 'neutral';
const text = (value: unknown): string =>
  typeof value === 'string' || typeof value === 'bigint' ? String(value) : '';
const known = (value: bigint | null, missing: number, format = formatCount) =>
  value === null ? 'Unknown' : `${format(value)}${missing ? ' + unknown' : ''}`;
const elapsedLabel = (span: TraceSpan, now: number) =>
  span.endMs !== null && span.endMs < span.startMs
    ? 'Timing unavailable'
    : isSpanInFlight(span)
      ? now < span.startMs
        ? `${span.status} · clock mismatch`
        : `${span.status} · ${formatDuration(now - span.startMs)}`
      : formatDuration(spanEndMs(span) - span.startMs);

/** The SDK owns bounded canonical evidence. This pane owns only selection,
 * expansion, geometry and display animation; application leases own all reads. */
export function TraceView({
  view,
  client,
  viewId,
  connected: attached,
}: {
  view: TraceObserver;
  client: Client;
  viewId: string;
  connected: boolean;
}) {
  const runtime = useRuntime();
  const evidence = useTraceView(view);
  const connected =
    attached && client.runtimeID === evidence.runtimeID && client.processEpoch === evidence.epoch;
  const { spans, originNS } = useMemo(() => projectTraceRows(evidence.rows), [evidence.rows]);
  const roots = useMemo(() => projectTraceRows(evidence.roots).spans, [evidence.roots]);
  const traceId = evidence.traceID;
  const running = connected && evidence.status === 'live' && spans.some(isSpanInFlight);
  const motion = useTranscriptMotion();
  const [nowNS, setNowNS] = useState<bigint | null>(() => traceNowNS(evidence));
  useEffect(() => setNowNS(traceNowNS(evidence)), [evidence.observedAtNS, evidence.receivedAtMs]);
  useEffect(() => {
    if (!running || !motion) return;
    setNowNS(traceNowNS(view.getSnapshot()));
    const interval = 1000 / 30;
    let last = performance.now();
    let frame: number;
    const tick = (time: number) => {
      const elapsed = time - last;
      if (elapsed >= interval) {
        last = time - (elapsed % interval);
        setNowNS(traceNowNS(view.getSnapshot()));
      }
      frame = requestAnimationFrame(tick);
    };
    frame = requestAnimationFrame(tick);
    return () => cancelAnimationFrame(frame);
  }, [running, motion, view]);
  const now = nowNS === null ? 0 : Number(nowNS - originNS) / 1e6;
  const [busy, setBusy] = useState(false);
  const [actionError, setActionError] = useState('');
  const download = useRef<AbortController | null>(null);
  const generation = useRef(0);
  useEffect(() => {
    setActionError('');
    setBusy(false);
    return () => {
      generation.current++;
      download.current?.abort();
    };
  }, [client, view]);
  useEffect(() => {
    if (!connected) download.current?.abort();
  }, [connected]);
  async function action(run: () => Promise<void>) {
    if (!connected || busy) return;
    const current = generation.current;
    setBusy(true);
    setActionError('');
    try {
      await run();
    } catch (error) {
      if (current === generation.current)
        setActionError(error instanceof Error ? error.message : String(error));
    } finally {
      if (current === generation.current) setBusy(false);
    }
  }
  async function exportTrace() {
    const controller = new AbortController();
    download.current = controller;
    const result = await client.exportTrace(
      {
        root_id: evidence.rootID,
        trace_id: evidence.traceID,
        expected_revision: evidence.revision,
      },
      { signal: controller.signal },
    );
    const bytes = await client
      .session(evidence.rootID)
      .content.readBytes(result.reference, { signal: controller.signal, maxBytes: 4 << 20 });
    controller.signal.throwIfAborted();
    await runtime.platform.download(bytes, 'whip-trace.json', result.reference.media_type);
  }
  const tree = useMemo(() => buildSpanTree(spans), [spans]);
  const [collapsed, setCollapsed] = useState<ReadonlySet<string>>(new Set());
  useEffect(() => {
    const present = new Set(spans.map((span) => span.id));
    setCollapsed((current) => {
      const next = new Set([...current].filter((id) => present.has(id)));
      return next.size === current.size ? current : next;
    });
  }, [spans]);
  const rows = useMemo(() => {
    const expanded = new Set(spans.map((span) => span.id).filter((id) => !collapsed.has(id)));
    return flattenRows(tree, expanded);
  }, [tree, spans, collapsed]);
  const domain = useMemo(() => timelineDomain(spans, now), [spans, now]);
  const [zoom, setZoom] = useState<TimelineView>();
  useEffect(() => {
    setZoom(undefined);
    setSelectedId(undefined);
  }, [traceId, evidence.windowBefore, evidence.epoch]);
  const timeline = zoom ?? fitView(domain.dur);
  const [selectedId, setSelectedId] = useState<string>();
  const selected = rows.find((row) => row.span.id === selectedId) ?? undefined;
  const totals = useMemo(() => traceTotals(spans), [spans]);
  const [panes, setPanes] = useState({ tree: true, timeline: true, details: false });
  const hasSpans = spans.length > 0;
  const showDetails = panes.details && hasSpans;
  const bodyRef = useRef<HTMLDivElement>(null);
  const listRef = useRef<HTMLDivElement>(null);
  const [widths, setWidths] = useState({ tree: TREE_WIDTH, details: DETAILS_WIDTH });
  const [measured, setMeasured] = useState({ body: 1000, list: 1000 });
  // Keep all visible panes reachable when the workspace becomes narrow.
  const mainMin = (panes.tree ? 160 : 0) + (panes.timeline ? 120 : 0) || 120;
  const shrink = Math.min(1, measured.body / (mainMin + (showDetails ? 200 : 0)));
  const detailsMin = 200 * shrink;
  const detailsMax = Math.max(detailsMin, measured.body - mainMin * shrink);
  const detailsWidth = showDetails ? Math.max(detailsMin, Math.min(widths.details, detailsMax)) : 0;
  const treeMin = 160 * shrink;
  const treeMax = Math.max(treeMin, measured.body - detailsWidth - 120 * shrink);
  const treeWidth = panes.timeline
    ? Math.max(treeMin, Math.min(widths.tree, treeMax))
    : measured.list;
  const laneWidth = Math.max(1, measured.list - (panes.tree ? treeWidth : 0) - 1);
  useLayoutEffect(() => {
    const measure = () =>
      setMeasured((current) => ({
        body: bodyRef.current?.clientWidth || current.body,
        list: listRef.current?.clientWidth || current.list,
      }));
    measure();
    if (typeof ResizeObserver === 'undefined') return;
    const observer = new ResizeObserver(measure);
    if (bodyRef.current) observer.observe(bodyRef.current);
    if (listRef.current) observer.observe(listRef.current);
    return () => observer.disconnect();
  }, [hasSpans]);
  const virtual = useVirtualizer({
    count: rows.length,
    getScrollElement: () => listRef.current,
    estimateSize: () => ROW_HEIGHT,
    overscan: 12,
    getItemKey: (index) => rows[index]!.span.id,
  });
  const rel = (ms: number) => ms - domain.startMs;
  useEffect(() => {
    const list = listRef.current;
    if (!list || !panes.timeline) return;
    const onWheel = (event: WheelEvent) => {
      if (event.ctrlKey || event.metaKey) {
        event.preventDefault();
        const lane = list.getBoundingClientRect().left + (panes.tree ? treeWidth : 0);
        const anchor =
          timeline.t0 + ((event.clientX - lane) / laneWidth) * (timeline.t1 - timeline.t0);
        setZoom(zoomAt(timeline, Math.exp(event.deltaY * 0.0022), anchor, domain.dur));
      } else if (Math.abs(event.deltaX) > Math.abs(event.deltaY)) {
        event.preventDefault();
        setZoom(
          panBy(timeline, (event.deltaX / laneWidth) * (timeline.t1 - timeline.t0), domain.dur),
        );
      }
    };
    // React delegates wheel events passively, but zoom/pan must cancel native scrolling.
    list.addEventListener('wheel', onWheel, { passive: false });
    return () => list.removeEventListener('wheel', onWheel);
  }, [hasSpans, panes.timeline, panes.tree, treeWidth, laneWidth, timeline, domain.dur]);
  const loading = (evidence.status === 'loading' || evidence.status === 'idle') && !spans.length;
  const options = [
    { value: '', label: 'All traces · loaded window' },
    ...roots.map((root) => ({
      value: root.traceId,
      label: `${spanDisplayName(root)} · ${root.turnId}`.slice(0, 100),
    })),
    ...(traceId && !roots.some((root) => root.traceId === traceId)
      ? [{ value: traceId, label: `Selected trace · ${traceId}` }]
      : []),
  ];
  return (
    <div {...stylex.props(styles.root)} data-session-view="trace">
      <div {...stylex.props(styles.toolbar)}>
        {options.length > 0 && (
          <Select
            label="Trace"
            options={options}
            value={traceId}
            disabled={!connected || busy}
            onValueChange={(value) => void action(() => view.selectTrace(value))}
          />
        )}
        {running && <Badge tone="info">running</Badge>}
        <ToggleGroup
          label="Panes"
          size="sm"
          multiple
          items={[
            { value: 'tree', label: 'Tree' },
            { value: 'timeline', label: 'Timeline' },
            { value: 'details', label: 'Details' },
          ]}
          value={(['tree', 'timeline', 'details'] as const).filter((pane) => panes[pane])}
          onValueChange={(value) =>
            setPanes({
              tree: value.includes('tree'),
              timeline: value.includes('timeline'),
              details: value.includes('details'),
            })
          }
        />
        {panes.timeline && (
          <span {...stylex.props(styles.toggles)} role="group" aria-label="Zoom">
            <IconButton
              variant="ghost"
              size="sm"
              label="Zoom in"
              onClick={() =>
                setZoom(zoomAt(timeline, 1 / 1.45, (timeline.t0 + timeline.t1) / 2, domain.dur))
              }
            >
              <ZoomIn size={14} />
            </IconButton>
            <IconButton
              variant="ghost"
              size="sm"
              label="Zoom out"
              onClick={() =>
                setZoom(zoomAt(timeline, 1.45, (timeline.t0 + timeline.t1) / 2, domain.dur))
              }
            >
              <ZoomOut size={14} />
            </IconButton>
            <IconButton
              variant="ghost"
              size="sm"
              label="Fit timeline"
              onClick={() => setZoom(undefined)}
            >
              <Maximize2 size={14} />
            </IconButton>
          </span>
        )}
        <span {...stylex.props(styles.totals)} aria-label="Loaded span totals">
          <Total label="duration" value={formatDuration(spans.length ? domain.dur : 0)} />
          <Total label="spans" value={String(totals.spans)} />
          <Total
            label="tokens"
            value={
              totals.promptTokens === null && totals.completionTokens === null
                ? 'Unknown'
                : known(
                    (totals.promptTokens ?? 0n) + (totals.completionTokens ?? 0n),
                    totals.missingInput + totals.missingOutput,
                  )
            }
          />
          <Total
            label="cost"
            value={known(totals.costNanoUSD, totals.missingCost, formatNanoUSD)}
          />
        </span>
      </div>
      {!connected && (
        <p role="status" {...stylex.props(styles.notice)}>
          Trace updates are paused. Showing the last available spans.
        </p>
      )}
      {evidence.truncated && (
        <p role="status" {...stylex.props(styles.notice)}>
          Showing a bounded window. Totals cover only loaded spans; missing usage stays unknown.
        </p>
      )}
      {evidence.latestMissing && (
        <p role="status" {...stylex.props(styles.notice)}>
          Viewing an older window. Refresh keeps this position.
        </p>
      )}
      <div {...stylex.props(styles.toolbar)}>
        <Button
          variant="ghost"
          disabled={!connected || busy}
          onClick={() => void action(() => view.refresh())}
        >
          Refresh trace
        </Button>
        {evidence.olderCursor !== null && (
          <Button
            variant="ghost"
            disabled={!connected || busy}
            onClick={() => void action(() => view.loadOlder())}
          >
            Older spans
          </Button>
        )}
        {evidence.latestMissing && (
          <Button
            variant="ghost"
            disabled={!connected || busy}
            onClick={() => void action(() => view.latest())}
          >
            Latest spans
          </Button>
        )}
        {evidence.olderRootsCursor !== null && (
          <Button
            variant="ghost"
            disabled={!connected || busy}
            onClick={() => void action(() => view.loadOlderRoots())}
          >
            Older traces
          </Button>
        )}
        {evidence.rootsBefore !== null && (
          <Button
            variant="ghost"
            disabled={!connected || busy}
            onClick={() => void action(() => view.latestRoots())}
          >
            Latest traces
          </Button>
        )}
        <Button
          variant="ghost"
          disabled={!connected || busy || evidence.revision === null || evidence.status !== 'live'}
          onClick={() => void action(exportTrace)}
        >
          Export trace
        </Button>
      </div>
      {evidence.error && (
        <ErrorNotice
          type="resource"
          owner={`${viewId}:trace`}
          error={evidence.error.message}
          action={
            <Button
              variant="ghost"
              disabled={!connected || busy}
              onClick={() => void action(() => view.refresh())}
            >
              Retry
            </Button>
          }
        />
      )}
      {actionError && (
        <ErrorNotice type="action" owner={`${viewId}:trace-action`} error={actionError} />
      )}
      <div ref={bodyRef} {...stylex.props(styles.body)}>
        {spans.length ? (
          <div
            ref={listRef}
            role="tree"
            aria-label="Trace spans"
            tabIndex={0}
            {...stylex.props(styles.list)}
          >
            <div {...stylex.props(styles.columns)}>
              {panes.tree && (
                <span
                  {...stylex.props(styles.columnLabel)}
                  style={{ width: panes.timeline ? treeWidth : '100%' }}
                >
                  Execution
                </span>
              )}
              {panes.timeline && (
                <div {...stylex.props(styles.axis)} aria-hidden="true">
                  {niceTicks(timeline, laneWidth).map((tick) => (
                    <span
                      key={tick}
                      {...stylex.props(styles.tick)}
                      style={{ left: xForTime(tick, timeline, laneWidth) }}
                    >
                      {formatTick(tick)}
                    </span>
                  ))}
                </div>
              )}
            </div>
            <div {...stylex.props(styles.rows)} style={{ height: virtual.getTotalSize() }}>
              {virtual.getVirtualItems().map((item) => {
                const node = rows[item.index]!;
                const span = node.span;
                const category = spanCategory(span);
                const open = isSpanInFlight(span);
                const start = xForTime(rel(span.startMs), timeline, laneWidth);
                const end = xForTime(rel(spanEndMs(span, now)), timeline, laneWidth);
                const width = Math.max(MIN_BAR_PX, end - start);
                const durationLabel = open ? span.status : elapsedLabel(span, now);
                return (
                  <div
                    key={item.key}
                    role="treeitem"
                    aria-level={node.depth + 1}
                    aria-selected={span.id === selectedId}
                    tabIndex={-1}
                    aria-expanded={node.children.length ? !collapsed.has(span.id) : undefined}
                    aria-label={`${spanDisplayName(span)} · ${durationLabel}`}
                    {...stylex.props(styles.row, span.id === selectedId && styles.rowSelected)}
                    style={{ top: item.start, height: ROW_HEIGHT }}
                    onClick={() => {
                      setSelectedId(span.id);
                      setPanes((current) =>
                        current.details ? current : { ...current, details: true },
                      );
                    }}
                    onDoubleClick={() =>
                      setZoom(
                        spanZoomView(rel(span.startMs), rel(spanEndMs(span, now)), domain.dur),
                      )
                    }
                  >
                    {panes.tree && (
                      <div
                        {...stylex.props(styles.tree)}
                        style={{
                          width: panes.timeline ? treeWidth : '100%',
                          paddingLeft: 8 + node.depth * 14,
                        }}
                      >
                        {node.children.length ? (
                          <button
                            type="button"
                            {...stylex.props(styles.toggle)}
                            aria-label={collapsed.has(span.id) ? 'Expand' : 'Collapse'}
                            onClick={(event) => {
                              event.stopPropagation();
                              setCollapsed((current) => {
                                const next = new Set(current);
                                if (next.has(span.id)) next.delete(span.id);
                                else next.add(span.id);
                                return next;
                              });
                            }}
                          >
                            {collapsed.has(span.id) ? (
                              <ChevronRight size={12} />
                            ) : (
                              <ChevronDown size={12} />
                            )}
                          </button>
                        ) : (
                          <span {...stylex.props(styles.toggleSpacer)} />
                        )}
                        <span aria-hidden="true" {...stylex.props(styles.dot, styles[category])} />
                        <span
                          {...stylex.props(
                            styles.label,
                            span.status === 'failed' && styles.labelMuted,
                          )}
                          title={spanDisplayName(span)}
                        >
                          {spanDisplayName(span)}
                        </span>
                        <span {...stylex.props(styles.duration)}>{durationLabel}</span>
                      </div>
                    )}
                    {panes.timeline && (
                      <div {...stylex.props(styles.lane)}>
                        <div
                          {...stylex.props(
                            styles.bar,
                            styles[category],
                            open && styles.barRunning,
                            span.status === 'failed' && styles.barError,
                          )}
                          style={{ left: start, width }}
                          title={`${spanDisplayName(span)} · ${durationLabel}`}
                        >
                          {width > LABEL_MIN_WIDTH && spanDisplayName(span)}
                        </div>
                      </div>
                    )}
                  </div>
                );
              })}
            </div>
          </div>
        ) : (
          <div {...stylex.props(styles.empty)}>
            <strong>
              {loading
                ? 'Loading trace…'
                : evidence.revision === null || evidence.error
                  ? 'Trace unavailable'
                  : 'No recorded spans'}
            </strong>
            <span>
              {loading
                ? 'Reading the session’s recorded spans.'
                : evidence.error
                  ? 'Recorded spans could not be loaded. Retry to read them.'
                  : evidence.revision === null
                    ? connected
                      ? 'Recorded spans have not been loaded.'
                      : 'Reconnect to read the recorded spans.'
                    : 'This conversation has no recorded span data. Older conversations may predate tracing; historical timings are not reconstructed.'}
            </span>
          </div>
        )}
        {hasSpans && panes.tree && panes.timeline && (
          <TraceResizeHandle
            label="Resize tree and timeline"
            width={treeWidth}
            min={treeMin}
            max={treeMax}
            defaultWidth={TREE_WIDTH}
            onResize={(tree) => setWidths((current) => ({ ...current, tree }))}
          />
        )}
        {showDetails && (
          <TraceResizeHandle
            label="Resize details"
            width={detailsWidth}
            min={detailsMin}
            max={detailsMax}
            defaultWidth={DETAILS_WIDTH}
            reverse
            onResize={(details) => setWidths((current) => ({ ...current, details }))}
          />
        )}
        {showDetails && (
          <SpanDetails
            client={client}
            connected={connected}
            width={detailsWidth}
            node={selected}
            domainStartMs={domain.startMs}
            now={now}
          />
        )}
      </div>
    </div>
  );
}

function Total({ label, value }: { label: string; value: string }) {
  return (
    <span {...stylex.props(styles.total)}>
      <span {...stylex.props(styles.totalLabel)}>{label}</span>
      <span {...stylex.props(styles.totalValue)}>{value}</span>
    </span>
  );
}

function SpanDetails({
  client,
  connected,
  width,
  node,
  domainStartMs,
  now,
}: {
  client: Client;
  connected: boolean;
  width: number;
  node?: TraceNode;
  domainStartMs: number;
  now: number;
}) {
  const [raw, setRaw] = useState(false);
  const sums = useMemo(() => (node ? rollup(node) : undefined), [node]);
  if (!node)
    return (
      <aside style={{ width }} aria-label="Span details" {...stylex.props(styles.details)}>
        <span {...stylex.props(styles.sectionLabel)}>Details</span>
        <p {...stylex.props(styles.notice)}>Select a span to inspect it.</p>
      </aside>
    );
  const span = node.span,
    attrs = span.attrs,
    group = span.kind === 'agent';
  const stats: [string, string][] = [
    ['duration', elapsedLabel(span, now)],
    ['start', `+${formatDuration(span.startMs - domainStartMs)}`],
    [
      'end',
      span.endMs === null
        ? 'Not finished'
        : span.endMs < span.startMs
          ? 'Clock mismatch'
          : `+${formatDuration(span.endMs - domainStartMs)}`,
    ],
    [
      group ? 'cost (loaded descendants)' : 'cost',
      known(sums!.costNanoUSD, sums!.missingCost, formatNanoUSD),
    ],
    ['tokens in', known(sums!.promptTokens, sums!.missingInput)],
    ['tokens out', known(sums!.completionTokens, sums!.missingOutput)],
    ...(span.kind === 'llm'
      ? ([
          ['model', text(attrs['gen_ai.request.model']) || span.name],
          [
            'cost source',
            attrs['whip.cost.source'] === 'provider'
              ? 'Provider reported'
              : attrs['whip.cost.source'] === 'prices'
                ? 'Catalog estimate'
                : 'Unknown',
          ],
        ] as [string, string][])
      : []),
    ...(group ? [['loaded descendants', String(sums!.descendants)] as [string, string]] : []),
    ['session', span.sessionId],
    ['turn', span.turnId],
    ['source', `${span.record.source_kind} · ${span.record.source_id}`],
    ['status', span.status],
  ];
  const bodies: [string, string][] = (
    [
      ['Arguments preview', 'whip.arguments.preview'],
      ['Result preview', 'whip.result.preview'],
      ['Error preview', 'error.preview'],
    ] as const
  ).flatMap(([label, key]) =>
    text(attrs[key]) ? [[label, text(attrs[key])] as [string, string]] : [],
  );
  return (
    <aside style={{ width }} aria-label="Span details" {...stylex.props(styles.details)}>
      <div {...stylex.props(styles.detailsHeader)}>
        <span aria-hidden="true" {...stylex.props(styles.dot, styles[spanCategory(span)])} />
        <span {...stylex.props(styles.detailsTitle)}>{spanDisplayName(span)}</span>
        <Badge tone={statusTone(span)}>{span.kind}</Badge>
        <ToggleGroup
          label="Detail mode"
          size="sm"
          value={[raw ? 'raw' : 'overview']}
          items={[
            { value: 'overview', label: 'Overview' },
            { value: 'raw', label: 'Raw' },
          ]}
          onValueChange={([value]) => {
            if (value) setRaw(value === 'raw');
          }}
        />
      </div>
      {raw ? (
        <CodeBlock
          code={JSON.stringify(span.record, null, 2)}
          language="json"
          label="Raw span"
          xstyle={styles.code}
        />
      ) : (
        <>
          <dl {...stylex.props(styles.stats)}>
            {stats.map(([label, value]) => (
              <div key={label} {...stylex.props(styles.stat)}>
                <dt {...stylex.props(styles.statLabel)}>{label}</dt>
                <dd {...stylex.props(styles.statValue)}>{value}</dd>
              </div>
            ))}
          </dl>
          {bodies.map(([label, value]) => (
            <div key={label} {...stylex.props(styles.section)}>
              <CodeBlock code={value} label={label} xstyle={styles.code} />
            </div>
          ))}
          {(attrs['whip.arguments.truncated'] === true ||
            attrs['whip.result.truncated'] === true) && (
            <p {...stylex.props(styles.notice)}>
              The recorded preview is truncated. Export reads the retained canonical body when
              available.
            </p>
          )}
          {attrs['whip.input.body_available'] === false && !attrs['whip.instructions.status'] && (
            <p {...stylex.props(styles.notice)}>
              Historical model request bodies were not retained. The request digest identifies the
              captured request; its prompt is unavailable.
            </p>
          )}
          {span.record.source_kind === 'attempt' && (
            <ModelRequestInspection
              key={`${client.runtimeID}:${client.processEpoch}:${span.sessionId}:${span.record.source_id}`}
              client={client}
              sessionID={span.sessionId}
              attemptID={span.record.source_id}
              connected={connected}
            />
          )}
          {!bodies.length && span.kind !== 'llm' && (
            <p {...stylex.props(styles.notice)}>
              This span has no recorded excerpt. Export includes only available canonical bodies.
            </p>
          )}
        </>
      )}
    </aside>
  );
}
