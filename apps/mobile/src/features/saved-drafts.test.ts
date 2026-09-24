/** @jest-environment node */
import { savedDraftPresentation } from './saved-drafts';

const hosts = [{ id: 'host', name: 'Desk Mac', url: 'https://desk.example.ts.net', clientId: 'phone', runtimeId: 'runtime' }];
const questionKey = JSON.stringify(['runtime', 'root', 'question', 'request']);

test('draft labels identify saved servers and message, child, creation and question scopes without exposing JSON keys', () => {
  expect(savedDraftPresentation(JSON.stringify(['runtime', 'root', 'root']), 'message', hosts)).toMatchObject({ title: 'Message to root agent', detail: 'Desk Mac · Session root' });
  expect(savedDraftPresentation(JSON.stringify(['runtime', 'root', 'child-1234567890123456']), 'message', hosts))
    .toMatchObject({ title: 'Message to agent child-12…3456', detail: 'Desk Mac · Session root' });
  expect(savedDraftPresentation(JSON.stringify(['create', 'runtime', 'workflow']), 'prompt', hosts))
    .toMatchObject({ title: 'New-session prompt', detail: 'Desk Mac · Draft workflow' });
  expect(savedDraftPresentation(questionKey, 'unreadable', hosts)).toMatchObject({ title: 'Question answers', detail: 'Desk Mac · Session root · Request request' });
  const removed = savedDraftPresentation(JSON.stringify(['removed-runtime-123456', 'root', 'root']), 'message', hosts);
  expect(removed.detail).toBe('Server removed-…3456 (not saved) · Session root');
  expect(savedDraftPresentation('unknown-private-storage-key', 'message', hosts).detail).toBe('Unrecognized draft location');
});

test('ordinary and new-session draft copies preserve whitespace, long text and JSON-looking authored messages verbatim', () => {
  const text = `  {"version":1,"answers":[]}\n${'long draft '.repeat(300)}\n\t`;
  for (const key of [JSON.stringify(['runtime', 'root', 'root']), JSON.stringify(['create', 'runtime', 'workflow']), 'unknown-key']) {
    const result = savedDraftPresentation(key, text, hosts);
    expect(result.copyText).toBe(text); expect(result.preview).toBe(text);
  }
});

test('obsolete question forms export their saved questions, every option and authored answer without live-form filtering', () => {
  const written = '  Keep my original spacing.\nSecond line\n\t';
  const text = JSON.stringify({ version: 1, definition: JSON.stringify([
    ['Old question from the earlier request?', true, ['old A', 'old B']], ['Another question?', false, []],
  ]), answers: [{ selected: ['choice no longer listed', 'old A'], text: written, skipped: false }, { selected: [], text: '', skipped: true }] });
  const result = savedDraftPresentation(questionKey, text, hosts);
  expect(result.copyText).toBe(`Question 1\nOld question from the earlier request?\n\nOptions (choose any):\n- old A\n- old B\n\nSelected answers:\n- choice no longer listed\n- old A\n\nWritten answer:\n${written}\n\n———\n\nQuestion 2\nAnother question?\n\nMarked as skipped.`);
  expect(result.preview).toContain('Old question from the earlier request?');
  expect(result.preview).toContain('choice no longer listed');
  expect(result.preview).toContain(written);
  expect(result.note).toBeUndefined();
});

test('legacy answer pages remain readable without inventing missing questions or dropping skipped-page text', () => {
  const result = savedDraftPresentation(questionKey, JSON.stringify([{ selected: ['earlier choice'], text: '  authored text  ', skipped: true }]), hosts);
  expect(result.copyText).toBe('Saved answer 1\n\nSelected answers:\n- earlier choice\n\nWritten answer:\n  authored text  \n\nMarked as skipped.');
  expect(result.note).toContain('do not include their original questions');
});

test('unknown or malformed form shapes copy the entire original record rather than discarding unfamiliar fields', () => {
  const known = { version: 1, definition: JSON.stringify([['Question', false, []]]), answers: [{ selected: [], text: 'preserve me', skipped: false }] };
  for (const text of [
    '  broken {json}\n', '[]', JSON.stringify({ ...known, version: 2 }), JSON.stringify({ ...known, extraAuthoredText: 'keep me' }),
    JSON.stringify({ ...known, definition: 'broken' }), JSON.stringify({ ...known, definition: '[]' }),
    JSON.stringify({ ...known, answers: [{ ...known.answers[0], extraText: 'keep this too' }] }),
    JSON.stringify({ ...known, answers: [{ ...known.answers[0], selected: [42] }] }),
  ]) {
    const result = savedDraftPresentation(questionKey, text, hosts);
    expect(result.copyText).toBe(text); expect(result.preview).toBe(text); expect(result.note).toContain('original text');
  }
});
