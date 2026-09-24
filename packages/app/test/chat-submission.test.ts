import { expect, it, vi } from 'vitest';
import type { Session } from '@whip/sdk';
import { submitChatInput, type ChatSubmission } from '../src/chat-submission';
import { CompositionStore, compositionKey, type CompositionAttachment } from '../src/compositions';
import { SubmittedInputs } from '../src/input-presentation';
import type { AppRuntime, CommandNotice } from '../src/runtime';

function fixture() {
  const commands: CommandNotice[] = [];
  const waits: { accepted(): void; finish(): void; reject(error: Error): void }[] = [];
  const drafts = new Map([['host:root:root', 'normal draft']]);
  const runtime = {
    compositions: new CompositionStore(),
    submittedInputs: new SubmittedInputs(),
    getSnapshot: () => ({ commands }),
    report: vi.fn(),
    draft: vi.fn((key: string) => drafts.get(key) ?? ''),
    setDraft: vi.fn((key: string, text: string) => drafts.set(key, text)),
    run: vi.fn((_handle: unknown, _label: string, accepted: () => void) =>
      new Promise((resolve, reject) => waits.push({ accepted, reject, finish: () => resolve({}) }))),
  };
  const session = {
    rootId: 'root',
    submit: vi.fn(() => ({})),
    steer: vi.fn(() => ({})),
    command: vi.fn(() => ({})),
    client: {
      getSnapshot: () => ({ state: 'connected', info: { runtime_id: 'host' } }),
      upload: vi.fn(async () => ({ asAttachment: (kind: string, name: string) => ({ kind, name, ref: 'normal-file' }) })),
    },
  };
  const accepted = vi.fn();
  const input: ChatSubmission = {
    runtime: runtime as unknown as AppRuntime, session: session as unknown as Session,
    runtimeId: 'host', agentId: 'root', compositionKey: compositionKey('host', 'root', 'root', 'design:tab'),
    connected: true, text: 'Change this', attachments: [], delivery: 'queued', onAccepted: accepted,
  };
  return { runtime, session, commands, waits, input, accepted, drafts };
}

const evidence: CompositionAttachment[] = [
  { id: 'evidence', name: 'context.txt', size: 6, value: { kind: 'text', name: 'context.txt', ref: 'utf8-ref' } },
  { id: 'screenshot', name: 'viewport.png', size: 12, value: { kind: 'image', name: 'viewport.png', ref: 'image-ref' } },
];

it.each([
  ['root', 'queued', undefined, 'submit'],
  ['root', 'queued', 'turn', 'submit'],
  ['root', 'steer', undefined, 'submit'],
  ['root', 'steer', 'turn', 'steer'],
  ['child', 'queued', undefined, 'command'],
  ['child', 'steer', 'turn', 'command'],
] as const)('dispatches %s %s (%s) using %s and one preview command ID', async (agentId, delivery, activeTurn, method) => {
  const f = fixture();
  const pending = submitChatInput({ ...f.input, agentId, delivery, activeTurn, attachments: evidence });
  const [preview] = f.runtime.submittedInputs.getSnapshot();
  expect(preview).toMatchObject({ runtimeId: 'host', rootId: 'root', agentId, queued: !!activeTurn });
  const payload = { text: f.input.text, attachments: evidence.map(item => item.value) };
  if (method === 'command') {
    expect(f.session.command).toHaveBeenCalledWith('agent.submit', { id: agentId, ...payload, delivery }, { commandId: preview!.id });
  } else {
    expect(f.session[method]).toHaveBeenCalledWith(payload, { commandId: preview!.id });
  }
  expect(f.runtime.run.mock.calls[0]?.[1]).toBe(agentId === 'root' ? 'Send message' : 'Message child');
  expect(f.accepted).not.toHaveBeenCalled();
  f.waits[0]!.accepted();
  expect(f.accepted).toHaveBeenCalledOnce();
  f.waits[0]!.finish();
  expect(await pending).toEqual({ status: 'completed', accepted: true });
});

it('keeps normal destination text and attachments isolated from Design acceptance', async () => {
  const f = fixture();
  const bytes = new TextEncoder().encode('normal file');
  const file = { name: 'normal.txt', type: 'text/plain', size: bytes.length, arrayBuffer: async () => bytes.buffer } as File;
  await f.runtime.compositions.add('host:root:root', f.input.session, 'host', 'root', [file]);
  const normal = f.runtime.compositions.get('host:root:root');
  await f.runtime.compositions.add(f.input.compositionKey, f.input.session, 'host', 'root', [file], 'design:tab');
  const attachments = f.runtime.compositions.get(f.input.compositionKey).attachments;
  const clear = vi.spyOn(f.runtime.compositions, 'clear');
  const pending = submitChatInput({ ...f.input, attachments });
  f.waits[0]!.accepted();
  f.waits[0]!.finish();
  await pending;
  expect(clear).toHaveBeenCalledWith(f.input.compositionKey, attachments.map(item => item.id));
  expect(f.runtime.compositions.get(f.input.compositionKey).attachments).toEqual([]);
  expect(f.runtime.compositions.get('host:root:root')).toBe(normal);
  expect(f.drafts.get('host:root:root')).toBe('normal draft');
  expect(f.runtime.draft).not.toHaveBeenCalled();
  expect(f.runtime.setDraft).not.toHaveBeenCalled();
});

it('rejects concurrent duplicate submission but permits a new input after admission', async () => {
  const f = fixture();
  const first = submitChatInput(f.input);
  expect(await submitChatInput(f.input)).toEqual({ status: 'skipped' });
  expect(f.session.submit).toHaveBeenCalledOnce();
  f.waits[0]!.accepted();
  const second = submitChatInput({ ...f.input, text: 'Next input' });
  expect(f.session.submit).toHaveBeenCalledTimes(2);
  // The first completion must not unlock the second admission.
  f.waits[0]!.finish();
  await first;
  expect(f.runtime.compositions.get(f.input.compositionKey).sending).toBe(true);
  f.waits[1]!.accepted();
  f.waits[1]!.finish();
  await second;
  expect(f.runtime.compositions.get(f.input.compositionKey).sending).toBe(false);
});

it('preserves uncertain admission, blocks a fresh command, and cleans up on later acceptance', async () => {
  const f = fixture();
  const clear = vi.spyOn(f.runtime.compositions, 'clear');
  const pending = submitChatInput({ ...f.input, attachments: evidence });
  const id = f.runtime.submittedInputs.getSnapshot()[0]!.id;
  f.commands.push({ id: 'notice', commandId: id, runtimeId: 'host', label: 'Send message',
    draftKey: f.input.compositionKey, status: 'uncertain', delivery: 'uncertain' } as CommandNotice);
  const error = new Error('Admission acknowledgement lost');
  f.waits[0]!.reject(error);
  expect(await pending).toEqual({ status: 'failed', error, accepted: false, outcome: 'uncertain', delivery: 'uncertain' });
  expect(f.accepted).not.toHaveBeenCalled();
  expect(clear).not.toHaveBeenCalled();
  expect(f.runtime.submittedInputs.getSnapshot()[0]!.id).toBe(id);
  expect(await submitChatInput(f.input)).toEqual({ status: 'skipped', delivery: 'uncertain' });
  expect(f.session.submit).toHaveBeenCalledOnce();
  f.waits[0]!.accepted(); // runtime.run's explicit status recovery owns this callback.
  expect(f.accepted).toHaveBeenCalledOnce();
  expect(clear).toHaveBeenCalledWith(f.input.compositionKey, ['evidence', 'screenshot']);
});

it('requires the original-command recovery path even after authoritative absence', async () => {
  const f = fixture();
  f.commands.push({ id: 'notice', commandId: 'original', runtimeId: 'host', label: 'Send message',
    draftKey: f.input.compositionKey, status: 'absent', delivery: 'absent' } as CommandNotice);
  expect(await submitChatInput(f.input)).toEqual({ status: 'skipped', delivery: 'absent' });
  expect(f.session.submit).not.toHaveBeenCalled();
});

it('reports a local acceptance callback error without retaining the admission lock', async () => {
  const f = fixture();
  const error = new Error('Local draft storage failed');
  const pending = submitChatInput({ ...f.input, onAccepted: () => { throw error; } });
  f.waits[0]!.accepted();
  f.waits[0]!.finish();
  expect(await pending).toEqual({ status: 'completed', accepted: true });
  expect(f.runtime.report).toHaveBeenCalledWith(error);
  expect(f.runtime.compositions.get(f.input.compositionKey).sending).toBe(false);
});

it('returns a non-vision rejection unchanged without clearing authored input or retrying', async () => {
  const f = fixture();
  const clear = vi.spyOn(f.runtime.compositions, 'clear');
  const pending = submitChatInput({ ...f.input, attachments: evidence });
  const error = new Error('The selected model does not support image input');
  f.waits[0]!.reject(error);
  expect(await pending).toMatchObject({ status: 'failed', accepted: false, error });
  expect(f.accepted).not.toHaveBeenCalled();
  expect(clear).not.toHaveBeenCalled();
  expect(f.runtime.submittedInputs.getSnapshot()).toEqual([]);
  expect(f.runtime.compositions.get(f.input.compositionKey).sending).toBe(false);
  expect(f.session.submit).toHaveBeenCalledOnce();
});

it('distinguishes failure after acceptance from rejected admission', async () => {
  const f = fixture();
  const pending = submitChatInput(f.input);
  f.waits[0]!.accepted();
  const error = new Error('Provider failed');
  f.waits[0]!.reject(error);
  expect(await pending).toMatchObject({ status: 'failed', accepted: true, error });
  expect(f.accepted).toHaveBeenCalledOnce();
});

it.each([
  { connected: false },
  { text: '  ' },
  { attachments: [{ id: 'pending', name: 'context.txt', size: 10 }] },
  { attachments: [{ id: 'failed', name: 'context.txt', size: 10, error: 'upload failed' }] },
])('skips unavailable, empty, pending or failed input %o', async (overrides) => {
  const f = fixture();
  expect(await submitChatInput({ ...f.input, ...overrides })).toEqual({ status: 'skipped' });
  expect(f.session.submit).not.toHaveBeenCalled();
  expect(f.runtime.submittedInputs.getSnapshot()).toEqual([]);
});
