import { useEffect, useRef, useState } from 'react';
import type { ContentReference, Session } from '@whip/sdk';
import type { DeepReadonly } from '@whip/sdk/state';
import { Button, CodeBlock } from '@whip/ui';
import * as stylex from '@stylexjs/stylex';
import { ErrorNotice } from '../error-feedback';
import { useRuntime } from '../context';
import { layout } from '../styles';

export function ContentRead({ session, connected, reference, label = 'Content', inline }: {
  session: Session;
  connected: boolean;
  reference?: string | DeepReadonly<ContentReference>;
  label?: string;
  inline?: string;
}) {
  const runtime = useRuntime();
  const controller = useRef<AbortController | null>(null);
  const [body, setBody] = useState<string>();
  const [image, setImage] = useState<Blob>();
  const [imageURL, setImageURL] = useState<string>();
  const [operation, setOperation] = useState<'resource' | 'action'>('resource');
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const id = typeof reference === 'string' ? reference : reference?.id;
  useEffect(() => {
    controller.current?.abort(); setBody(undefined); setImage(undefined); setBusy(false); setError('');
    return () => controller.current?.abort();
  }, [session.client, session.id, id]);
  useEffect(() => { if (!connected) { controller.current?.abort(); setImage(undefined); setBusy(false); } }, [connected]);
  useEffect(() => {
    if (!image) { setImageURL(undefined); return; }
    const url = URL.createObjectURL(image); setImageURL(url);
    return () => URL.revokeObjectURL(url);
  }, [image]);
  async function read(download: boolean) {
    if (!connected || !reference) return;
    controller.current?.abort();
    const current = new AbortController(); controller.current = current;
    const signal = current.signal;
    setBusy(true); setError(''); setOperation(download ? 'action' : 'resource');
    try {
      const metadata = typeof reference === 'string' ? await session.content.get(reference, { signal }) : reference;
      const previewImage = ['image/png', 'image/jpeg', 'image/webp', 'image/gif'].includes(metadata.media_type);
      const data = await session.content.readBytes(metadata, { maxBytes: download || previewImage ? 4 << 20 : 1 << 20, signal });
      signal.throwIfAborted();
      if (download) await runtime.platform.download(data, 'whip-content', metadata.media_type);
      else if (previewImage) { setBody(undefined); setImage(new Blob([data], { type: metadata.media_type })); }
      else setBody(new TextDecoder('utf-8', { fatal: true }).decode(data));
    } catch (failure) {
      if (!signal.aborted) setError(failure instanceof Error ? failure.message : String(failure));
    } finally { if (!signal.aborted) setBusy(false); }
  }
  return <div {...stylex.props(layout.column)}>
    {imageURL && <img src={imageURL} alt={label} {...stylex.props(styles.image)} onError={() => { setImage(undefined); setError('Image could not be decoded'); }} />}
    {(body ?? inline) !== undefined && <CodeBlock label={label} code={body ?? inline ?? ''} maxBytes={128 << 10} />}
    {reference && <>
      <span {...stylex.props(layout.muted)}>{label}{typeof reference !== 'string' && ` · ${reference.size} bytes`}</span>
      <div {...stylex.props(layout.row, layout.wrap)}>
        <Button variant="ghost" size="sm" disabled={busy || !connected} onClick={() => void read(false)}>Read {label.toLowerCase()}</Button>
        <Button variant="ghost" size="sm" disabled={busy || !connected} onClick={() => void read(true)}>Download</Button>
      </div>
    </>}
    {error && (connected || operation === 'action') && <ErrorNotice type={operation} owner={`${session.client.runtimeID}:${session.id}:${id ?? label}`} title={`Could not ${operation === 'action' ? 'download' : 'load'} ${label.toLowerCase()}`} error={error} />}
  </div>;
}

const styles = stylex.create({
  image: { display: 'block', maxWidth: '100%', maxHeight: 480, objectFit: 'contain' },
});
