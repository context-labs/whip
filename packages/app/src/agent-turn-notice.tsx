import { useEffect, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Alert, Collapsible, CopyButton } from '@whip/ui';
import * as stylex from '@stylexjs/stylex';
import { scale, surface, typography } from '@whip/ui/tokens.stylex';
import { useRuntime } from './context';
import { ErrorNotice } from './error-feedback';
import type { Session, SessionRecord, Turn } from '@whip/sdk';
import type { DeepReadonly } from '@whip/sdk/state';

/** Canonical metadata for the selected native session, independent of transcript paging. */
export function useSelectedAgent(session: Session, connected: boolean) {
  return useQuery({
    queryKey: ['selected-agent', session.client.runtimeID, session.client.processEpoch, session.id],
    queryFn: ({ signal }) => session.get({ signal }),
    enabled: connected, gcTime: 0, retry: false, refetchInterval: connected ? 3000 : false,
  });
}

export function AgentTurnNotice({ session, selected, turn, activeTurn, connected = true }: {
  session: Session; selected?: DeepReadonly<SessionRecord>; turn?: DeepReadonly<Turn>; activeTurn?: string; connected?: boolean;
}) {
  const runtime = useRuntime();
  const [copyError, setCopyError] = useState<unknown>();
  useEffect(() => setCopyError(undefined), [session.id, turn?.id]);
  const visible = !!turn && turn.session_id === session.id && selected?.id === session.id && !activeTurn && ['failed', 'cancelled', 'interrupted'].includes(turn.state);
  const definition = useQuery({
    queryKey: ['turn-notice-definition', session.client.runtimeID, session.client.processEpoch, selected?.definition.id, selected?.definition.revision],
    queryFn: async ({ signal }) => {
      if (!selected) throw new Error('The selected session is unavailable.');
      const value = await session.client.agents.get(selected.definition, { signal });
      if (value.ref.id !== selected.definition.id || value.ref.revision !== selected.definition.revision) throw new Error('The definition identity changed.');
      return value;
    },
    enabled: connected && visible && selected?.parent_id !== null,
    staleTime: Infinity, gcTime: 0, retry: false,
  });
  if (!visible || !turn || !selected) return null;
  const label = turn.state === 'failed' ? 'Last turn failed' : turn.state === 'cancelled' ? 'Last turn cancelled' : 'Last turn interrupted';
  const name = selected.parent_id === null ? 'Root agent' : definition.data?.document.name.trim() || selected.definition.id;
  const model = selected.config_revision === turn.config_revision ? selected.configuration.model : null;
  return <div {...stylex.props(styles.container)} data-agent-turn-outcome={turn.state} data-error-type="turn" data-error-owner={`${session.client.runtimeID}:${session.id}:${turn.id}`}>
    <Alert tone={turn.state === 'failed' ? 'error' : 'neutral'} title={`${name} · ${label}`}
      action={turn.failure && <CopyButton label="Copy error" text={turn.failure} copy={async text => { await runtime.platform.copy(text); setCopyError(undefined); }} onError={setCopyError} />}>
      <div {...stylex.props(styles.body)}>
        {model && <span {...stylex.props(styles.meta)}>{[model.name, model.provider].join(' · ')}</span>}
        {turn.failure ? <Collapsible key={`${session.id}:${turn.id}`} title="Error details">
          <pre {...stylex.props(styles.error)}>{turn.failure}</pre>
        </Collapsible> : <span>No error details were recorded.</span>}
        <ErrorNotice type="action" owner={`${session.id}:copy-error`} error={copyError} title="Could not copy error" />
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
