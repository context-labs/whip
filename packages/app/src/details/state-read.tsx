import { useEffect, useRef, useState } from 'react';
import type { Session, StateVersion } from '@whip/sdk';
import { Button, CodeBlock } from '@whip/ui';
import { useRuntime } from '../context';
import { ErrorNotice } from '../error-feedback';

/** An explicit immutable-version read, bounded independently of the query cache. */
export async function readStateBytes(
  session: Session,
  version: StateVersion,
  maxBytes: number,
  signal: AbortSignal,
) {
  if (version.session_id !== null && version.session_id !== session.id)
    throw new TypeError('Shared value belongs to another session');
  if (BigInt(version.size) > BigInt(maxBytes))
    throw new RangeError(`Shared value exceeds the ${maxBytes >> 20} MiB read limit`);
  const bytes = new Uint8Array(Number(version.size));
  for (let offset = 0; offset < bytes.length;) {
    signal.throwIfAborted();
    const length = Math.min(65536, bytes.length - offset);
    const part = await session.client.call(
      'state.read',
      { session_id: session.id, version_id: version.id, offset: String(offset), length },
      { signal },
    );
    if (
      part.offset !== String(offset) ||
      part.version.id !== version.id ||
      part.version.revision !== version.revision ||
      part.version.key !== version.key ||
      part.version.author_id !== version.author_id ||
      part.version.digest !== version.digest ||
      part.version.size !== version.size ||
      part.version.tree_id !== version.tree_id ||
      part.version.session_id !== version.session_id
    )
      throw new TypeError('Shared value identity changed');
    const binary = atob(part.data_base64);
    if (btoa(binary) !== part.data_base64 || binary.length !== length)
      throw new TypeError('Shared value page is incomplete');
    bytes.set(
      Uint8Array.from(binary, (character) => character.charCodeAt(0)),
      offset,
    );
    offset += length;
  }
  const digest = [...new Uint8Array(await crypto.subtle.digest('SHA-256', bytes))]
    .map((value) => value.toString(16).padStart(2, '0'))
    .join('');
  signal.throwIfAborted();
  if (digest !== version.digest) throw new TypeError('Shared value digest mismatch');
  return bytes;
}
export function StateRead({
  session,
  version,
  connected,
}: {
  session: Session;
  version: StateVersion;
  connected: boolean;
}) {
  const runtime = useRuntime(),
    controller = useRef<AbortController | null>(null);
  const [body, setBody] = useState<string>(),
    [error, setError] = useState(''),
    [busy, setBusy] = useState(false);
  useEffect(() => {
    controller.current?.abort();
    setBody(undefined);
    setError('');
    setBusy(false);
    return () => controller.current?.abort();
  }, [session.client, session.id, version.id]);
  useEffect(() => {
    if (!connected) {
      controller.current?.abort();
      setBusy(false);
    }
  }, [connected]);
  async function read(download: boolean) {
    if (!connected || busy) return;
    const current = new AbortController();
    controller.current = current;
    setBusy(true);
    setError('');
    try {
      const bytes = await readStateBytes(
        session,
        version,
        download ? 64 << 20 : 1 << 20,
        current.signal,
      );
      if (download)
        await runtime.platform.download(bytes, 'whip-shared-value', 'application/octet-stream');
      else setBody(new TextDecoder('utf-8', { fatal: true }).decode(bytes));
    } catch (failure) {
      if (!current.signal.aborted)
        setError(failure instanceof Error ? failure.message : String(failure));
    } finally {
      if (!current.signal.aborted) setBusy(false);
    }
  }
  return (
    <>
      <Button variant="ghost" disabled={!connected || busy} onClick={() => void read(false)}>
        Read shared value
      </Button>
      <Button variant="ghost" disabled={!connected || busy} onClick={() => void read(true)}>
        Download shared value
      </Button>
      {body !== undefined && <CodeBlock code={body} label="Shared value" maxBytes={128 << 10} />}
      {error && <ErrorNotice type="resource" owner={`${session.id}:${version.id}`} error={error} />}
    </>
  );
}
