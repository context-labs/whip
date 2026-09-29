import type { ContextUsage, HistorySnapshot, TurnUsage } from '@whip/protocol';

/** Formatting only: readings remain canonical and are never summed from UI rows. */
export const exactCount = (value: string) => BigInt(value).toString().replace(/\B(?=(\d{3})+(?!\d))/g, ',');
const cost = (value: string) => {
  const amount = BigInt(value);
  return `$${amount / 1000000000n}.${(amount % 1000000000n).toString().padStart(9, '0')}`;
};
export function contextUsageLines(value: ContextUsage, sessionID: string, configRevision: string, history: Readonly<HistorySnapshot> | null): string[] {
  if (value.session_id !== sessionID) return ['Context reading belongs to another session.'];
  const reason = value.config_revision !== configRevision ? 'configuration_changed'
    : history && value.history_revision !== history.revision ? 'history_changed' : value.unavailable_reason;
  if (reason || !value.prefill) {
    return [reason === 'configuration_changed' ? 'Context usage is unknown after configuration changed.'
      : reason === 'history_changed' ? 'Context usage is unknown after history changed.'
      : reason === 'selection_changed' ? 'Context usage is unknown after the selected context changed.'
      : 'No captured context prefill is available.'];
  }
  const prefill = value.prefill;
  const stale = prefill.stale || !!history && BigInt(history.through_sequence) > BigInt(prefill.through_sequence);
  return [
    `${prefill.input_source === 'reported' ? 'Provider-reported' : 'Estimated'} input: ${exactCount(prefill.input_tokens)} tokens.`,
    prefill.context_window_tokens === null ? 'Model context capacity is unknown.' : `Captured model capacity: ${exactCount(prefill.context_window_tokens)} tokens.`,
    ...(prefill.context_window_tokens === null ? [] : [`Latest prefill uses ${BigInt(prefill.input_tokens) * 1000n / BigInt(prefill.context_window_tokens) / 10n}.${BigInt(prefill.input_tokens) * 1000n / BigInt(prefill.context_window_tokens) % 10n}% of captured capacity.`]),
    `Captured through message ${prefill.through_sequence} · ${prefill.model.provider}/${prefill.model.name}.`,
    stale ? 'Stale prefill: the transcript has advanced since this request.' : 'Latest prefill at the captured transcript tail.',
    'This is the last request’s input, not the current full context or cumulative token usage.',
  ];
}
export function turnUsageLines(value: TurnUsage): string[] {
  const { usage, compaction_attempts: folds } = value, attempts = usage.attempts;
  return [
    `${exactCount(attempts.settled)} settled model attempts · ${exactCount(attempts.in_flight)} in flight · ${exactCount(attempts.reserved)} reserved · ${exactCount(attempts.not_dispatched)} not dispatched.`,
    `${exactCount(attempts.uncertain)} settled attempts have an uncertain outcome.`,
    `${exactCount(value.compactions)} committed compactions · ${exactCount(folds.settled)} settled compaction attempts · ${exactCount(folds.in_flight)} in flight · ${exactCount(folds.reserved)} reserved.`,
    `${exactCount(folds.uncertain)} compaction attempts have an uncertain outcome · ${exactCount(folds.not_dispatched)} were not dispatched.`,
    `Provider-reported cost: ${usage.reported_cost.overflow ? 'at least ' : ''}${cost(usage.reported_cost.value)} from ${exactCount(usage.reported_cost.attempts)} attempts.`,
    `Catalog-estimated cost: ${usage.estimated_cost.overflow ? 'at least ' : ''}${cost(usage.estimated_cost.value)} from ${exactCount(usage.estimated_cost.attempts)} attempts.`,
    `${exactCount(usage.unknown_cost)} settled attempts have unknown cost.`,
    ...([['Input', usage.input_tokens], ['Output', usage.output_tokens]] as const).map(([name, amount]) =>
      `${name} tokens: ${amount.known_attempts === '0' ? 'not reported' : `${amount.overflow ? 'at least ' : ''}${exactCount(amount.value)}`} · reported by ${exactCount(amount.known_attempts)} attempts, missing from ${exactCount(amount.missing_attempts)}.`),
  ];
}
