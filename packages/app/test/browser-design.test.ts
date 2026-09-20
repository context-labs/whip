import { describe, expect, it, vi } from 'vitest';
import { BrowserDesignController, type DesignSubmission } from '../src/browser-design-controller';
import { boundedDesignText, designComposerBounds, designHoverBounds, designHoverLabel, designHoverMotion, designLease, designRevision } from '../src/browser-design-geometry';
import type { BrowserDesignCapture, BrowserDesignEvent, BrowserDesignPlatform, BrowserDesignState } from '../src/browser-design-types';

const state = (): BrowserDesignState => ({ epoch: 'epoch', tabId: 'tab', generation: 'generation', designId: 'design', documentRevision: 1, selectionRevision: 1, status: 'active', viewport: { width: 1280, height: 800 }, elements: [{ id: 'one', label: 'button', number: 1, color: 'blue', bounds: { x: 100, y: 100, width: 80, height: 30 } }] });
const theme = { name: 'default', mode: 'light' } as const;
const recipient = { id: 'conversation', label: 'Conversation · Local', available: true };
function deferred<T>() { let resolve!: (value: T) => void; const promise = new Promise<T>(done => { resolve = done; }); return { promise, resolve }; }
function setup(submit = vi.fn(async (_input: DesignSubmission) => ({ accepted: false, error: 'Rejected' }))) {
  let listener: (event: BrowserDesignEvent) => void = () => {};
  const capture = (): BrowserDesignCapture => ({ ...designRevision(state()), capturedAt: '2026-09-18T00:00:00Z', text: '{"untrusted":true}', image: 'data:image/png;base64,AA==' });
  const platform: BrowserDesignPlatform = { start: vi.fn(async () => state()), stop: vi.fn(async () => {}), update: vi.fn(async () => {}), capture: vi.fn(async () => capture()), onEvent: next => { listener = next; return () => { listener = () => {}; }; } };
  const controller = new BrowserDesignController(platform, submit);
  controller.configure([recipient], [recipient.id], theme);
  return { controller, platform, submit, capture, emit: (event: BrowserDesignEvent) => listener(event) };
}
async function start(fixture: ReturnType<typeof setup>) { await fixture.controller.start({ epoch: 'epoch', tabId: 'tab', generation: 'generation' }); await fixture.controller.intent({ kind: 'prompt', value: 'Make this clearer' }); }

describe('design hover label and placement', () => {
  const viewport = { width: 800, height: 600 };
  const size = { width: 280, height: 52 };
  it('separates the kind from normalized page text without interpreting markup', () => {
    expect(designHoverLabel('h1 ·  Inference\n infrastructure · AI')).toEqual({ kind: 'h1', description: 'Inference infrastructure · AI' });
    expect(designHoverLabel('button')).toEqual({ kind: 'button', description: '' });
    expect(designHoverLabel('div · <script>text</script>')).toEqual({ kind: 'div', description: '<script>text</script>' });
  });
  it('anchors above using the measured height and flips below at the top edge', () => {
    expect(designHoverBounds({ x: 100, y: 100, width: 200, height: 40 }, viewport, size)).toEqual({ x: 100, y: 40, ...size });
    expect(designHoverBounds({ x: 100, y: 4, width: 200, height: 40 }, viewport, size)).toEqual({ x: 100, y: 52, ...size });
  });
  it('clamps to narrow viewports and anchors oversized elements to their visible bounds', () => {
    expect(designHoverBounds({ x: 780, y: 580, width: 60, height: 50 }, viewport, size)).toEqual({ x: 512, y: 520, ...size });
    const bounds = designHoverBounds({ x: -300, y: -100, width: 1200, height: 900 }, { width: 240, height: 400 }, size);
    expect(bounds.x).toBe(8);
    expect(bounds.width).toBe(224);
    expect(bounds.y).toBeGreaterThanOrEqual(8);
    expect(bounds.y + bounds.height).toBeLessThanOrEqual(392);
  });
  it('avoids a composer above the target when space below is available', () => {
    const target = { x: 100, y: 200, width: 200, height: 40 };
    const composer = { x: 80, y: 80, width: 360, height: 110 };
    expect(designHoverBounds(target, viewport, size, [composer])).toEqual({ x: 100, y: 248, ...size });
  });
});

describe('design hover motion', () => {
  const hovered = (id = 'a', x = 10): BrowserDesignState => ({ ...state(), hoverGeometryRevision: 0, hover: { id, label: id, number: 0, color: 'blue', bounds: { x, y: 20, width: 100, height: 40 } } });
  it('snaps first appearances, animates switches, and retains interrupted transitions across equal snapshots', () => {
    const a = hovered(), b = hovered('b', 200);
    expect(designHoverMotion(undefined, a, false)).toBe(false);
    expect(designHoverMotion(state(), a, false)).toBe(false);
    expect(designHoverMotion(a, b, false)).toBe(true);
    expect(designHoverMotion(b, structuredClone(b), true)).toBe(true);
    expect(designHoverMotion(b, hovered('c', 400), true)).toBe(true);
    expect(designHoverMotion(b, hovered('b', 201), true)).toBe(false);
    expect(designHoverMotion(b, structuredClone(b), false)).toBe(false);
  });
  it('never bridges geometry invalidation even when the clear snapshot is batched away', () => {
    const a = hovered(), b = hovered('b', 200);
    expect(designHoverMotion(a, { ...b, hoverGeometryRevision: 1 }, true)).toBe(false);
    expect(designHoverMotion(a, { ...b, hoverGeometryRevision: undefined }, true)).toBe(false);
    expect(designHoverMotion(a, { ...b, viewport: { width: 640, height: 480 } }, true)).toBe(false);
    for (const key of ['epoch', 'generation', 'tabId', 'designId'] as const) expect(designHoverMotion(a, { ...b, [key]: 'changed' }, true)).toBe(false);
    for (const key of ['documentRevision', 'selectionRevision'] as const) expect(designHoverMotion(a, { ...b, [key]: 2 }, true)).toBe(false);
    for (const status of ['stale', 'stopped', 'unavailable', 'capturing'] as const) expect(designHoverMotion(a, { ...b, status }, true)).toBe(false);
    expect(designHoverMotion(a, { ...b, hover: undefined }, true)).toBe(false);
  });
});

describe('design composer geometry', () => {
  it('bounds Unicode clipboard text by UTF-8 bytes without broken code points', () => {
    expect(boundedDesignText('日本語', 7)).toBe('日本');
    expect(new TextEncoder().encode(boundedDesignText('🙂'.repeat(5000), 16384)).length).toBe(16384);
    expect(boundedDesignText('a\u0000b\n', 20)).toBe('ab\n');
  });
  it('anchors below a single element and flips above bottom targets', () => {
    expect(designComposerBounds(state(), 220)).toEqual({ x: 100, y: 142, width: 360, height: 220 });
    const next = state(); next.elements[0]!.bounds.y = 740;
    expect(designComposerBounds(next, 220).y).toBe(508);
  });
  it('docks multiple selections and clamps narrow/offscreen geometry', () => {
    const next = state(); next.elements.push({ ...next.elements[0]!, id: 'two' });
    expect(designComposerBounds(next, 220)).toEqual({ x: 908, y: 568, width: 360, height: 220 });
    next.viewport = { width: 320, height: 200 }; next.elements = [next.elements[0]!]; next.elements[0]!.bounds.x = -100;
    expect(designComposerBounds(next, 400)).toEqual({ x: 12, y: 12, width: 296, height: 176 });
  });
  it('projects only IPC lease and revision keys', () => {
    expect(Object.keys(designLease(state())).sort()).toEqual(['designId', 'epoch', 'generation', 'tabId']);
    expect(Object.keys(designRevision(state())).sort()).toEqual(['designId', 'documentRevision', 'epoch', 'generation', 'selectionRevision', 'tabId']);
  });
});
describe('design draft controller', () => {
  it('defaults only a unique explicit association and never substitutes a focused conversation', () => {
    const fixture = setup(); const controller = new BrowserDesignController(fixture.platform, fixture.submit);
    controller.configure([recipient], [], theme); expect(controller.getSnapshot().draft.recipientId).toBe('');
    controller.configure([recipient, { ...recipient, id: 'other', available: false }], [recipient.id, 'other'], theme);
    expect(controller.getSnapshot().draft.recipientId).toBe('');
    controller.configure([recipient], [recipient.id], theme); expect(controller.getSnapshot().draft.recipientId).toBe(recipient.id);
  });
  it('preserves an explicitly cleared recipient instead of re-defaulting', async () => {
    const fixture = setup(); await fixture.controller.intent({ kind: 'recipient', id: '' });
    fixture.controller.configure([recipient], [recipient.id], theme);
    expect(fixture.controller.getSnapshot().draft.recipientId).toBe('');
  });
  it('sends closed-schema IPC arguments and keeps drafts on rejection', async () => {
    const fixture = setup(); await start(fixture);
    await fixture.controller.intent({ kind: 'send' });
    expect(fixture.platform.capture).toHaveBeenCalledWith({ ...designRevision(state()), screenshot: true });
    expect(Object.keys(vi.mocked(fixture.platform.update).mock.calls[0]![0]).sort()).toEqual(['designId', 'draft', 'epoch', 'generation', 'tabId']);
    expect(fixture.submit).toHaveBeenCalledOnce();
    expect(fixture.controller.getSnapshot().draft).toMatchObject({ prompt: 'Make this clearer', busy: false, error: 'Rejected' });
    await fixture.controller.stop(); expect(fixture.platform.stop).toHaveBeenCalledWith(designLease(state()));
    await start(fixture); expect(fixture.controller.getSnapshot().draft.prompt).toBe('Make this clearer');
  });
  it('invalidates live evidence but retains authored text on navigation and ignores older state', async () => {
    const fixture = setup(); await start(fixture); await fixture.controller.intent({ kind: 'capture' });
    expect(fixture.controller.getSnapshot().draft.evidence).toBeDefined();
    fixture.emit({ kind: 'state', state: { ...state(), documentRevision: 2, elements: [], status: 'stale' } });
    fixture.emit({ kind: 'state', state: state() });
    expect(fixture.controller.getSnapshot().state?.documentRevision).toBe(2);
    expect(fixture.controller.getSnapshot().draft).toMatchObject({ prompt: 'Make this clearer', evidence: undefined });
    await fixture.controller.intent({ kind: 'send' }); expect(fixture.submit).not.toHaveBeenCalled();
  });
  it('discards an in-flight capture after navigation or stop', async () => {
    const fixture = setup(); await start(fixture);
    const capture = deferred<BrowserDesignCapture>(); vi.mocked(fixture.platform.capture).mockReturnValue(capture.promise);
    const sending = fixture.controller.intent({ kind: 'send' }); await fixture.controller.stop(); capture.resolve(fixture.capture()); await sending;
    expect(fixture.submit).not.toHaveBeenCalled(); expect(fixture.controller.getSnapshot().draft.prompt).toBe('Make this clearer');
  });
  it('freezes destination and blocks duplicate sends until acceptance is known', async () => {
    const result = deferred<{ accepted: boolean; uncertain: boolean }>();
    const submit = vi.fn((_input: DesignSubmission) => result.promise);
    const fixture = setup(submit); await start(fixture); const sending = fixture.controller.intent({ kind: 'send' });
    await vi.waitFor(() => expect(submit).toHaveBeenCalledOnce());
    await fixture.controller.intent({ kind: 'recipient', id: '' });
    expect(fixture.controller.getSnapshot().draft.recipientId).toBe(recipient.id);
    result.resolve({ accepted: false, uncertain: true }); await sending;
    await fixture.controller.intent({ kind: 'send' }); expect(submit).toHaveBeenCalledOnce();
    expect(fixture.controller.getSnapshot().draft).toMatchObject({ prompt: 'Make this clearer', uncertain: true, busy: false });
    expect(vi.mocked(fixture.platform.update).mock.calls.every(([input]) => !input.clearSelection)).toBe(true);
    expect(fixture.controller.getSnapshot().state?.elements).toEqual(state().elements);
  });
  it('late acceptance cannot erase a newer design prompt', async () => {
    const fixture = setup(); await start(fixture); await fixture.controller.intent({ kind: 'send' });
    const input = fixture.submit.mock.calls[0]![0];
    await fixture.controller.intent({ kind: 'prompt', value: 'A newer request' }); input.accepted();
    expect(fixture.controller.getSnapshot().draft.prompt).toBe('A newer request');
  });
  it('acceptance clears the matching prompt and requests a revision-scoped selection reset without stopping Design Mode', async () => {
    const result = deferred<{ accepted: boolean }>(); const submit = vi.fn((_input: DesignSubmission) => result.promise);
    const fixture = setup(submit); await start(fixture); const sending = fixture.controller.intent({ kind: 'send' });
    await vi.waitFor(() => expect(submit).toHaveBeenCalledOnce()); submit.mock.calls[0]![0].accepted();
    expect(fixture.controller.getSnapshot().draft).toMatchObject({ prompt: '', busy: false, evidence: undefined });
    expect(fixture.platform.update).toHaveBeenLastCalledWith(expect.objectContaining({
      clearSelection: { documentRevision: 1, selectionRevision: 1 },
      draft: expect.objectContaining({ prompt: '', busy: false, promptReset: 1 }),
    }));
    fixture.emit({ kind: 'state', state: { ...state(), selectionRevision: 2, elements: [], hover: undefined } });
    expect(fixture.controller.getSnapshot().state).toMatchObject({ status: 'active', elements: [] });
    expect(fixture.platform.stop).not.toHaveBeenCalled();
    result.resolve({ accepted: true }); await sending;
  });
  it('late acceptance preserves a newer selection', async () => {
    const fixture = setup(); await start(fixture); await fixture.controller.intent({ kind: 'send' });
    fixture.emit({ kind: 'state', state: { ...state(), selectionRevision: 2 } });
    fixture.submit.mock.calls[0]![0].accepted();
    expect(vi.mocked(fixture.platform.update).mock.calls.every(([input]) => !input.clearSelection)).toBe(true);
    expect(fixture.controller.getSnapshot().state).toMatchObject({ selectionRevision: 2, elements: state().elements });
  });
  it('blocks stale revisions before admission after asynchronous upload', async () => {
    const fixture = setup(); await start(fixture); await fixture.controller.intent({ kind: 'send' });
    const input = fixture.submit.mock.calls[0]![0]; expect(input.current()).toBe(true);
    fixture.emit({ kind: 'state', state: { ...state(), selectionRevision: 2 } }); expect(input.current()).toBe(false);
  });
});
