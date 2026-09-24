import type { ExecutionCell, ExecutionHostCall, ExecutionRow } from '@whip/sdk/state';
import type { TimelineRow } from './conversation-rows';

export interface ActivityGroup extends TimelineRow {
  role: 'activity';
  items?: readonly ActivityItem[];
  autoOpen?: boolean;
  cells: readonly ExecutionCell[];
  updates?: readonly TimelineRow[];
  /** Reading bookmarks can address any folded member, including a former group. */
  memberIds: readonly string[];
  memberSeqs: readonly number[];
}
export interface ActivityItem { id: string; kind: 'reasoning' | 'mailbox' | 'operation' | 'execution'; row?: TimelineRow; cell?: ExecutionCell; host?: ExecutionHostCall }
export interface AgentActivityRow extends TimelineRow { role: 'agent-activity'; agentHost: ExecutionHostCall; cell: ExecutionCell }
export type ConversationActivityRow = TimelineRow | ActivityGroup | AgentActivityRow;
export const isAgentActivity = (row: TimelineRow): row is AgentActivityRow => 'agentHost' in row;
export const activityItems = (group: ActivityGroup): readonly ActivityItem[] => group.items ?? [
  ...(group.updates ?? []).map(row => ({ id: row.id, kind: 'mailbox' as const, row })),
  ...group.cells.flatMap<ActivityItem>(cell => cell.hosts.length ? cell.hosts.map(host => ({ id: host.id, kind: 'operation' as const, cell, host })) : [{ id: cell.id, kind: 'execution' as const, cell }]),
];
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
    if (row.historyGap) { incomplete = true; finish(); incomplete = true; continue; }
    if (row.role === 'user') { finish(); continue; }
    last = row.id;
    if (row.role !== 'assistant') continue;
    if (row.body) incomplete = true;
    const source = row.copyText ?? row.text;
    if (!source.trim()) continue;
    // Bound derived copy strings as well as the underlying SDK window.
    if (bytes + source.length > 256 * 1024) { incomplete = true; continue; }
    prose.push(source);
    bytes += source.length;
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
  return name || 'Running an execution';
}

export function cellActivityLabel(cell: ExecutionCell): string {
  const active = cell.hosts.filter(host => host.status === 'running').at(-1);
  return active ? operationLabel(active.name) : cell.status === 'writing' ? 'Preparing an execution' : cell.language === 'javascript' ? 'Running JavaScript' : 'Running Starlark';
}

/** A pure display projection; typed SDK evidence owns operation reconciliation. */
export function conversationActivityRows(
  rows: readonly TimelineRow[], executions: readonly ExecutionRow[], previous: readonly ActivityGroup[] = [],
  activeTurnId?: string,
): ConversationActivityRow[] {
  const cells = executions.filter((row): row is ExecutionCell => row.kind === 'cell');
  const recorded = new Map(cells.filter(cell => cell.seq !== undefined).map(cell => [JSON.stringify([cell.seq, cell.callId]), cell]));
  const parts = new Map(cells.filter(cell => cell.partId).map(cell => [cell.partId, cell]));
  const observed = new Map(cells.flatMap(cell => (cell.presentationSeqs ?? (cell.eventSeq ? [cell.eventSeq] : [])).map(seq => [seq, cell] as const)));
  const used = new Set<string>();
  const reused = new Set<string>();
  const priorByMember = new Map(previous.flatMap(group => [...activityItems(group).map(item => item.id), ...group.cells.map(cell => cell.id)].map(id => [id, group] as const)));
  const output: ConversationActivityRow[] = [];
  let pending: ActivityItem[] = [];
  let members: TimelineRow[] = [];
  const flush = () => {
    if (!pending.length) return;
    const overlaps = [...new Set(pending.map(item => priorByMember.get(item.id) ?? priorByMember.get(item.cell?.id ?? '')).filter((group): group is ActivityGroup => !!group))];
    const prior = overlaps.find(group => !reused.has(group.id));
    const id = prior?.id ?? `activity:${pending[0]!.id}`;
    reused.add(id);
    const aliases = new Set([id, ...pending.map(item => item.id), ...members.flatMap(row => [row.id, ...(row.memberIds ?? [])])]);
    for (const group of overlaps) for (const alias of [group.id, ...group.memberIds]) if (aliases.size < 512) aliases.add(alias);
    const groupedCells = [...new Map(pending.flatMap(item => item.cell ? [[item.cell.id, item.cell] as const] : [])).values()];
    output.push({ id, role: 'activity', text: '', seq: members[0]?.seq, cells: groupedCells, items: pending,
      updates: pending.flatMap(item => item.kind === 'mailbox' && item.row ? [item.row] : []),
      memberIds: [...aliases].slice(0, 512), memberSeqs: [...new Set(members.flatMap(row => row.seq === undefined ? [] : [row.seq]))],
      live: pending.some(item => item.cell ? executionActive(item.cell) : item.row?.live), turnId: members.find(row => row.turnId)?.turnId ?? groupedCells[0]?.turnId });
    pending = []; members = [];
  };
  const restarts = executions.filter(row => row.kind === 'restart');
  const append = (item: ActivityItem, row: TimelineRow) => {
    const last = members.at(-1);
    const priorTurn = last?.turnId ?? pending.at(-1)?.cell?.turnId;
    const nextTurn = row.turnId ?? item.cell?.turnId;
    if (last && ((priorTurn !== nextTurn) || last.activityBoundary !== row.activityBoundary
      || item.cell?.historyUnmatched || pending.at(-1)?.cell?.historyUnmatched
      || restarts.some(restart => restart.seq !== undefined && last.seq !== undefined && row.seq !== undefined && restart.seq >= last.seq && restart.seq <= row.seq))) flush();
    pending.push(item); members.push(row);
  };
  const addCell = (cell: ExecutionCell, row: TimelineRow) => {
    used.add(cell.id);
    if (!cell.hosts.length) append({ id: cell.id, kind: 'execution', cell }, row);
    for (const host of cell.hosts) {
      if (host.name === 'agents.spawn') {
        flush();
        output.push({ ...row, id: `agent:${host.id}`, role: 'agent-activity', text: '', agentHost: host, cell, memberIds: [row.id, cell.id] });
      } else append({ id: host.id, kind: 'operation', cell, host }, row);
    }
    if (cell.hosts.length && ['failed', 'cancelled', 'interrupted'].includes(cell.status) && !cell.hosts.some(host => host.status === cell.status))
      append({ id: `${cell.id}:outcome`, kind: 'execution', cell }, row);
  };
  for (const row of rows) {
    if (row.role === 'mailbox') {
      if (pending.some(item => item.cell || item.kind === 'reasoning') || pending.length === 6) flush();
      append({ id: row.id, kind: 'mailbox', row }, row); continue;
    }
    if (row.role === 'reasoning') { append({ id: row.id, kind: 'reasoning', row }, row); continue; }
    let cell: ExecutionCell | undefined;
    if (row.role === 'tool' && row.toolName === 'rlm_exec' && row.callId) {
      cell = row.partId ? parts.get(row.partId) : undefined;
      cell ??= row.seq !== undefined ? recorded.get(JSON.stringify([row.seq, row.callId])) : observed.get(row.eventSeq ?? '');
      if (!cell && row.seq === undefined) {
        const candidates = cells.filter(item => !used.has(item.id) && item.callId === row.callId && item.seq === undefined && (!row.turnId || !item.turnId || row.turnId === item.turnId));
        if (candidates.length === 1) cell = candidates[0];
      }
      if (cell && used.has(cell.id)) cell = undefined;
    }
    if (cell) addCell(cell, row);
    else { flush(); output.push(row); }
  }
  flush();
  // A missing live prefix may leave current work without a transcript row.
  // Older unplaced evidence belongs in the REPL, never under a later response.
  for (const cell of cells.filter(cell => activeTurnId && cell.turnId === activeTurnId && !used.has(cell.id) && cell.seq === undefined)) {
    addCell(cell, { id: cell.id, role: 'tool', text: '', turnId: cell.turnId }); flush();
  }
  const tail = [...output].reverse().find(row => !row.queued);
  if (tail && isActivityGroup(tail)) tail.autoOpen = true;
  return output;
}

export function activitySummary(group: ActivityGroup): string {
  const counts = { commands: 0, reads: 0, searches: 0, fetches: 0, edits: 0, other: 0, executions: 0, failed: 0 };
  const files = new Set<string>();
  let thought = false;
  for (const item of activityItems(group)) {
    if (item.kind === 'reasoning') { thought = true; continue; }
    if (item.kind === 'execution') { if (!item.cell?.hosts.length) counts.executions++; if (item.cell?.status === 'failed') counts.failed++; continue; }
    const host = item.host;
    if (!host) continue;
    if (host.status === 'failed') counts.failed++;
    if (['shell.run', 'shell.start'].includes(host.name)) counts.commands++;
    else if (host.name === 'files.read') counts.reads++;
    else if (['files.list', 'files.search', 'browser.search'].includes(host.name)) counts.searches++;
    else if (['browser.fetch', 'web.fetch'].includes(host.name)) counts.fetches++;
    else if (['files.write', 'files.patch'].includes(host.name)) { if (host.display?.target) files.add(host.display.target); else counts.edits++; }
    else counts.other++;
  }
  const plural = (n: number, one: string, many = `${one}s`) => `${n} ${n === 1 ? one : many}`;
  const labels = [thought ? 'Thought' : '', counts.commands ? `ran ${plural(counts.commands, 'command')}` : '', files.size ? `edited ${plural(files.size, 'file')}` : '',
    counts.edits ? `made ${plural(counts.edits, 'edit')}` : '', counts.reads ? `read ${plural(counts.reads, 'file')}` : '', counts.searches ? `searched ${plural(counts.searches, 'time')}` : '',
    counts.fetches ? `fetched ${plural(counts.fetches, 'page')}` : '', counts.other ? `called ${plural(counts.other, 'tool')}` : '', counts.executions ? plural(counts.executions, 'execution') : '', counts.failed ? `${counts.failed} failed` : ''].filter(Boolean);
  const text = labels.join(' · ') || 'Agent updates';
  return `${group.cells.some(cell => cell.truncated || cell.historyUnmatched) || activityItems(group).some(item => item.row?.truncated) ? 'Partial activity · ' : ''}${text[0]!.toUpperCase()}${text.slice(1)}`;
}
