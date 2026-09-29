import type { TracePageParams } from '@whip/protocol';
import type { Client } from '../dist/index.js';
import { createTraceView, type DeepReadonly, type TraceRow, type TraceViewSnapshot } from '../dist/state.js';
import { useTraceView } from '../dist/react.js';
declare const client: Client;
const view = createTraceView(client, 'root', { maxRows: 256, maxRoots: 32 });
const snapshot: TraceViewSnapshot | ReturnType<typeof view.getSnapshot> = useTraceView(view);
const row: DeepReadonly<TraceRow> | undefined = snapshot.rows[0];
void row;
const newest: TracePageParams = {
  root_id: 'root',
  before: null,
  expected_revision: null,
  trace_id: '',
  roots_only: false,
  limit: 1,
  max_bytes: 4096,
};
void newest;
// @ts-expect-error Readonly canonical evidence must not become a mutable client ledger.
snapshot.rows.push(row!);
// @ts-expect-error Every read declares its direction.
const missing: TracePageParams = {
  root_id: 'root',
  expected_revision: null,
  trace_id: '',
  roots_only: false,
  limit: 1,
  max_bytes: 4096,
};
void missing;
