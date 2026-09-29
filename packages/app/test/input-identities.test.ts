import { describe, expect, it } from 'vitest';
import type { Admission, InputPageResult } from '@whip/protocol';
import { SubmittedInputs, queuedInputRows, matchesInput, admittedText } from '../src/input-presentation';
type Input = NonNullable<InputPageResult['items']>[number];
const input = (id: string, owner = 'child'): Input => ({ id, session_id: owner, ordinal: '9007199254740993', source: 'user', kind: 'prompt', state: 'queued', turn_id: null, created_at: '2026-01-01T00:00:00Z', text_preview: 'Accepted text', preview_truncated: false, attachment_count: '0' });
const ack = (client: string, request: string, id: string, owner = 'child') => ({ receipt: { identity: { client_id: client, request_id: request }, input_id: id }, input: { id, session_id: owner } }) as Admission;
describe('v4 input presentation identity', () => {
  it('matches a local preview only to the exact acknowledged client, owner and input', () => {
    const store = new SubmittedInputs(); const id = store.add({ runtimeId: 'runtime', rootId: 'root', agentId: 'child', clientId: 'client' }, 'Authored', true, 'request');
    store.acknowledge(ack('other', id, 'canonical'), 'runtime'); store.acknowledge(ack('client', id, 'canonical', 'other'), 'runtime');
    expect(store.getSnapshot()[0]?.accepted).toBe(false);
    store.acknowledge(ack('client', id, 'canonical'), 'runtime');
    expect(matchesInput(store.getSnapshot()[0]!, input('canonical'))).toBe(true);
    expect(matchesInput(store.getSnapshot()[0]!, input('canonical', 'other'))).toBe(false);
    const rows = queuedInputRows([{ item: input('canonical'), stale: false }], store.getSnapshot());
    expect(rows).toHaveLength(1); expect(rows[0]?.text).toBe('Authored'); expect(rows[0]?.status).toBe('Queued');
  });
  it('keeps accepted input from another client visible and missing activity explicitly stale', () => {
    const rows = queuedInputRows([{ item: input('external'), stale: true }], []);
    expect(rows[0]).toMatchObject({ text: 'Accepted text', status: 'Checking queue…', stale: true });
    expect(admittedText({ ...input('attachment'), text_preview: '', attachment_count: '2' })).toBe('2 attached files');
  });
});
