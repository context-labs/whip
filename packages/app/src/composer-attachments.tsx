import { useCallback, useRef, useState } from 'react';
import { Button, Dialog, IconButton, Spinner } from '@whip/ui';
import { Paperclip, X } from 'lucide-react';
import * as stylex from '@stylexjs/stylex';
import { colors, scale, surface, typography } from '@whip/ui/tokens.stylex';
import type { CompositionAttachment } from './compositions';
import { ErrorNotice } from './error-feedback';
import { layout } from './styles';

export function ComposerAttachments({ attachments, owner, onRemove, disabled = false }: {
  attachments: readonly CompositionAttachment[];
  owner: string;
  onRemove: (id: string) => void;
  disabled?: boolean;
}) {
  if (!attachments.length) return null;
  return <div role="group" aria-label="Message attachments" {...stylex.props(styles.attachments)}>
    {attachments.map(item => {
      const image = item.mediaType?.startsWith('image/') || item.value?.kind === 'image';
      return <div key={item.id} {...stylex.props(image ? styles.imageItem : styles.fileItem)}>
        {image ? <ImagePreview item={item} disabled={disabled} onRemove={() => onRemove(item.id)} /> :
          <div {...stylex.props(layout.row)}>
            <Paperclip size={13} />
            <span {...stylex.props(layout.grow, styles.filename)}>
              {item.name} · {item.error ? 'Upload failed' : item.value || item.staged ? 'Ready' : 'Uploading…'}
            </span>
            <IconButton label={`Remove ${item.name}`} disabled={disabled} onClick={() => onRemove(item.id)}><X size={13} /></IconButton>
          </div>}
      </div>;
    })}
    {attachments.filter(item => item.error).map(item => <div key={`error:${item.id}`} {...stylex.props(styles.fileItem)}>
      <ErrorNotice type="resource" owner={`${owner}:${item.id}`} title={`${item.name} could not upload`} error={item.error} />
    </div>)}
  </div>;
}

function ImagePreview({ item, onRemove, disabled }: { item: CompositionAttachment; onRemove: () => void; disabled: boolean }) {
  const [loaded, setLoaded] = useState<string>();
  const [failed, setFailed] = useState<string>();
  const [open, setOpen] = useState(false);
  const trigger = useRef<HTMLButtonElement>(null);
  const url = item.previewUrl;
  const unavailable = !url || failed === url;
  const decoding = !unavailable && loaded !== url;
  const uploading = !item.staged && !item.value && !item.error;
  const measure = useCallback((element: HTMLImageElement | null) => {
    if (element?.complete && element.naturalWidth > 0) setLoaded(url);
  }, [url]);
  return <>
    <div aria-busy={decoding || uploading || undefined} {...stylex.props(styles.tile)}>
      <Button ref={trigger} type="button" xstyle={styles.thumbnail} disabled={unavailable}
        aria-label={`Preview ${item.name}`} aria-haspopup="dialog"
        title={`${item.name}${item.error ? ' · Upload failed' : uploading ? ' · Uploading' : ''}`}
        onClick={() => setOpen(true)}>
        {unavailable ? <span>Preview unavailable</span> : <>
          <img ref={measure} src={url} alt={item.name} decoding="async" loading="lazy"
            onLoad={() => setLoaded(url)} onError={() => setFailed(url)}
            {...stylex.props(styles.image, decoding && styles.loading)} />
          {decoding && <Spinner label={`Loading preview of ${item.name}`} size={16} />}
        </>}
      </Button>
      {!decoding && uploading && <span {...stylex.props(styles.status)}>
        <Spinner label={`Uploading ${item.name}`} size={14} />
      </span>}
      {item.error && <span role="status" {...stylex.props(styles.status, styles.failed)}>Upload failed</span>}
      <IconButton label={`Remove ${item.name}`} xstyle={styles.remove} disabled={disabled} onClick={onRemove}>
        <X size={14} {...stylex.props(styles.removeIcon)} />
      </IconButton>
    </div>
    <Dialog open={open && !unavailable} onOpenChange={setOpen} title={item.name}
      finalFocus={trigger} xstyle={styles.dialog}>
      <img src={url} alt={item.name} {...stylex.props(styles.original)} />
    </Dialog>
  </>;
}

const styles = stylex.create({
  attachments: {
    display: 'flex', flexWrap: 'wrap', alignItems: 'flex-start', gap: 12,
    maxHeight: 'min(264px, 32vh)', overflowY: 'auto', overscrollBehavior: 'contain',
    padding: 4,
  },
  imageItem: { width: 120, flexShrink: 0 },
  fileItem: { width: '100%', minWidth: 0 },
  filename: { overflowWrap: 'anywhere' },
  tile: { position: 'relative', width: 120, height: 120 },
  thumbnail: {
    position: 'relative', width: '100%', height: '100%', padding: 0,
    borderRadius: 12, borderColor: colors.border, overflow: 'hidden',
    backgroundColor: { default: 'transparent', ':hover': 'transparent' },
    color: surface.secondaryText, fontSize: typography.size12, whiteSpace: 'normal',
  },
  image: { position: 'absolute', inset: 0, width: '100%', height: '100%', objectFit: 'cover' },
  loading: { visibility: 'hidden' },
  status: {
    position: 'absolute', bottom: 6, left: 6, display: 'flex', padding: 4,
    borderRadius: 6, backgroundColor: colors.background, color: surface.secondaryText,
    fontSize: typography.size11, pointerEvents: 'none',
  },
  failed: { color: colors.error },
  remove: {
    position: 'absolute', top: 0, right: 0, borderRadius: 12,
    width: { default: 24, [scale.touch]: 44 }, height: { default: 24, [scale.touch]: 44 },
    minHeight: 0, padding: 0, borderWidth: 0, backgroundColor: { default: 'transparent', ':hover': 'transparent' },
  },
  removeIcon: { boxSizing: 'border-box', width: 22, height: 22, padding: 4, borderRadius: '50%', backgroundColor: colors.background },
  dialog: { width: 'min(960px, 90vw)' },
  original: { display: 'block', width: '100%', height: 'auto', maxHeight: '75vh', objectFit: 'contain' },
});
