import { afterEach, describe, expect, it, vi } from 'vitest';
import type { Session } from '@whip/sdk';
import { CompositionStore, compositionKey } from '../src/compositions';

afterEach(() => vi.unstubAllGlobals());

function previewURLs() {
  let next = 0;
  vi.stubGlobal('URL', class extends URL {
    static createObjectURL = vi.fn(() => `blob:preview-${++next}`);
    static revokeObjectURL = vi.fn();
  });
}

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((done) => {
    resolve = done;
  });
  return { promise, resolve };
}
function file(name = 'note.txt', body = 'hello', size?: number): File {
  const bytes = new TextEncoder().encode(body);
  return {
    name,
    type: name.endsWith('.png') ? 'image/png' : 'text/plain',
    size: size ?? bytes.length,
    arrayBuffer: vi.fn(async () => bytes.buffer),
  } as unknown as File;
}
function fixture() {
  const state = {
    state: 'connected',
    info: { runtime_id: 'host', generation: '1' },
  };
  const uploaded = (id = 'content') => ({
    asAttachment: (kind: string, name: string) => ({ kind, name, ref: id }),
  });
  const upload = vi.fn(async (_bytes: Uint8Array, _options: unknown) =>
    uploaded(),
  );
  const session = {
    rootId: 'root',
    client: { getSnapshot: () => state, upload },
  } as unknown as Session;
  return {
    store: new CompositionStore(),
    session,
    upload,
    uploaded,
    state,
    key: 'host:root:child',
  };
}

describe('transient compositions', () => {
  it('stages local files without uploading, then transfers their identities/previews to the created root', async () => {
    previewURLs();
    const f = fixture();
    const image = file('first.png');
    f.store.stage('new:one:prompt', [image, file()]);
    const staged = f.store.get('new:one:prompt').attachments;
    expect(staged.map(item => item.staged)).toEqual([true, true]);
    expect(image.arrayBuffer).not.toHaveBeenCalled();
    expect(f.upload).not.toHaveBeenCalled();
    // A local draft cannot block another session's serial upload queue.
    await f.store.add(f.key, f.session, 'host', 'child', [file('other.txt')]);
    f.store.invalidateRuntime('host');
    expect(f.store.get('new:one:prompt').attachments).toBe(staged);
    await f.store.adopt('new:one:prompt', f.session, 'host');
    const uploaded = f.store.get('host:root:root').attachments;
    expect(uploaded.map(item => item.id)).toEqual(staged.map(item => item.id));
    expect(uploaded[0]).toMatchObject({ previewUrl: staged[0]!.previewUrl, staged: false, value: { kind: 'image' } });
    expect(f.store.get('new:one:prompt').attachments).toHaveLength(0);
    expect(f.upload).toHaveBeenLastCalledWith(expect.any(Uint8Array), expect.objectContaining({ rootId: 'root', agentId: 'root' }));
    expect(URL.revokeObjectURL).not.toHaveBeenCalled();
    f.store.dispose();
    expect(URL.revokeObjectURL).toHaveBeenCalledExactlyOnceWith(staged[0]!.previewUrl);
  });

  it('bounds staged files with other drafts and releases removed files before adoption', async () => {
    previewURLs();
    const f = fixture();
    expect(() => f.store.stage('new:one:prompt', Array.from({ length: 17 }, () => file('shot.png')))).toThrow(/16 files/);
    f.store.stage('new:one:prompt', [file('first.png', '', 15 * 1024 * 1024)]);
    expect(() => f.store.add(f.key, f.session, 'host', 'child', [file('second.png', '', 6 * 1024 * 1024)])).toThrow(/20 MiB/);
    f.store.clear('new:one:prompt');
    await f.store.adopt('new:one:prompt', f.session, 'host');
    expect(f.upload).not.toHaveBeenCalled();
    expect(f.store.getSnapshot().bytes).toBe(0);
    expect(URL.revokeObjectURL).toHaveBeenCalledOnce();
    f.store.dispose();
  });

  it('keeps local image previews through uploading, observer removal, and settlement without another read', async () => {
    previewURLs();
    const f = fixture();
    const transfer = deferred<ReturnType<typeof f.uploaded>>();
    f.upload.mockImplementationOnce(() => transfer.promise);
    const image = file('shot.png');
    const unsubscribe = f.store.subscribe(vi.fn());
    const pending = f.store.add(f.key, f.session, 'host', 'child', [image, file()]);
    expect(f.store.get(f.key).attachments[0]).toMatchObject({ mediaType: 'image/png', previewUrl: 'blob:preview-1' });
    expect(f.store.get(f.key).attachments[1]?.previewUrl).toBeUndefined();
    expect(URL.createObjectURL).toHaveBeenCalledExactlyOnceWith(image);
    unsubscribe();
    expect(URL.revokeObjectURL).not.toHaveBeenCalled();
    transfer.resolve(f.uploaded()); await pending;
    expect(f.store.get(f.key).attachments[0]).toMatchObject({ previewUrl: 'blob:preview-1', value: { kind: 'image' } });
    expect(image.arrayBuffer).toHaveBeenCalledOnce();
    expect(URL.revokeObjectURL).not.toHaveBeenCalled();
    f.store.dispose();
    expect(URL.revokeObjectURL).toHaveBeenCalledExactlyOnceWith('blob:preview-1');
  });

  it.each(['remove', 'accepted', 'session', 'host', 'clearAll', 'dispose'] as const)('releases image previews on %s, once, including pending uploads', async action => {
    previewURLs();
    const f = fixture();
    await f.store.add(f.key, f.session, 'host', 'child', [file('shot.png')]);
    const transfer = deferred<ReturnType<typeof f.uploaded>>();
    f.upload.mockImplementationOnce(() => transfer.promise);
    const pending = f.store.add(f.key, f.session, 'host', 'child', [file('shot.png')]);
    await vi.waitFor(() => expect(f.upload).toHaveBeenCalledTimes(2));
    const ids = f.store.get(f.key).attachments.map(item => item.id);
    if (action === 'remove') ids.forEach(id => f.store.remove(f.key, id));
    if (action === 'accepted') f.store.clear(f.key, ids);
    if (action === 'session') f.store.clearSession('host', 'root');
    if (action === 'host') f.store.invalidateRuntime('host');
    if (action === 'clearAll') f.store.clearAll();
    if (action === 'dispose') f.store.dispose();
    transfer.resolve(f.uploaded()); await pending;
    expect(URL.revokeObjectURL).toHaveBeenCalledTimes(2);
    expect(URL.revokeObjectURL).toHaveBeenCalledWith('blob:preview-1');
    expect(URL.revokeObjectURL).toHaveBeenCalledWith('blob:preview-2');
    expect(f.store.get(f.key).attachments.every(item => !item.previewUrl)).toBe(true);
    f.store.dispose();
    expect(URL.revokeObjectURL).toHaveBeenCalledTimes(2);
  });

  it('retains failed-upload previews, releases only accepted IDs, and isolates other drafts', async () => {
    previewURLs();
    const f = fixture();
    f.upload.mockRejectedValueOnce(new Error('Offline'));
    await f.store.add(f.key, f.session, 'host', 'child', [file('shot.png')]);
    const [failed] = f.store.get(f.key).attachments;
    expect(failed).toMatchObject({ previewUrl: 'blob:preview-1', error: 'Offline' });
    await f.store.add('host:other:child', { ...f.session, rootId: 'other' } as Session, 'host', 'child', [file('shot.png')]);
    await f.store.add(f.key, f.session, 'host', 'child', [file('shot.png')]);
    expect(URL.revokeObjectURL).not.toHaveBeenCalled();
    f.store.clear(f.key, [failed!.id]);
    expect(URL.revokeObjectURL).toHaveBeenCalledExactlyOnceWith('blob:preview-1');
    expect(f.store.get(f.key).attachments[0]?.previewUrl).toBe('blob:preview-3');
    f.store.clearSession('host', 'root');
    expect(f.store.get('host:other:child').attachments[0]?.previewUrl).toBe('blob:preview-2');
    f.store.dispose();
    expect(URL.revokeObjectURL).toHaveBeenCalledTimes(3);
  });

  it('does not allocate previews for rejected batches or fail an upload when local preview creation fails', async () => {
    previewURLs();
    const f = fixture();
    expect(() => f.store.add(f.key, f.session, 'host', 'child', Array.from({ length: 17 }, () => file('shot.png')))).toThrow(/16 files/);
    expect(URL.createObjectURL).not.toHaveBeenCalled();
    vi.mocked(URL.createObjectURL).mockImplementationOnce(() => { throw new Error('No preview'); });
    await f.store.add(f.key, f.session, 'host', 'child', [file('shot.png')]);
    expect(f.store.get(f.key).attachments[0]).toMatchObject({ value: { kind: 'image' } });
    expect(f.store.get(f.key).attachments[0]?.previewUrl).toBeUndefined();
    f.store.dispose();
    expect(URL.revokeObjectURL).not.toHaveBeenCalled();
  });
  it('isolates an explicit surface and retains recipient-scoped UTF-8/image upload semantics', async () => {
    const f = fixture();
    const surface = 'design:tab';
    const key = compositionKey('host', 'root', 'child', surface);
    expect(key).toBe('host:root:child:surface:design%3Atab');
    expect(compositionKey('host', 'root', 'child')).toBe(f.key);
    await f.store.add(f.key, f.session, 'host', 'child', [file('normal.txt')]);
    await f.store.add(key, f.session, 'host', 'child', [file('context.txt', '改变'), file('viewport.png')], surface);
    expect(f.store.get(key).attachments.map(item => item.value?.kind)).toEqual(['text', 'image']);
    expect(f.store.get(f.key).attachments.map(item => item.name)).toEqual(['normal.txt']);
    expect(Array.from(f.upload.mock.calls[1]![0])).toEqual(Array.from(new TextEncoder().encode('改变')));
    expect(f.upload.mock.calls[1]![1]).toMatchObject({ rootId: 'root', agentId: 'child', mediaType: 'text/plain' });
    expect(f.store.hasAttachments('host', 'root')).toBe(true);
    f.store.clear(key);
    expect(f.store.get(f.key).attachments).toHaveLength(1);
    expect(f.store.getSnapshot().attachmentCount).toBe(1);
    f.store.dispose();
  });

  it('rejects mismatched or unbounded surface keys without weakening host/recipient checks', () => {
    const f = fixture();
    const surface = 'design:tab';
    const key = compositionKey('host', 'root', 'child', surface);
    expect(() => f.store.add(key, f.session, 'host', 'child', [file()])).toThrow(/selected recipient/);
    expect(() => f.store.add(key, f.session, 'host', 'child', [file()], 'other')).toThrow(/selected recipient/);
    expect(() => f.store.add(key, f.session, 'host', 'other', [file()], surface)).toThrow(/selected recipient/);
    expect(() => f.store.add(f.key, f.session, 'host', 'child', [file()], surface)).toThrow(/selected recipient/);
    expect(() => compositionKey('host', 'root', 'child', '')).toThrow(/surface/);
    expect(() => compositionKey('host', 'root', 'child', 'x'.repeat(129))).toThrow(/surface/);
    expect(() => compositionKey('host', 'root', 'child', '😀'.repeat(64))).toThrow(/too long/);
    f.state.info.runtime_id = 'another-host';
    expect(() => f.store.add(key, f.session, 'host', 'child', [file()], surface)).toThrow(/connected host/);
    expect(f.upload).not.toHaveBeenCalled();
    f.store.dispose();
  });

  it('root deletion aborts surface uploads and locks without touching another root', async () => {
    const f = fixture();
    const surface = 'design:tab';
    const key = compositionKey('host', 'root', 'child', surface);
    const otherKey = compositionKey('host', 'other', 'child', surface);
    await f.store.add(otherKey, { ...f.session, rootId: 'other' } as Session, 'host', 'child', [file()], surface);
    const transfer = deferred<ReturnType<typeof f.uploaded>>();
    f.upload.mockImplementationOnce(() => transfer.promise);
    const pending = f.store.add(key, f.session, 'host', 'child', [file()], surface);
    await vi.waitFor(() => expect(f.upload).toHaveBeenCalledTimes(2));
    const signal = (f.upload.mock.calls[1]![1] as { signal: AbortSignal }).signal;
    const token = f.store.beginSubmission(key)!;
    f.store.clearSession('host', 'root');
    await pending;
    expect(signal.aborted).toBe(true);
    expect(f.store.get(key)).toEqual({ attachments: [], sending: false });
    expect(f.store.get(otherKey).attachments).toHaveLength(1);
    transfer.resolve(f.uploaded());
    await Promise.resolve();
    f.store.finishSubmission(key, token);
    expect(f.store.get(key)).toEqual({ attachments: [], sending: false });
    f.store.dispose();
  });

  it('host invalidation marks surface evidence unavailable and releases submission locks', async () => {
    const f = fixture();
    const surface = 'design:tab';
    const key = compositionKey('host', 'root', 'child', surface);
    await f.store.add(key, f.session, 'host', 'child', [file()], surface);
    f.store.beginSubmission(key);
    f.store.invalidateRuntime('host');
    expect(f.store.get(key).sending).toBe(false);
    expect(f.store.get(key).attachments[0]).toMatchObject({ error: expect.stringMatching(/changing hosts/) });
    expect(f.store.get(key).attachments[0]!.value).toBeUndefined();
    f.store.dispose();
  });

  it('deleting one root cancels all recipient uploads and submission locks without clearing another root', async () => {
    const f = fixture();
    const other = { ...f.session, rootId: 'other' } as Session;
    await f.store.add('host:other:child', other, 'host', 'child', [file('keep.txt')]);
    const transfer = deferred<ReturnType<typeof f.uploaded>>();
    f.upload.mockImplementationOnce(() => transfer.promise);
    const done = f.store.add(f.key, f.session, 'host', 'child', [file()]);
    const queued = f.store.add('host:root:root', f.session, 'host', 'root', [file('queued.txt')]);
    await vi.waitFor(() => expect(f.upload).toHaveBeenCalledTimes(2));
    const signal = (f.upload.mock.calls[1]![1] as { signal: AbortSignal }).signal;
    const submission = f.store.beginSubmission(f.key)!;
    f.store.rememberSelection('host:view:child', { start: 1, end: 2 });
    f.store.rememberSelection('host:other-view:child', { start: 3, end: 4 });
    f.store.clearSession('host', 'root', ['view']);
    await Promise.all([done, queued]);
    expect(signal.aborted).toBe(true);
    expect(f.store.get(f.key)).toEqual({ attachments: [], sending: false });
    expect(f.store.get('host:root:root').attachments).toEqual([]);
    expect(f.store.get('host:other:child').attachments).toHaveLength(1);
    expect(f.store.selection('host:view:child')).toBeUndefined();
    expect(f.store.selection('host:other-view:child')).toEqual({ start: 3, end: 4 });
    transfer.resolve(f.uploaded()); await Promise.resolve();
    f.store.finishSubmission(f.key, submission);
    expect(f.store.get(f.key)).toEqual({ attachments: [], sending: false });
    expect(f.store.getSnapshot().attachmentCount).toBe(1);
    f.store.dispose();
  });
  it('serializes source reads and exact-recipient uploads across drafts, retaining only ready references', async () => {
    const f = fixture();
    const transfer = deferred<ReturnType<typeof f.uploaded>>();
    f.upload.mockImplementationOnce(() => transfer.promise);
    const first = file();
    const second = file('second.txt');
    const a = f.store.add(f.key, f.session, 'host', 'child', [first]);
    const b = f.store.add('host:root:root', f.session, 'host', 'root', [
      second,
    ]);
    await vi.waitFor(() => expect(f.upload).toHaveBeenCalledTimes(1));
    expect(second.arrayBuffer).not.toHaveBeenCalled();
    expect(f.upload.mock.calls[0]![1]).toMatchObject({
      rootId: 'root',
      agentId: 'child',
      mediaType: 'text/plain',
    });
    transfer.resolve(f.uploaded());
    await Promise.all([a, b]);
    expect(f.upload.mock.calls[1]![1]).toMatchObject({
      rootId: 'root',
      agentId: 'root',
    });
    expect(f.store.get(f.key).attachments[0]).toMatchObject({
      name: 'note.txt',
      value: { ref: 'content', kind: 'text' },
    });
    const stable = f.store.get(f.key);
    expect(f.store.get(f.key)).toBe(stable);
    expect(f.store.getSnapshot()).toEqual({ attachmentCount: 2, bytes: 10 });
    f.store.dispose();
  });
  it('survives observer removal, but removing a pending file aborts it and ignores late completion', async () => {
    const f = fixture();
    const transfer = deferred<ReturnType<typeof f.uploaded>>();
    f.upload.mockImplementationOnce(() => transfer.promise);
    const unsubscribe = f.store.subscribe(vi.fn());
    const done = f.store.add(f.key, f.session, 'host', 'child', [file()]);
    await vi.waitFor(() => expect(f.upload).toHaveBeenCalledTimes(1));
    const signal = (f.upload.mock.calls[0]![1] as { signal: AbortSignal })
      .signal;
    unsubscribe();
    expect(signal.aborted).toBe(false);
    const id = f.store.get(f.key).attachments[0]!.id;
    f.store.remove(f.key, id);
    expect(signal.aborted).toBe(true);
    await done;
    transfer.resolve(f.uploaded());
    await Promise.resolve();
    expect(f.store.get(f.key).attachments).toEqual([]);
    expect(f.store.getSnapshot().bytes).toBe(0);
    f.store.dispose();
  });
  it('marks host-scoped references unavailable and cannot revive them after replacement', async () => {
    const f = fixture();
    await f.store.add(f.key, f.session, 'host', 'child', [file()]);
    const transfer = deferred<ReturnType<typeof f.uploaded>>();
    f.upload.mockImplementationOnce(() => transfer.promise);
    const done = f.store.add(f.key, f.session, 'host', 'child', [
      file('second.txt'),
    ]);
    await vi.waitFor(() => expect(f.upload).toHaveBeenCalledTimes(2));
    f.store.invalidateRuntime('host');
    const state = f.store.get(f.key);
    expect(
      state.attachments.every(
        (item) => !item.value && item.error?.includes('unavailable'),
      ),
    ).toBe(true);
    transfer.resolve(f.uploaded());
    await done;
    await Promise.resolve();
    expect(f.store.get(f.key)).toBe(state);
    expect(f.upload).toHaveBeenCalledTimes(2);
    f.state.info.runtime_id = 'replacement';
    expect(() =>
      f.store.add(f.key, f.session, 'host', 'child', [file()]),
    ).toThrow(/connected host/);
    expect(() =>
      f.store.add('replacement:root:child', f.session, 'host', 'child', [
        file(),
      ]),
    ).toThrow(/connected host/);
    f.store.dispose();
  });
  it('does not confuse a generation change with another persistent runtime', async () => {
    const f = fixture();
    const transfer = deferred<ReturnType<typeof f.uploaded>>();
    f.upload.mockImplementationOnce(() => transfer.promise);
    const done = f.store.add(f.key, f.session, 'host', 'child', [file()]);
    await vi.waitFor(() => expect(f.upload).toHaveBeenCalledTimes(1));
    f.state.info.generation = '2';
    transfer.resolve(f.uploaded());
    await done;
    expect(f.store.get(f.key).attachments[0]?.value).toBeDefined();
    expect(f.upload).toHaveBeenCalledTimes(1);
    f.store.dispose();
  });
  it('rejects file-count, aggregate-byte and draft-count overflow without evicting another draft', async () => {
    const f = fixture();
    const read = deferred<ArrayBuffer>();
    const large = file('large.png', '', 20 * 1024 * 1024);
    large.arrayBuffer = () => read.promise;
    const done = f.store.add(f.key, f.session, 'host', 'child', [large]);
    expect(() =>
      f.store.add('host:root:root', f.session, 'host', 'root', [file()]),
    ).toThrow(/20 MiB/);
    expect(f.store.get(f.key).attachments).toHaveLength(1);
    f.store.clearAll();
    await done;
    read.resolve(new ArrayBuffer(0));
    expect(() =>
      f.store.add(
        f.key,
        f.session,
        'host',
        'child',
        Array.from({ length: 17 }, () => file()),
      ),
    ).toThrow(/16 files/);
    for (let i = 0; i < 32; i++)
      await f.store.add(`host:root:${i}`, f.session, 'host', String(i), [
        file('empty.txt', ''),
      ]);
    expect(() =>
      f.store.add(f.key, f.session, 'host', 'child', [file('empty.txt', '')]),
    ).toThrow(/32 unsent/);
    expect(f.store.getSnapshot().attachmentCount).toBe(32);
    f.store.dispose();
  });
  it('clears only accepted file identities and cannot unlock a later submission with an old token', async () => {
    const f = fixture();
    await f.store.add(f.key, f.session, 'host', 'child', [file()]);
    const sent = f.store.get(f.key).attachments.map((item) => item.id);
    const first = f.store.beginSubmission(f.key)!;
    expect(f.store.beginSubmission(f.key)).toBeUndefined();
    f.store.finishSubmission(f.key, first);
    await f.store.add(f.key, f.session, 'host', 'child', [file('new.txt')]);
    const second = f.store.beginSubmission(f.key)!;
    f.store.clear(f.key, sent);
    f.store.finishSubmission(f.key, first);
    expect(f.store.get(f.key).sending).toBe(true);
    expect(f.store.get(f.key).attachments.map((item) => item.name)).toEqual([
      'new.txt',
    ]);
    f.store.finishSubmission(f.key, second);
    f.store.dispose();
    expect(f.store.getSnapshot()).toEqual({ attachmentCount: 0, bytes: 0 });
    expect(() => f.store.beginSubmission(f.key)).toThrow(/closed/);
  });
  it('records failed uploads once without replaying them and rejects invalid text before upload', async () => {
    const f = fixture();
    f.upload.mockRejectedValueOnce(new Error('interrupted'));
    await f.store.add(f.key, f.session, 'host', 'child', [file()]);
    expect(f.store.get(f.key).attachments[0]?.error).toBe('interrupted');
    f.state.state = 'reconnecting';
    expect(() =>
      f.store.add(f.key, f.session, 'host', 'child', [file()]),
    ).toThrow(/connected host/);
    f.state.state = 'connected';
    const invalid = file();
    invalid.arrayBuffer = async () => new Uint8Array([255]).buffer;
    await f.store.add(f.key, f.session, 'host', 'child', [invalid]);
    expect(f.upload).toHaveBeenCalledTimes(1);
    expect(f.store.get(f.key).attachments[1]?.error).toBeTruthy();
    f.store.dispose();
  });
});
