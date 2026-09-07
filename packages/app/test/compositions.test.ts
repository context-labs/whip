import { describe, expect, it, vi } from 'vitest';
import type { Session } from '@whip/sdk';
import { CompositionStore } from '../src/compositions';

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
