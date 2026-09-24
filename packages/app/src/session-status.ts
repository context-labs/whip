import type { SessionSummariesResult } from '@whip/protocol';

export type SessionNavigationSummary = SessionSummariesResult['items'][number];

/** Busy: the session has running or queued agents. */
export function sessionBusy(item?: SessionNavigationSummary, stale = false): boolean {
  if (!item || stale || item.missing) return false;
  return BigInt(item.running_agents) > 0n || BigInt(item.queued_agents) > 0n;
}

/** Needs input: the session has pending permissions or questions. */
export function sessionNeedsInput(item?: SessionNavigationSummary, stale = false): boolean {
  if (!item || stale || item.missing) return false;
  return BigInt(item.pending_permissions) + BigInt(item.pending_questions) > 0n;
}

export function summaryDescription(item?: SessionNavigationSummary, stale = false) {
  if (!item || stale) return 'Activity unavailable';
  if (item.missing) return 'Session unavailable';
  const needs = BigInt(item.pending_permissions) + BigInt(item.pending_questions);
  if (needs > 0n) return `${needs} ${needs === 1n ? 'request needs' : 'requests need'} your input`;
  if (BigInt(item.running_agents) || BigInt(item.queued_agents)) return `${item.running_agents} running · ${item.queued_agents} queued agents`;
  return 'No currently observed activity';
}
