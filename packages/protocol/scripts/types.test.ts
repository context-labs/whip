import { type Operations, type Session, validate } from '../generated/index.js';

const update: Operations['sessions.configure']['params'] = {
  session_id: 'session', expected_revision: '9007199254740993',
  patch: { tools: {}, output: { schema: null } },
};
const input: unknown = update;
if (validate('Session', input)) {
  const session: Session = input;
  const parent: string | null = session.parent_id;
  void parent;
}
// @ts-expect-error decimal counters cannot be JS numbers
const invalid: Operations['sessions.history']['params'] = { session_id: 'session', after: 2, limit: 10 };
void invalid;

declare const operation: NonNullable<Operations['turns.operations']['result']['items']>[number];
const owner: string = operation.session_id;
const operationID: string = operation.id;
if (operation.origin === 'cell') {
  const cell: string = operation.cell_id;
  void cell;
} else {
  const noCell: null = operation.cell_id;
  void noCell;
}
// @ts-expect-error native operation scope cannot become unknown through a nested union
const numericOwner: number = operation.session_id;
void owner; void operationID; void numericOwner;

declare const trace: Operations['trace.page']['result']['items'][number];
if (trace.span) {
  for (const attribute of trace.span.attributes) {
    const key: string = attribute.key;
    const count: string | null = attribute.count;
    const text: string | null = attribute.text;
    const flag: boolean | null = attribute.flag;
    void key; void count; void text; void flag;
  }
}
