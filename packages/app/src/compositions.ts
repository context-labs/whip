import type { InputAttachment, Session } from '@whip/sdk';

export interface CompositionAttachment {
  readonly id: string;
  readonly name: string;
  readonly size: number;
  readonly mediaType?: string;
  /** Local image source, owned by the draft rather than any mounted view. */
  readonly previewUrl?: string;
  /** Selected locally; upload waits until Send creates a session. */
  readonly staged?: boolean;
  readonly value?: InputAttachment;
  readonly error?: string;
}
export interface Composition {
  readonly attachments: readonly CompositionAttachment[];
  readonly sending: boolean;
}
interface Entry {
  state: Composition;
  submission?: symbol;
}
interface Upload {
  key: string;
  id: string;
  runtimeId: string;
  agentId: string;
  session?: Session;
  file?: File;
  abort: AbortController;
  done: Promise<void>;
  settle(): void;
}
export interface ComposerSelection {
  start: number;
  end: number;
}
const empty: Composition = Object.freeze({
  attachments: Object.freeze([]),
  sending: false,
});
const maxBytes = 20 * 1024 * 1024;

function imagePreview(file: File): string | undefined {
  if (!file.type.startsWith('image/')) return;
  try {
    return URL.createObjectURL(file);
  } catch {
    // A preview failure must not prevent the original file from being uploaded.
    return undefined;
  }
}

/** Canonical recipient scope, optionally isolated for an independent authoring surface. */
export function compositionKey(runtimeId: string, rootId: string, agentId: string, surfaceId?: string): string {
  const recipient = `${runtimeId}:${rootId}:${agentId}`;
  if (surfaceId === undefined) return recipient;
  if (!surfaceId || surfaceId.length > 128) throw new Error('Invalid composition surface');
  const key = `${recipient}:surface:${encodeURIComponent(surfaceId)}`;
  if (key.length > 512) throw new Error('Composition scope is too long');
  return key;
}

/** Window-memory composition state. Uploads belong to this store, not a view. */
export class CompositionStore {
  private readonly entries = new Map<string, Entry>();
  private readonly listeners = new Set<() => void>();
  private readonly jobs = new Map<string, Upload>();
  private readonly selections = new Map<string, ComposerSelection>();
  private snapshot: Readonly<{ attachmentCount: number; bytes: number }> =
    Object.freeze({ attachmentCount: 0, bytes: 0 });
  private draining = false;
  private disposed = false;

  getSnapshot = () => this.snapshot;
  subscribe = (listener: () => void) => {
    this.listeners.add(listener);
    return () => {
      this.listeners.delete(listener);
    };
  };
  get(key: string): Composition {
    return this.entries.get(key)?.state ?? empty;
  }
  hasAttachments(runtimeId?: string, rootId?: string) {
    const prefix = runtimeId
      ? `${runtimeId}:${rootId ? rootId + ':' : ''}`
      : '';
    return [...this.entries].some(
      ([key, entry]) =>
        key.startsWith(prefix) && entry.state.attachments.length > 0,
    );
  }
  private entry(key: string): Entry {
    if (this.disposed) throw new Error('Composition store is closed');
    if (!key || new TextEncoder().encode(key).length > 512)
      throw new Error('Invalid composition identity');
    let entry = this.entries.get(key);
    if (!entry) {
      if (this.entries.size >= 32)
        throw new Error(
          'There are 32 unsent recipient drafts. Send or discard one before attaching more files.',
        );
      entry = { state: empty };
      this.entries.set(key, entry);
    }
    return entry;
  }
  private update(key: string, state: Composition) {
    const entry = this.entries.get(key);
    if (!entry) return;
    for (const item of entry.state.attachments) {
      if (item.previewUrl && !state.attachments.some(next => next.previewUrl === item.previewUrl))
        URL.revokeObjectURL(item.previewUrl);
    }
    entry.state = Object.freeze(state);
    if (!state.attachments.length && !state.sending) this.entries.delete(key);
    let attachmentCount = 0;
    let bytes = 0;
    for (const { state } of this.entries.values()) {
      attachmentCount += state.attachments.length;
      for (const item of state.attachments) bytes += item.size;
    }
    this.snapshot = Object.freeze({ attachmentCount, bytes });
    for (const listener of this.listeners) listener();
  }
  add(
    key: string,
    session: Session,
    runtimeId: string,
    agentId: string,
    files: readonly File[],
    surfaceId?: string,
  ): Promise<void> {
    if (
      key !== compositionKey(runtimeId, session.rootId, agentId, surfaceId) ||
      session.client.getSnapshot().info?.runtime_id !== runtimeId ||
      session.client.getSnapshot().state !== 'connected'
    )
      throw new Error(
        'Attachments must belong to the connected host and selected recipient',
      );
    return this.enqueue(key, files, session, runtimeId, agentId);
  }
  stage(key: string, files: readonly File[]): void {
    if (!key.startsWith('new:')) throw new Error('A new-session draft is required');
    void this.enqueue(key, files);
  }
  /** Transfer local files and their previews once, then upload in the real root scope. */
  adopt(key: string, session: Session, runtimeId: string): Promise<void> {
    const destination = compositionKey(runtimeId, session.rootId, session.rootId);
    if (!key.startsWith('new:') || this.entries.has(destination))
      throw new Error('Attachments require a new session destination');
    const entry = this.entries.get(key);
    if (!entry) return Promise.resolve();
    const jobs = [...this.jobs.values()].filter(job => job.key === key);
    this.entries.delete(key);
    this.entries.set(destination, entry);
    entry.submission = undefined;
    for (const job of jobs) {
      job.key = destination;
      job.session = session;
      job.runtimeId = runtimeId;
      job.agentId = session.rootId;
    }
    this.update(destination, {
      sending: false,
      attachments: Object.freeze(entry.state.attachments.map(item => Object.freeze({ ...item, staged: false }))),
    });
    void this.drain();
    return Promise.all(jobs.map(job => job.done)).then(() => {});
  }
  private enqueue(key: string, files: readonly File[], session?: Session, runtimeId = '', agentId = ''): Promise<void> {
    if (!files.length) return Promise.resolve();
    const prior = this.get(key);
    if (prior.sending)
      throw new Error('Wait for submission acceptance before adding files');
    if (prior.attachments.length + files.length > 16)
      throw new Error('Attach at most 16 files per draft.');
    if (
      files.some(
        (file) =>
          !Number.isSafeInteger(file.size) ||
          file.size < 0 ||
          file.name.length > 1024 ||
          file.type.length > 256,
      )
    )
      throw new Error('Invalid attachment size or filename');
    if (
      this.snapshot.bytes + files.reduce((sum, file) => sum + file.size, 0) >
      maxBytes
    )
      throw new Error(
        'Unsent attachments total at most 20 MiB across all drafts. Remove files before adding more.',
      );
    const entry = this.entry(key);
    const jobs = files.map((file) => {
      let settle!: () => void;
      const done = new Promise<void>((resolve) => {
        settle = resolve;
      });
      const job: Upload = {
        key,
        id: crypto.randomUUID(),
        runtimeId,
        agentId,
        session,
        file,
        abort: new AbortController(),
        done,
        settle,
      };
      this.jobs.set(job.id, job);
      return job;
    });
    this.update(key, {
      ...entry.state,
      attachments: Object.freeze([
        ...entry.state.attachments,
        ...jobs.map((job) =>
          Object.freeze({
            id: job.id,
            name: job.file!.name,
            size: job.file!.size,
            mediaType: job.file!.type,
            previewUrl: imagePreview(job.file!),
            ...(!session ? { staged: true } : {}),
          }),
        ),
      ]),
    });
    void this.drain();
    return Promise.all(jobs.map((job) => job.done)).then(() => {});
  }
  private async drain() {
    if (this.draining) return;
    this.draining = true;
    try {
      while (!this.disposed) {
        const job = [...this.jobs.values()].find(job => job.session);
        if (!job) break;
        const session = job.session;
        const file = job.file;
        job.file = undefined;
        try {
          job.abort.signal.throwIfAborted();
          if (!file || !session) continue;
          if (session.client.getSnapshot().info?.runtime_id !== job.runtimeId || session.client.getSnapshot().state !== 'connected')
            throw new Error('Reconnect to the attachment’s execution host and select the file again.');
          const kind = file.type.startsWith('image/') ? 'image' : 'text';
          if (kind === 'text' && file.size > 256 * 1024)
            throw new Error('Text attachments are limited to 256 KiB.');
          const bytes = new Uint8Array(await file.arrayBuffer());
          job.abort.signal.throwIfAborted();
          if (kind === 'text')
            new TextDecoder('utf-8', { fatal: true }).decode(bytes);
          const content = await session.client.upload(bytes, {
            rootId: session.rootId,
            agentId: job.agentId,
            mediaType: kind === 'image' ? file.type : 'text/plain',
            signal: job.abort.signal,
          });
          job.abort.signal.throwIfAborted();
          if (session.client.getSnapshot().info?.runtime_id !== job.runtimeId)
            throw new Error(
              'The attachment’s execution host changed. Select the file again.',
            );
          this.complete(job, { value: content.asAttachment(kind, file.name) });
        } catch (error) {
          if (!job.abort.signal.aborted)
            this.complete(job, {
              error: (error instanceof Error
                ? error.message
                : String(error)
              ).slice(0, 2048),
            });
        } finally {
          this.jobs.delete(job.id);
          job.file = undefined;
          job.session = undefined;
          job.settle();
        }
      }
    } finally {
      this.draining = false;
    }
  }
  private complete(
    job: Upload,
    outcome: Pick<CompositionAttachment, 'value' | 'error'>,
  ) {
    const current = this.get(job.key);
    if (!current.attachments.some((item) => item.id === job.id)) return;
    this.update(job.key, {
      ...current,
      attachments: Object.freeze(
        current.attachments.map((item) =>
          item.id === job.id ? Object.freeze({ ...item, ...outcome }) : item,
        ),
      ),
    });
  }
  private cancelUpload(id: string) {
    const job = this.jobs.get(id);
    if (!job) return;
    job.abort.abort();
    job.file = undefined;
    job.session = undefined;
    job.settle();
    this.jobs.delete(id);
  }
  remove(key: string, id: string) {
    this.clear(key, [id]);
  }
  clear(key: string, ids?: readonly string[]) {
    const current = this.get(key);
    const removing = new Set(ids ?? current.attachments.map((item) => item.id));
    for (const item of current.attachments) {
      if (!removing.has(item.id)) continue;
      this.cancelUpload(item.id);
    }
    this.update(key, {
      ...current,
      attachments: Object.freeze(
        current.attachments.filter((item) => !removing.has(item.id)),
      ),
    });
  }
  clearAll() {
    for (const key of this.entries.keys()) this.clear(key);
  }
  /** Forget all recipients after the owning root has been deleted. */
  clearSession(runtimeId: string, rootId: string, viewIds: readonly string[] = []) {
    const prefix = `${runtimeId}:${rootId}:`;
    for (const [key, entry] of this.entries) {
      if (!key.startsWith(prefix)) continue;
      for (const item of entry.state.attachments) this.cancelUpload(item.id);
      entry.submission = undefined;
      this.update(key, empty);
    }
    for (const viewId of viewIds) {
      const viewPrefix = `${runtimeId}:${viewId}:`;
      for (const key of this.selections.keys())
        if (key.startsWith(viewPrefix)) this.selections.delete(key);
    }
  }
  beginSubmission(key: string): symbol | undefined {
    const entry = this.entry(key);
    if (entry.state.sending) return;
    const token = Symbol('submission');
    entry.submission = token;
    this.update(key, { ...entry.state, sending: true });
    return token;
  }
  finishSubmission(key: string, token: symbol) {
    const entry = this.entries.get(key);
    if (entry?.submission !== token) return;
    entry.submission = undefined;
    this.update(key, { ...entry.state, sending: false });
  }
  rememberSelection(key: string, selection: ComposerSelection) {
    if (
      this.disposed ||
      key.length > 512 ||
      !Number.isSafeInteger(selection.start) ||
      !Number.isSafeInteger(selection.end) ||
      selection.start < 0 ||
      selection.end < selection.start
    )
      return;
    this.selections.delete(key);
    this.selections.set(
      key,
      Object.freeze({ start: selection.start, end: selection.end }),
    );
    if (this.selections.size > 128)
      this.selections.delete(this.selections.keys().next().value!);
  }
  selection(key: string) {
    return this.selections.get(key);
  }
  invalidateRuntime(runtimeId?: string) {
    for (const [key, entry] of this.entries) {
      if (runtimeId && !key.startsWith(runtimeId + ':')) continue;
      for (const item of entry.state.attachments) {
        this.cancelUpload(item.id);
      }
      entry.submission = undefined;
      this.update(key, {
        sending: false,
        attachments: Object.freeze(
          entry.state.attachments.map(({ id, name, size, mediaType }) =>
            Object.freeze({
              id,
              name,
              size,
              mediaType,
              error:
                'Attachment unavailable after changing hosts. Remove it and select the file again.',
            }),
          ),
        ),
      });
    }
  }
  dispose() {
    if (this.disposed) return;
    this.clearAll();
    this.entries.clear();
    this.selections.clear();
    this.disposed = true;
    this.listeners.clear();
  }
}
