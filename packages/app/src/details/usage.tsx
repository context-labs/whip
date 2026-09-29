import { useSessionView } from '@whip/sdk/react';
import { contextUsageLines, turnUsageLines } from '../usage-presentation';
import { useQuery } from '@tanstack/react-query';
import type { Operations } from '@whip/sdk';
import { Button } from '@whip/ui';
import * as stylex from '@stylexjs/stylex';
import { layout } from '../styles';
import { formatCount, formatNanoUSD } from '../trace-math';
import { QueryFeedback, Section, type InspectorProps } from './shared';

type Usage = Operations['usage.get']['result'];

/** Whole-tree accounting reads the existing attempt ledger through the SDK;
 * selected transcript or trace windows never contribute to these totals. */
export function WholeTreeUsage(props: InspectorProps) {
  const query = useQuery({
    queryKey: ['usage', props.client.runtimeID, props.client.processEpoch, props.rootId],
    queryFn: ({ signal }) => props.client.session(props.rootId).usage({ signal }),
    enabled: props.connected,
    gcTime: 0,
    retry: false,
    refetchInterval: props.connected ? 10000 : false,
  });
  const value = query.data;
  return (
    <Section
      title="Whole-tree usage"
      description="Original model attempts across this tree, including deleted descendants. Imported fork history does not copy charges. These totals are independent of the visible transcript and trace windows."
    >
      <QueryFeedback query={query} connected={props.connected} />
      {query.error && value && <p role="status">Showing the last available totals; the latest read failed.</p>}
      <Button
        size="sm"
        variant="ghost"
        disabled={!props.connected || query.isFetching}
        onClick={() => void query.refetch()}
      >
        Refresh usage
      </Button>
      {value && (
        <>
          <p>
            {formatCount(BigInt(value.attempts.settled))} settled attempts ·{' '}
            {formatCount(BigInt(value.attempts.in_flight))} in flight ·{' '}
            {formatCount(BigInt(value.attempts.reserved))} reserved ·{' '}
            {formatCount(BigInt(value.attempts.not_dispatched))} cancelled before dispatch.
          </p>
          {value.attempts.uncertain !== '0' && (
            <p>
              {formatCount(BigInt(value.attempts.uncertain))} of the settled attempts have an
              uncertain outcome. Any reported usage is still counted.
            </p>
          )}
          <Cost label="Provider-reported cost" value={value.reported_cost} />
          <Cost label="Catalog-estimated cost" value={value.estimated_cost} />
          <p>
            {formatCount(BigInt(value.unknown_cost))} settled attempts have unknown cost. In-flight
            and reserved attempts are not included in settled cost totals.
          </p>
          <span {...stylex.props(layout.muted)}>
            Reasoning and cached tokens are reported detail fields within input/output totals; do
            not add them together.
          </span>
          {(
            [
              ['Input tokens', value.input_tokens],
              ['Output tokens', value.output_tokens],
              ['Reasoning tokens', value.reasoning_tokens],
              ['Cached input tokens', value.cached_input],
              ['Cached output tokens', value.cached_output],
              ['Model elapsed time', value.elapsed_millis],
            ] as const
          ).map(([label, amount]) => (
            <div
              key={label}
              role="group"
              aria-label={label}
              {...stylex.props(layout.column, layout.notice)}
            >
              <strong>{label}</strong>
              <span>
                {amount.known_attempts === '0'
                  ? 'Not reported'
                  : `${amount.overflow ? 'At least ' : ''}${label === 'Model elapsed time' ? `${BigInt(amount.value) / 1000n}.${(BigInt(amount.value) % 1000n).toString().padStart(3, '0')} s` : formatCount(BigInt(amount.value))}`}
              </span>
              <span>
                Reported by {formatCount(BigInt(amount.known_attempts))} settled attempts · missing
                from {formatCount(BigInt(amount.missing_attempts))}.
              </span>
              {amount.overflow && (
                <span>The sum exceeds the supported counter range; this is a lower bound.</span>
              )}
            </div>
          ))}
        </>
      )}
    </Section>
  );
}
function Cost({ label, value }: { label: string; value: Usage['reported_cost'] }) {
  return (
    <div role="group" aria-label={label} {...stylex.props(layout.column, layout.notice)}>
      <strong>{label}</strong>
      <span>
        {value.overflow ? 'At least ' : ''}
        {formatNanoUSD(BigInt(value.value))} · {formatCount(BigInt(value.attempts))} attempts
      </span>
      {value.overflow && (
        <span>The sum exceeds the supported counter range; this is a lower bound.</span>
      )}
    </div>
  );
}

/** One selected-session prefill read; transcript growth only marks that captured reading stale. */
export function ContextUsage(props: InspectorProps) {
  const observed = useSessionView(props.view);
  const connected = props.connected && observed.status === 'live';
  const query = useQuery({
    queryKey: ['context-usage', props.client.runtimeID, props.client.processEpoch, props.session.id, props.selected.config_revision, observed.history.snapshot?.revision],
    queryFn: ({ signal }) => props.session.context.usage({ signal }),
    enabled: connected, gcTime: 0, retry: false, refetchInterval: connected ? 3000 : false,
  });
  const value = connected && !query.error ? query.data : undefined;
  return <Section title="Latest context prefill" description="The selected agent’s last ordinary model request. Helper and child calls do not replace this reading.">
    <QueryFeedback query={query} connected={connected} />
    {value && contextUsageLines(value, props.session.id, props.selected.config_revision, observed.history.snapshot).map(line => <p key={line}>{line}</p>)}
    <Button size="sm" variant="ghost" disabled={!connected || query.isFetching} onClick={() => void query.refetch()}>Refresh context reading</Button>
  </Section>;
}

export function TurnUsage(props: InspectorProps & { turnID: string }) {
  const query = useQuery({
    queryKey: ['turn-usage', props.client.runtimeID, props.client.processEpoch, props.session.id, props.turnID],
    queryFn: ({ signal }) => props.session.turns.usage(props.turnID, { signal }),
    enabled: props.connected, gcTime: 0, retry: false, refetchInterval: props.connected ? 3000 : false,
  });
  return <Section title="Selected turn usage" description="Canonical attempts for this turn, including its helpers and compaction retries. Child turns and other turns are separate.">
    <code>{props.turnID}</code>
    <QueryFeedback query={query} connected={props.connected} />
    {props.connected && !query.error && query.data && turnUsageLines(query.data).map(line => <p key={line}>{line}</p>)}
    <Button size="sm" variant="ghost" disabled={!props.connected || query.isFetching} onClick={() => void query.refetch()}>Refresh turn usage</Button>
  </Section>;
}
