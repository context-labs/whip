import * as stylex from '@stylexjs/stylex';
import {useEffect, useLayoutEffect, useMemo, useRef, useState} from 'react';
import type {ReactNode} from 'react';
import {Button, CopyButton, type Styled} from './actions';
import {CodeBlock} from './code-block';
import {Dialog} from './overlays';
import {useTheme} from './themes';
import {browserSurfaces} from './theme-contrast';
import {colors, surface, typography} from './tokens.stylex';
import {renderMermaid, validateMermaid, MermaidRenderError} from './mermaid-renderer';

export type MermaidView = 'diagram' | 'source';
export interface MermaidBlockProps extends Styled {
  code: string;
  /** A live response stays source-only, even if its current fence parses. */
  live?: boolean;
  /** Clipped source must never be represented as a complete diagram. */
  truncated?: boolean;
  renderText?(text: string, offset: number): ReactNode;
  view?: MermaidView;
  onViewChange?(view: MermaidView): void;
}
const styles = stylex.create({
  root: {minWidth: 0, margin: 0, borderWidth: 1, borderStyle: 'solid', borderColor: surface.quietBorder, borderRadius: 8, overflow: 'hidden'},
  header: {display: 'flex', alignItems: 'center', justifyContent: 'space-between', flexWrap: 'wrap', gap: 4, paddingBlock: 6, paddingInline: 12, backgroundColor: colors.panel},
  label: {color: surface.secondaryText, fontFamily: typography.sans, fontSize: typography.size11, marginInlineEnd: 8},
  actions: {display: 'flex', alignItems: 'center', flexWrap: 'wrap', gap: 4, minWidth: 0},
  source: {borderWidth: 0, borderRadius: 0},
  copy: {minWidth: '10em', fontSize: typography.size12},
  pending: {minHeight: 160, backgroundColor: colors.background},
  status: {margin: 0, paddingBlock: 7, paddingInline: 12, backgroundColor: colors.panel, color: surface.secondaryText, fontFamily: typography.sans, fontSize: typography.size12, lineHeight: '1.5'},
  viewport: {padding: 16, overflow: 'auto', maxHeight: 520, backgroundColor: colors.background},
  image: {display: 'block', maxWidth: 'none', marginInline: 'auto'},
  dialog: {width: 'min(1200px, calc(100vw - 24px))', maxHeight: '90dvh', padding: 16, gap: 12, overflow: 'hidden'},
  dialogBody: {minHeight: 0, overflow: 'hidden'},
  expanded: {maxHeight: '65dvh', minHeight: 100, padding: 8},
  fit: {maxWidth: '100%', maxHeight: '60dvh', width: 'auto', height: 'auto'},
});
const typeLabels = {flowchart: 'Flowchart', state: 'State diagram', sequence: 'Sequence diagram', class: 'Class diagram', er: 'Entity relationship', xychart: 'XY chart'};
type DiagramImage = {source: string; url: string; width: number; height: number};

/** A diagram is only ever an isolated image; source remains the copy authority. */
export function MermaidBlock({code, live = false, truncated = false, renderText, view, onViewChange, xstyle}: MermaidBlockProps) {
  const {resolvedTheme, display, increasedContrast} = useTheme();
  const palette = useMemo(() => {
    const roles = browserSurfaces(resolvedTheme, increasedContrast);
    return {
      background: resolvedTheme.colors.background, foreground: resolvedTheme.colors.foreground,
      surface: resolvedTheme.colors.element, accent: resolvedTheme.colors.accent,
      muted: roles.secondaryText, line: roles.controlBorder, border: roles.controlBorder,
      font: display.uiFont, fontSize: display.uiSize,
    };
  }, [resolvedTheme, increasedContrast, display.uiFont, display.uiSize]);
  const validation = useMemo(() => validateMermaid(code), [code]);
  const [localView, setLocalView] = useState<MermaidView>('diagram');
  const [image, setImage] = useState<DiagramImage | null>(null);
  const [failure, setFailure] = useState<{source: string; message: string; retry: boolean} | null>(null);
  const [attempt, setAttempt] = useState(0);
  const [open, setOpen] = useState(false);
  const [fit, setFit] = useState(true);
  const source = useRef<HTMLDivElement>(null);
  const expand = useRef<HTMLButtonElement>(null);
  const onViewChangeRef = useRef(onViewChange);
  const urls = useRef(new Set<string>());
  useLayoutEffect(() => {onViewChangeRef.current = onViewChange;});
  // Keep the previous image until its replacement has decoded and committed.
  useLayoutEffect(() => {
    for (const url of urls.current) if (url !== image?.url) {URL.revokeObjectURL(url); urls.current.delete(url);}
  }, [image]);
  useEffect(() => () => {for (const url of urls.current) URL.revokeObjectURL(url); urls.current.clear();}, []);
  useEffect(() => {
    setFailure(null);
    if (live || truncated || !validation.ok) {setImage(null); return;}
    const controller = new AbortController();
    let pendingURL: string | undefined;
    void renderMermaid(code, palette, {signal: controller.signal}).then(async result => {
      if (controller.signal.aborted) return;
      pendingURL = URL.createObjectURL(new Blob([result.svg], {type: 'image/svg+xml'}));
      urls.current.add(pendingURL);
      const decoded = new Image();
      decoded.src = pendingURL;
      await decoded.decode();
      if (controller.signal.aborted) return;
      // An automatic settlement must not take away the reader's focused/selected source.
      const selection = window.getSelection();
      if (source.current && (source.current.contains(document.activeElement) ||
          (selection && !selection.isCollapsed && Array.from({length: selection.rangeCount}, (_, index) => selection.getRangeAt(index)).some(range => range.intersectsNode(source.current!))))) {
        setLocalView('source');
        onViewChangeRef.current?.('source');
      }
      setImage({source: code, url: pendingURL, width: result.width, height: result.height});
      pendingURL = undefined;
    }).catch(error => {
      if (controller.signal.aborted) return;
      if (pendingURL) {URL.revokeObjectURL(pendingURL); urls.current.delete(pendingURL); pendingURL = undefined;}
      setImage(null);
      setFailure({source: code, message: error instanceof MermaidRenderError ? error.message : "Couldn't load this diagram. Showing source.",
        retry: !(error instanceof MermaidRenderError) || ['unavailable', 'busy', 'timeout'].includes(error.reason)});
    });
    return () => {
      controller.abort();
      if (pendingURL) {URL.revokeObjectURL(pendingURL); urls.current.delete(pendingURL);}
    };
  }, [code, palette, live, truncated, validation, attempt]);
  const current = !live && !truncated && validation.ok && image?.source === code ? image : null;
  const error = failure?.source === code ? failure : null;
  const selectedView = view ?? localView;
  const showDiagram = selectedView === 'diagram' && current !== null;
  const label = validation.ok ? typeLabels[validation.type] : 'Diagram';
  const status = live ? 'Diagram will render when this response finishes.'
    : truncated ? 'This diagram source is incomplete. Showing source.'
    : !validation.ok ? `${validation.message}${validation.reason === 'unsupported-type' ? ' Supported families: flowchart, state, sequence, class, entity relationship, and XY charts.' : ''}`
    : error ? error.message
    : !current ? 'Rendering diagram…'
    : null;
  const changeView = (next: MermaidView) => {setLocalView(next); onViewChange?.(next);};
  const diagram = (expanded: boolean) => current && <img src={current.url} alt={`Mermaid ${label.toLowerCase()}. Original source is available in Source view.`} width={current.width} height={current.height} {...stylex.props(styles.image, expanded && fit && styles.fit)} />;
  return <figure {...stylex.props(styles.root, xstyle)} data-mermaid-view={showDiagram ? 'diagram' : 'source'}>
    <figcaption {...stylex.props(styles.header)}>
      <span {...stylex.props(styles.label)}>Mermaid · {label}</span>
      <div {...stylex.props(styles.actions)}>
        <div role="group" aria-label="Mermaid view" {...stylex.props(styles.actions)}>
          <Button size="sm" variant={showDiagram ? 'secondary' : 'ghost'} aria-pressed={showDiagram} disabled={!current} onClick={() => changeView('diagram')}>Diagram</Button>
          <Button size="sm" variant={!showDiagram ? 'secondary' : 'ghost'} aria-pressed={!showDiagram} onClick={() => changeView('source')}>Source</Button>
        </div>
        <CopyButton text={code} label="Copy source" showLabel showError xstyle={styles.copy} />
        <Button ref={expand} size="sm" variant="ghost" disabled={!current} onClick={() => {setFit(true); setOpen(true);}}>Expand</Button>
        {error?.retry && <Button size="sm" variant="ghost" onClick={() => setAttempt(value => value + 1)}>Retry</Button>}
      </div>
    </figcaption>
    {status && <p role="status" {...stylex.props(styles.status)}>{status}</p>}
    {showDiagram ? <div role="region" aria-label="Mermaid diagram" tabIndex={0} {...stylex.props(styles.viewport)}>{diagram(false)}</div>
      : <div ref={source} {...stylex.props(!live && !truncated && validation.ok && !error && !current && styles.pending)}><CodeBlock code={code} label="Mermaid source" truncated={truncated} renderText={renderText} hideHeader xstyle={styles.source} /></div>}
    <Dialog open={open} onOpenChange={setOpen} title={`Mermaid · ${label}`} description="Scroll to explore at 100%, or fit the diagram to this window. Source is available in the conversation." finalFocus={expand} xstyle={styles.dialog} bodyXstyle={styles.dialogBody}>
      <div role="group" aria-label="Diagram size" {...stylex.props(styles.actions)}>
        <Button size="sm" aria-pressed={fit} variant={fit ? 'secondary' : 'ghost'} onClick={() => setFit(true)}>Fit</Button>
        <Button size="sm" aria-pressed={!fit} variant={!fit ? 'secondary' : 'ghost'} onClick={() => setFit(false)}>100%</Button>
        <CopyButton text={code} label="Copy source" showLabel showError xstyle={styles.copy} />
      </div>
      {current ? <div role="region" aria-label="Expanded Mermaid diagram" tabIndex={0} {...stylex.props(styles.viewport, styles.expanded)}>{diagram(true)}</div>
        : <p role="status" {...stylex.props(styles.status)}>{status ?? 'Diagram unavailable. Close this window to read the source.'}</p>}
    </Dialog>
  </figure>;
}
