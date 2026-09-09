import type { ExecutionCell, ExecutionRow } from '@whip/sdk/state';
import type { TimelineRow } from './conversation-rows';

export interface ActivityGroup extends TimelineRow {
  role: 'activity';
  cells: readonly ExecutionCell[];
  updates?: readonly TimelineRow[];
  /** Reading bookmarks can address any folded member, including a former group. */
  memberIds: readonly string[];
  memberSeqs: readonly number[];
}
export type ConversationActivityRow = TimelineRow | ActivityGroup;
export const isActivityGroup = (row: TimelineRow): row is ActivityGroup => 'cells' in row;
export const executionActive = (cell: ExecutionCell) => cell.status === 'running' || cell.status === 'writing';

/** One footer for a response to an authored input. Internal deliveries and tool
 * steps remain within that response; queued input does not finish active work.
 * This is a view of retained prose, not a second turn/event store.
 */
export function responseCopies(rows: readonly ConversationActivityRow[], active: boolean, hasEarlier = false): ReadonlyMap<string, { text: string; label: string }> {
  const copies = new Map<string, { text: string; label: string }>();
  let prose: string[] = [];
  let last: string | undefined;
  let incomplete = hasEarlier;
  let bytes = 0;
  const finish = () => {
    if (last && prose.length) copies.set(last, { text: prose.join('\n\n'), label: incomplete ? 'Copy visible response' : 'Copy response' });
    prose = [];
    last = undefined;
    bytes = 0;
    incomplete = false;
  };
  for (const row of rows) {
    if (row.queued) continue;
    if (row.role === 'user') { finish(); continue; }
    last = row.id;
    if (row.role !== 'assistant') continue;
    if (row.body) incomplete = true;
    if (!row.text.trim()) continue;
    // Bound derived copy strings as well as the underlying SDK window.
    if (bytes + row.text.length > 256 * 1024) { incomplete = true; continue; }
    prose.push(row.text);
    bytes += row.text.length;
  }
  if (!active) finish();
  return copies;
}

/** Only typed host evidence supplies an operation label; never guess from code. */
export function operationLabel(name: string): string {
  const labels: Record<string, string> = {
    'files.read': 'Reading files', 'files.list': 'Exploring files', 'files.search': 'Searching files',
    'files.write': 'Writing files', 'files.patch': 'Editing files',
    'shell.run': 'Running a command', 'shell.start': 'Starting a command', 'shell.wait': 'Waiting for a command',
    'agents.spawn': 'Starting an agent', 'agents.wait': 'Waiting for agents',
    'agents.inspect': 'Checking an agent', 'agents.list': 'Checking agents',
    'models.call': 'Calling a model', 'models.batch': 'Calling models',
  };
  if (labels[name]) return labels[name];
  if (name.startsWith('browser.')) return 'Using the browser';
  if (name.startsWith('computer.')) return 'Using the computer';
  return name || 'Running Starlark';
}

export function cellActivityLabel(cell: ExecutionCell): string {
  const active = cell.hosts.filter(host => host.status === 'running').at(-1);
  return active ? operationLabel(active.name) : cell.status === 'writing' ? 'Preparing an execution' : 'Running Starlark';
}

/** Fold adjacent executions without moving authored prose or decoding tool results twice.
 * Previous groups are only identity hints for the retained reading window, not session state.
 */
export function conversationActivityRows(
  rows: readonly TimelineRow[], executions: readonly ExecutionRow[], previous: readonly ActivityGroup[] = [],
): ConversationActivityRow[] {
  const cells = executions.filter((row): row is ExecutionCell => row.kind === 'cell');
  const recorded = new Map(cells.filter(cell => cell.seq !== undefined).map(cell => [JSON.stringify([cell.seq, cell.callId]), cell]));
  const observed = new Map(cells.flatMap(cell => (cell.presentationSeqs ?? (cell.eventSeq ? [cell.eventSeq] : [])).map(seq => [seq, cell] as const)));
  const used = new Set<string>();
  const reused = new Set<string>();
  const priorByMember = new Map(previous.flatMap(group => [...group.cells, ...(group.updates ?? [])].map(item => [item.id, group] as const)));
  const output: ConversationActivityRow[] = [];
  let pending: { row: TimelineRow; cell?: ExecutionCell }[] = [];
  const flush = () => {
    if (!pending.length) return;
    const first = pending[0]!;
    const overlapping = [...new Set(pending.map(item => priorByMember.get(item.cell?.id ?? item.row.id)).filter((group): group is ActivityGroup => !!group))];
    const prior = overlapping.find(group => !reused.has(group.id));
    const id = prior?.id ?? `activity:${first.cell?.id ?? first.row.id}`;
    reused.add(id);
    const members = new Set(pending.flatMap(({ row, cell }) => cell ? [row.id, cell.id] : [row.id]));
    for (const group of overlapping) {
      members.add(group.id);
      for (const alias of group.memberIds) if (members.size < 512) members.add(alias);
    }
    const groupedCells = pending.flatMap(item => item.cell ? [item.cell] : []);
    output.push({ id, role: 'activity', text: '', seq: first.row.seq, cells: groupedCells,
      updates: pending.filter(item => !item.cell).map(item => item.row),
      memberIds: [...members], memberSeqs: pending.flatMap(item => item.row.seq === undefined ? [] : [item.row.seq]),
      live: groupedCells.some(executionActive), turnId: groupedCells[0]?.turnId });
    pending = [];
  };
  const restarts = executions.filter(row => row.kind === 'restart');
  for (const row of rows) {
    if (row.role === 'mailbox') {
      // A digest starts a new delivery boundary; keep it with the following work,
      // never with earlier work or across authored prose. Bound expanded updates.
      if (pending.some(item => item.cell) || pending.length === 6) flush();
      pending.push({ row });
      continue;
    }
    let cell: ExecutionCell | undefined;
    if (row.role === 'tool' && row.toolName === 'rlm_exec' && row.callId) {
      cell = row.seq !== undefined ? recorded.get(JSON.stringify([row.seq, row.callId])) : observed.get(row.eventSeq ?? '');
      if (!cell && row.seq === undefined) {
        const candidates = cells.filter(item => !used.has(item.id) && item.callId === row.callId && item.seq === undefined
          && (!row.turnId || !item.turnId || row.turnId === item.turnId));
        if (candidates.length === 1) cell = candidates[0];
      }
      if (cell && used.has(cell.id)) cell = undefined;
    }
    if (!cell) { flush(); output.push(row); continue; }
    const last = pending.at(-1);
    if (last?.cell && (last.cell.turnId !== cell.turnId || last.row.activityBoundary !== row.activityBoundary
      || last.cell.status === 'failed' || last.cell.status === 'cancelled' || last.cell.status === 'interrupted'
      || cell.historyUnmatched || last.cell.historyUnmatched
      || restarts.some(restart => restart.seq !== undefined && last.row.seq !== undefined && row.seq !== undefined
        && restart.seq >= last.row.seq && restart.seq <= row.seq))) flush();
    used.add(cell.id);
    pending.push({ row, cell });
  }
  flush();
  // Lost presentation prefixes can leave live evidence without a transcript row.
  // Keep that evidence explicitly separate; do not guess where old observations belong.
  for (const cell of cells.filter(cell => !used.has(cell.id) && cell.seq === undefined)) {
    output.push({ id: `activity:${cell.id}`, role: 'activity', text: '', cells: [cell],
      memberIds: [cell.id], memberSeqs: [], live: executionActive(cell) });
  }
  return output;
}
