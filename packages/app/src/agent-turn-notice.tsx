import { useEffect, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import type { RootSnapshot } from '@whip/protocol';
import type { DeepReadonly, SessionView, SessionViewSnapshot } from '@whip/sdk/state';
import { Alert, Collapsible, CopyButton } from '@whip/ui';
import * as stylex from '@stylexjs/stylex';
import { scale, surface, typography } from '@whip/ui/tokens.stylex';
import { useRuntime } from './context';
import { ErrorNotice } from './error-feedback';
import { ContentRead } from './details/shared';

type Agent = DeepReadonly<NonNullable<RootSnapshot['agents']>[number]>;

// Most selected agents are already in the SDK snapshot/collection. A scoped,
// cancellable detail read covers selection beyond the bounded first page.
export function useSelectedAgent(view: SessionView, state: DeepReadonly<SessionViewSnapshot>, agentId: string, connected: boolean) {
  const agent = state.root?.agents?.find(item => item.id === agentId)
    ?? state.collections.agents?.items?.find(item => item.agent?.id === agentId)?.agent;
  const query = useQuery({
    queryKey: ['selected-agent', view.session.client.getSnapshot().info?.runtime_id, view.session.rootId, agentId],
    queryFn: ({ signal }) => view.session.agents.inspect(agentId, { signal }),
    enabled: connected && !agent,
    gcTime: 0,
  });
  const refetch = query.refetch;
  useEffect(() => {
    // Agent records are replaced by the SDK's coalesced lifecycle snapshot,
    // not by individual streaming deltas. Reuse that invalidation boundary.
    if (connected && !agent) void refetch();
  }, [state.root?.agents, connected, agent, refetch]);
  return agent ?? query.data?.result?.agent;
}

export function AgentTurnNotice({ agent, view, activeTurn }: { agent?: Agent; view: SessionView; activeTurn?: string }) {
  const runtime = useRuntime();
  const outcome = agent?.last_turn;
  const [copyError, setCopyError] = useState<unknown>();
  useEffect(() => setCopyError(undefined), [agent?.id, outcome?.event_seq]);
  if (!outcome || activeTurn || !['failed', 'cancelled', 'interrupted'].includes(outcome.status)) return null;
  const label = outcome.status === 'failed' ? 'Last turn failed' : outcome.status === 'cancelled' ? 'Last turn cancelled' : 'Last turn interrupted';
  return <div {...stylex.props(styles.container)} data-agent-turn-outcome={outcome.status} data-error-type="turn" data-error-owner={`${view.session.rootId}:${agent?.id}:${outcome.event_seq}`}>
    <Alert tone={outcome.status === 'failed' ? 'error' : 'neutral'} title={`${agent?.name || 'Root agent'} · ${label}`}
      action={outcome.error && <CopyButton label={outcome.error_truncated ? 'Copy error preview' : 'Copy error'} text={outcome.error} copy={async text => { await runtime.platform.copy(text); setCopyError(undefined); }} onError={setCopyError} />}>
      <div {...stylex.props(styles.body)}>
        <span {...stylex.props(styles.meta)}>{[agent?.model, agent?.provider].filter(Boolean).join(' · ')}</span>
        {outcome.error ? <Collapsible key={`${agent?.id}:${outcome.event_seq}`} title="Error details">
          <pre {...stylex.props(styles.error)}>{outcome.error}</pre>
          {outcome.error_truncated && <span {...stylex.props(styles.meta)}>Showing the beginning of the recorded error.</span>}
          {outcome.error_details && <ContentRead key={outcome.error_details.reference_id} view={view} agentId={agent?.id} value={outcome.error_details} label="Full error" />}
        </Collapsible> : !outcome.error && <span>No error details were recorded.</span>}
        <ErrorNotice type="action" owner={`${agent?.id}:copy-error`} error={copyError} title="Could not copy error" />
      </div>
    </Alert>
  </div>;
}

const styles = stylex.create({
  container: { width: '100%', maxWidth: 880, marginInline: 'auto', padding: { default: '12px 24px', [scale.phone]: '8px 12px' }, minWidth: 0, flexShrink: 0, maxHeight: '40dvh', overflowY: 'auto', overflowWrap: 'anywhere' },
  body: { minWidth: 0, display: 'flex', flexDirection: 'column', gap: 8, marginTop: 4, overflowWrap: 'anywhere', fontSize: typography.size13 },
  meta: { color: surface.secondaryText, fontSize: typography.size12 },
  error: { margin: 0, fontFamily: typography.mono, fontSize: typography.codeSize, lineHeight: 1.6, whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' },
});
