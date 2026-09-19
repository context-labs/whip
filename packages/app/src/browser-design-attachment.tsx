import { useEffect, useRef, useState, type ReactNode } from 'react';
import { Button, CopyButton, Dialog } from '@whip/ui';
import { MousePointer2 } from 'lucide-react';
import * as stylex from '@stylexjs/stylex';
import { colors, scale, surface, typography } from '@whip/ui/tokens.stylex';

export interface BrowserDesignAttachmentProps {
  context: {
    elements: readonly { label: string; selector?: string }[];
    element_count: number;
    url?: string;
    title?: string;
  };
  screenshot?: ReactNode;
  rawText?: string;
  readContext?: (signal: AbortSignal) => Promise<string>;
  connected?: boolean;
}

export function BrowserDesignAttachment({ context, screenshot, rawText, readContext, connected = true }: BrowserDesignAttachmentProps) {
  const [open, setOpen] = useState(false);
  const trigger = useRef<HTMLButtonElement>(null);
  const labels = context.elements.slice(0, 2);
  const remaining = Math.max(0, context.element_count - labels.length);
  let path = context.url;
  try { if (path) path = new URL(path).pathname; } catch { /* Keep non-URL evidence as plain text. */ }

  return <>
    <div role="group" aria-label="Captured page context" {...stylex.props(styles.reference)}>
      {screenshot && <div {...stylex.props(styles.thumbnail)}>{screenshot}</div>}
      <Button ref={trigger} size="sm" variant="ghost" aria-label="View captured page context" aria-haspopup="dialog"
        xstyle={styles.summaryTrigger} onClick={() => setOpen(true)}>
        <span {...stylex.props(styles.summary)}>
          <span {...stylex.props(styles.labels)}>
            <MousePointer2 size={13} aria-hidden="true" {...stylex.props(styles.accent)} />
            {labels.length ? labels.map((element, index) => <span key={index} title={element.label} {...stylex.props(styles.label)}>{element.label}</span>) :
              <span>Page context</span>}
            {remaining > 0 && <span {...stylex.props(styles.quiet)}>+{remaining} elements</span>}
          </span>
          {path && <span title={context.url} {...stylex.props(styles.path)}>{path}</span>}
        </span>
        <span {...stylex.props(styles.quiet)}>Details</span>
      </Button>
    </div>
    <Dialog open={open} onOpenChange={setOpen} title="Captured page context" finalFocus={trigger}>
      {open && <div {...stylex.props(styles.details)}>
        <div>
          <div {...stylex.props(styles.pageTitle)}>{context.title || 'Captured page'}</div>
          {context.url && <div {...stylex.props(styles.url)}>{context.url}</div>}
        </div>
        {context.elements.length > 0 && <ul aria-label="Captured elements" {...stylex.props(styles.elements)}>
          {context.elements.map((element, index) => <li key={index} {...stylex.props(styles.element)}>
            <span>{element.label}</span>
            {element.selector && <code {...stylex.props(styles.selector)}>{element.selector}</code>}
          </li>)}
        </ul>}
        {context.element_count > context.elements.length && <div {...stylex.props(styles.quiet)}>
          Showing {context.elements.length} of {context.element_count} elements. Open raw context for the full captured evidence.
        </div>}
        <RawContext rawText={rawText} readContext={readContext} connected={connected} />
      </div>}
    </Dialog>
  </>;
}

function RawContext({ rawText, readContext, connected }: Pick<BrowserDesignAttachmentProps, 'rawText' | 'readContext' | 'connected'>) {
  const [loaded, setLoaded] = useState<string>();
  const [failed, setFailed] = useState(false);
  const [attempt, setAttempt] = useState(0);
  useEffect(() => {
    if (rawText !== undefined || !readContext || !connected) return;
    const controller = new AbortController();
    setFailed(false);
    void Promise.resolve().then(() => readContext(controller.signal)).then(text => {
      if (!controller.signal.aborted) setLoaded(text);
    }, () => {
      if (!controller.signal.aborted) setFailed(true);
    });
    return () => controller.abort();
  }, [rawText, readContext, connected, attempt]);
  const text = rawText ?? loaded;

  return <details {...stylex.props(styles.rawSection)}>
    <summary {...stylex.props(styles.rawSummary)}>Raw context</summary>
    {text !== undefined ? <>
      <div {...stylex.props(styles.copy)}><CopyButton text={text} label="Copy raw context" showLabel /></div>
      <pre {...stylex.props(styles.raw)}>{text}</pre>
    </> : !connected ? <p {...stylex.props(styles.quiet)}>Connect to the host to load raw context.</p> : failed ?
      <div role="alert" {...stylex.props(styles.failure)}>
        <span>Could not load raw context.</span>
        <Button size="sm" variant="ghost" onClick={() => setAttempt(value => value + 1)}>Retry</Button>
      </div> : <p role="status" {...stylex.props(styles.quiet)}>{readContext ? 'Loading raw context…' : 'Raw context unavailable.'}</p>}
  </details>;
}

const styles = stylex.create({
  reference: { display: 'flex', alignItems: 'center', gap: scale.space2, minWidth: 0, paddingBlock: scale.space2, fontFamily: typography.sans, fontSize: typography.size13 },
  thumbnail: { flexShrink: 0 },
  summaryTrigger: { flex: 1, minWidth: 0, textAlign: 'left', whiteSpace: 'normal', gap: scale.space3 },
  summary: { display: 'flex', flexDirection: 'column', gap: scale.space1, flex: 1, minWidth: 0 },
  labels: { display: 'flex', alignItems: 'center', flexWrap: 'wrap', gap: scale.space2, minWidth: 0 },
  label: { maxWidth: 'min(220px, 100%)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', fontWeight: 500 },
  accent: { color: colors.primary, flexShrink: 0 },
  quiet: { color: surface.secondaryText },
  path: { color: surface.secondaryText, fontSize: typography.size12, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' },
  details: { display: 'flex', flexDirection: 'column', gap: scale.space4, minWidth: 0 },
  pageTitle: { fontWeight: 500, overflowWrap: 'anywhere' },
  url: { color: surface.secondaryText, overflowWrap: 'anywhere', marginTop: scale.space1 },
  elements: { listStyle: 'none', margin: 0, padding: 0, display: 'flex', flexDirection: 'column', gap: scale.space3 },
  element: { display: 'flex', flexDirection: 'column', gap: scale.space1, overflowWrap: 'anywhere' },
  selector: { fontFamily: typography.mono, fontSize: typography.codeSize, color: surface.secondaryText, whiteSpace: 'pre-wrap' },
  rawSection: { borderTopWidth: 1, borderTopStyle: 'solid', borderTopColor: surface.quietBorder, paddingTop: scale.space3 },
  rawSummary: { color: surface.secondaryText, cursor: 'pointer' },
  copy: { display: 'flex', justifyContent: 'flex-end', marginTop: scale.space2 },
  raw: { maxHeight: '40vh', overflow: 'auto', overscrollBehavior: 'contain', whiteSpace: 'pre-wrap', overflowWrap: 'anywhere', fontFamily: typography.mono, fontSize: typography.codeSize, margin: 0, paddingBlock: scale.space2 },
  failure: { display: 'flex', alignItems: 'center', gap: scale.space2, color: colors.error, marginTop: scale.space2 },
});
