import type { Cell, HostOperation, Turn } from '@whip/protocol';
import { createExecutionView, cellExecutionRows, type DeepReadonly } from '../dist/state.js';
import type { SessionView } from '../dist/state.js';
import type { Session } from '../dist/index.js';

declare const session: Session;
declare const source: SessionView;
const execution = createExecutionView(session, source);
const snapshot = execution.getSnapshot();
const cells: readonly DeepReadonly<Cell>[] = snapshot.cells;
const turns: readonly DeepReadonly<Turn>[] = snapshot.turns;
const operations: readonly DeepReadonly<HostOperation>[] = snapshot.operations;
for (const row of cellExecutionRows(snapshot, source.getSnapshot().history.messages)) {
  const code: unknown = row.call?.value.arguments.code;
  const output: string | undefined = row.result?.value.output;
  for (const operation of row.operations) {
    if (operation.origin === 'cell') { const cellID: string = operation.cell_id; void cellID; }
    else { const cellID: null = operation.cell_id; void cellID; }
  }
  void code; void output;
}
// @ts-expect-error SDK snapshots are immutable, not a second renderer-owned ledger.
snapshot.cells.push(cells[0]);
void turns; void operations;
