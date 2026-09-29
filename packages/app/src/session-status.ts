import type { TreeSummariesResult } from '@whip/protocol';

export type SessionNavigationSummary = TreeSummariesResult['items'][number];

export function sessionBusy(item?: SessionNavigationSummary, stale = false): boolean {
  if (!item || stale) return false;
  return BigInt(item.activity.active_turn_count) > 0n || BigInt(item.activity.queued_input_count) > 0n || BigInt(item.activity.active_workspace_action_count) > 0n;
}
export function sessionNeedsInput(item?: SessionNavigationSummary, stale = false): boolean {
  if (!item || stale) return false;
  return BigInt(item.activity.pending_permission_count) + BigInt(item.activity.pending_question_count) > 0n;
}
export function summaryDescription(item?: SessionNavigationSummary, stale = false, missing = false) {
  if (stale) return 'Activity unavailable';
  if (missing) return 'Session unavailable';
  if (!item) return 'Activity unavailable';
  const activity = item.activity;
  const needs = BigInt(activity.pending_permission_count) + BigInt(activity.pending_question_count);
  if (needs > 0n) return `${needs} ${needs === 1n ? 'request needs' : 'requests need'} your input`;
  if (sessionBusy(item)) return `${activity.active_turn_count} active turns · ${activity.queued_input_count} queued inputs${BigInt(activity.active_workspace_action_count) > 0n ? ` · ${activity.active_workspace_action_count} workspace actions` : ''}`;
  return 'No currently observed activity';
}
