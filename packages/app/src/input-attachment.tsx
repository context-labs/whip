import { useEffect, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import type { WhipClient } from '@whip/sdk';
import type { ContentHandle } from '@whip/protocol';
import { Button, Dialog, Spinner } from '@whip/ui';
import { Paperclip } from 'lucide-react';
import * as stylex from '@stylexjs/stylex';
import { colors } from '@whip/ui/tokens.stylex';
import { ErrorNotice } from './error-feedback';

export function InputAttachment({ client, rootId, runtimeId, agentId, file, name, image, connected, compact = false }: {
  client: WhipClient; rootId: string; runtimeId: string; agentId: string; file: ContentHandle; name: string; image: boolean; connected: boolean;
  compact?: boolean;
}) {
  const [open, setOpen] = useState(false);
  const [url, setURL] = useState<string>();
  const [loaded, setLoaded] = useState(false);
  const [failed, setFailed] = useState(false);
  const query = useQuery({
    queryKey: ['queued-attachment', runtimeId, rootId, agentId, file.reference_id, file.digest],
    enabled: connected && (image || open), staleTime: Infinity, gcTime: 0, retry: false, networkMode: 'always', refetchOnWindowFocus: false,
    queryFn: ({ signal }) => client.content(file, { rootId, agentId }).readBytes({ maxBytes: 20 << 20, signal }),
  });
  useEffect(() => {
    if (!image || !query.data) return;
    const objectURL = URL.createObjectURL(new Blob([query.data], { type: file.media_type }));
    setURL(objectURL); setLoaded(false); setFailed(false);
    return () => URL.revokeObjectURL(objectURL);
  }, [image, query.data, file.media_type]);
  const unavailable = !connected && !loaded || failed || !!query.error;
  const retry = <Button type="button" disabled={!connected} onClick={() => void query.refetch()}>Retry</Button>;
  const error = <ErrorNotice type="resource" owner={file.reference_id} error={query.error} action={retry} />;
  return <div {...stylex.props(compact && styles.compactAttachment)}>
    <Button type="button" variant="ghost" aria-label={`Preview ${name}`} title={name} disabled={image && !loaded} onClick={() => setOpen(true)} xstyle={image ? [styles.thumbnail, compact && styles.smallThumbnail] : undefined}>
      {!image && <><Paperclip size={14} />{name}</>}
      {image && url && !failed && <img src={url} alt={name} decoding="async" onLoad={() => setLoaded(true)} onError={() => setFailed(true)} {...stylex.props(styles.image)} />}
      {image && unavailable && <span aria-label="Image unavailable">{compact ? <Paperclip size={14} /> : 'Image unavailable'}</span>}
      {image && !unavailable && !loaded && <span {...stylex.props(styles.loader)}><Spinner label={`Loading ${name}`} size={14} /></span>}
    </Button>
    {!compact && !open && error}
    {!compact && failed && retry}
    <Dialog open={open} onOpenChange={setOpen} title={name}>
      {error}
      {image && url && <img src={url} alt={name} {...stylex.props(styles.original)} />}
      {!image && query.data && <pre {...stylex.props(styles.body)}>{new TextDecoder().decode(query.data)}</pre>}
      {!query.data && !query.error && (connected ? <Spinner label={`Loading ${name}`} /> : <p>Reconnect to load this attachment.</p>)}
    </Dialog>
  </div>;
}


const styles = stylex.create({
  body: { whiteSpace: 'pre-wrap', overflowWrap: 'anywhere', maxHeight: '50dvh', overflowY: 'auto', margin: 0 },
  thumbnail: { width: 80, height: 80, padding: 0, overflow: 'hidden', position: 'relative', borderRadius: 12, borderWidth: 1, borderStyle: 'solid', borderColor: colors.border, backgroundColor: colors.background },
  compactAttachment: { flexShrink: 0, display: 'flex' },
  smallThumbnail: { width: 28, height: 28, minHeight: 28, borderRadius: 6 },
  loader: { position: 'absolute', inset: 0, display: 'grid', placeItems: 'center' },
  image: { position: 'absolute', inset: 0, width: '100%', height: '100%', objectFit: 'cover' },
  original: { display: 'block', maxWidth: '100%', maxHeight: '75dvh', objectFit: 'contain' },
});
