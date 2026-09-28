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
