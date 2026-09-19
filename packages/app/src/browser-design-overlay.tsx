import { useEffect, useId, useLayoutEffect, useRef, useState } from 'react';
import * as stylex from '@stylexjs/stylex';
import { Button, Checkbox, Select, UIProvider, applyTheme, applyDisplayPreferences, defaultDisplayPreferences, themeCatalog } from '@whip/ui';
import { validateTheme, validateDisplayPreferences } from '@whip/ui/theme-data';
import { appearance, colors, scale, surface, typography } from '@whip/ui/tokens.stylex';
import { ArrowUp, Crosshair, X } from 'lucide-react';
import type { BrowserDesignBounds, BrowserDesignElement, BrowserDesignBridge, BrowserDesignColor, BrowserDesignIntent, BrowserDesignModel, BrowserDesignState } from './browser-design-types';
import { browserDesignLimits } from './browser-design-types';
import { boundedDesignText, designComposerBounds, designHoverBounds, designHoverLabel, designHoverMotion, designRevision } from './browser-design-geometry';

declare global { interface Window { whipBrowserDesign?: BrowserDesignBridge } }

/** Isolated renderer entry: deliberately does not import app context, runtime, or SDK. */
export function BrowserDesignOverlayRoot() {
  const [model, setModel] = useState<BrowserDesignModel>();
  const [error, setError] = useState('');
  const bridge = window.whipBrowserDesign;
  useEffect(() => {
    if (!bridge) return;
    let alive = true, received = false;
    const off = bridge.onModel(next => { received = true; if (alive) setModel(next); });
    void bridge.snapshot().then(next => { if (alive && !received) setModel(next); }).catch(error => { if (alive) setError(String(error)); });
    return () => { alive = false; off(); };
  }, [bridge]);
  // Native hover snapshots clone the model; only appearance changes rewrite tokens.
  const appearance = JSON.stringify(model?.draft.theme);
  useLayoutEffect(() => {
    if (!appearance) return;
    try {
      const projected = JSON.parse(appearance) as BrowserDesignModel['draft']['theme'];
      const theme = projected.palette ? validateTheme(projected.palette) : themeCatalog.find(item => item.id === projected.name) ?? themeCatalog.find(item => item.dark === (projected.mode === 'dark'));
      const display = projected.display ? validateDisplayPreferences(projected.display) : defaultDisplayPreferences;
      applyDisplayPreferences(display);
      if (theme) applyTheme(theme, document.documentElement, display.contrast === 'more');
    } catch (error) { setError(String(error)); }
    // This renderer is a transparent native sibling, not an opaque application page.
    document.documentElement.style.backgroundColor = 'transparent';
  }, [appearance]);
  if (!model || !bridge) return error ? <p role="alert">{error}</p> : null;
  return <UIProvider><BrowserDesignOverlay model={model} onIntent={intent => {
    void bridge.intent({ revision: designRevision(model.state), intent }).then(() => { if (intent.kind !== 'hover') setError(''); }).catch(error => { if (intent.kind !== 'hover') setError(String(error)); });
  }} bridgeError={error}/></UIProvider>;
}

export function BrowserDesignOverlay({ model, onIntent, bridgeError }: { model: BrowserDesignModel; onIntent(intent: BrowserDesignIntent): void; bridgeError?: string }) {
  const { state, draft } = model;
  const hoverTipId = useId();
  const previousHover = useRef<{ state: BrowserDesignState; animated: boolean } | undefined>(undefined);
  const animateHover = draft.theme.display?.motion !== 'reduce' && designHoverMotion(previousHover.current?.state, state, previousHover.current?.animated ?? false);
  // Commit history, not renders: abandoned renders must not become animation origins.
  useLayoutEffect(() => { previousHover.current = { state, animated: animateHover }; });
  const hover = state.status === 'active' ? state.hover : undefined;
  // Native hover IDs differ from selection IDs. Suppress only a visually coincident border.
  const coincidentSelection = hover && state.elements.some(({ bounds }) =>
    bounds.x === hover.bounds.x && bounds.y === hover.bounds.y && bounds.width === hover.bounds.width && bounds.height === hover.bounds.height);
  const composer = useRef<HTMLFormElement>(null), input = useRef<HTMLTextAreaElement>(null), picker = useRef<HTMLDivElement>(null);
  const [height, setHeight] = useState(240), [preview, setPreview] = useState(false), [submitting, setSubmitting] = useState(false);
  const [prompt, setPrompt] = useState(draft.prompt);
  const editingPrompt = useRef(false), promptReset = useRef(draft.promptReset ?? 0);
  const hoverFrame = useRef<number | undefined>(undefined);
  const pointer = useRef({ x: 0, y: 0 });
  const menuOpen = useRef(false), menuEscape = useRef(false), dismissingMenu = useRef(false);
  const appearance = JSON.stringify(draft.theme);
  useLayoutEffect(() => {
    const resize = () => {
      if (!input.current) return;
      input.current.style.height = '0px';
      input.current.style.height = `${Math.max(78, Math.min(220, input.current.scrollHeight))}px`;
    };
    resize();
    // Root appearance projection happens in a parent layout effect.
    const frame = requestAnimationFrame(resize);
    return () => cancelAnimationFrame(frame);
  }, [prompt, state.viewport.width, state.elements.length > 0, appearance]);
  useEffect(() => {
    // Local authored text owns this renderer until an explicit admission reset.
    // Matching text is not an acknowledgement: A → AB → A can have older A echoes.
    if (!editingPrompt.current || promptReset.current !== (draft.promptReset ?? 0)) {
      promptReset.current = draft.promptReset ?? 0;
      editingPrompt.current = false; setPrompt(draft.prompt); setSubmitting(false);
    }
  }, [draft.prompt, draft.promptReset]);
  useEffect(() => () => { if (hoverFrame.current !== undefined) cancelAnimationFrame(hoverFrame.current); }, []);
  useLayoutEffect(() => {
    if (!composer.current) return;
    const observer = new ResizeObserver(() => { if (composer.current) setHeight(composer.current.scrollHeight + 2); });
    observer.observe(composer.current); return () => observer.disconnect();
  }, [state.elements.length > 0, preview]);
  const previousCount = useRef(0);
  useEffect(() => { if (!previousCount.current && state.elements.length) input.current?.focus(); else if (!state.elements.length) picker.current?.focus(); previousCount.current = state.elements.length; }, [state.elements.length]);
  const bounds = designComposerBounds(state, height);
  useEffect(() => { if (draft.busy || draft.error || bridgeError) setSubmitting(false); }, [draft.busy, draft.error, bridgeError]);
  const blocked = draft.busy || draft.uncertain || submitting;
  const canSend = !blocked && state.status === 'active' && !!state.elements.length && !!prompt.trim() && draft.recipients.some(item => item.id === draft.recipientId && item.available);
  const send = () => {
    if (!canSend) return;
    setSubmitting(true);
    // Flush current authored text before Send on the ordered narrow IPC channel.
    onIntent({ kind: 'prompt', value: prompt }); onIntent({ kind: 'send' });
  };
  const editing = (target: EventTarget) => target instanceof HTMLElement && !!target.closest('form');
  return <main aria-label="Browser Design Mode" tabIndex={0} {...stylex.props(styles.root)} onKeyDownCapture={event => {
    if (event.key === 'Escape') menuEscape.current = menuOpen.current;
  }} onKeyDown={event => {
    // Portals retain this React ancestry: let the dropdown consume its own Escape.
    if (event.nativeEvent.isComposing || event.defaultPrevented || menuOpen.current || (event.key === 'Escape' && menuEscape.current)) return;
    if (event.key === 'Escape') { event.preventDefault(); if (preview) { setPreview(false); onIntent({ kind: 'evidence-close' }); } else onIntent({ kind: 'stop' }); }
    else if (!editing(event.target) && event.key === 'Backspace') { event.preventDefault(); onIntent({ kind: 'ancestor' }); }
  }}>
    <div ref={picker} role="button" tabIndex={0} aria-label="Pick an element" aria-describedby={hover ? hoverTipId : undefined} aria-description="Tab or arrow keys inspect elements. Enter selects. Shift Enter adds. Up arrow selects the ancestor." {...stylex.props(styles.picker)} onKeyDown={event => {
      if (event.nativeEvent.isComposing || blocked || menuOpen.current) return;
      if (event.key === 'Tab' || event.key === 'ArrowRight' || event.key === 'ArrowLeft') { event.preventDefault(); onIntent({ kind: event.shiftKey || event.key === 'ArrowLeft' ? 'previous' : 'next' }); }
      else if (event.key === 'Enter' || event.key === ' ') { event.preventDefault(); onIntent({ kind: 'pick-hover', additive: event.shiftKey }); }
      else if (event.key === 'ArrowUp') { event.preventDefault(); onIntent({ kind: 'ancestor' }); }
    }} onPointerMove={event => {
      if (preview || blocked || menuOpen.current) return;
      pointer.current = { x: event.clientX, y: event.clientY };
      if (hoverFrame.current !== undefined) return;
      hoverFrame.current = requestAnimationFrame(() => { hoverFrame.current = undefined; if (!menuOpen.current) onIntent({ kind: 'hover', ...pointer.current }); });
    }} onPointerDown={event => {
      dismissingMenu.current = menuOpen.current;
      if (!menuOpen.current) { event.preventDefault(); picker.current?.focus({ preventScroll: true }); }
    }} onClick={event => {
      if (!blocked && !menuOpen.current && !dismissingMenu.current) onIntent({ kind: 'pick', x: event.clientX, y: event.clientY, additive: event.shiftKey || event.metaKey || event.ctrlKey });
      dismissingMenu.current = false;
    }} onWheel={event => {
      if (menuOpen.current) return;
      onIntent({ kind: 'scroll', x: event.clientX, y: event.clientY, deltaX: Math.max(-1000, Math.min(1000, event.deltaX)), deltaY: Math.max(-1000, Math.min(1000, event.deltaY)) });
    }}/>
    {state.elements.map(element => <div key={element.id} {...stylex.props(styles.outline, palette[element.color])} style={{ left: element.bounds.x, top: element.bounds.y, width: element.bounds.width, height: element.bounds.height }}><span {...stylex.props(styles.marker)}>{element.number}</span></div>)}
    {hover && <><div data-design-hover="" {...stylex.props(styles.outline, styles.hover, animateHover && styles.hoverMotion, coincidentSelection && styles.hiddenOutline)} style={{ left: hover.bounds.x, top: hover.bounds.y, width: hover.bounds.width, height: hover.bounds.height }}/><DesignHoverTip id={hoverTipId} hover={hover} viewport={state.viewport} obstacles={[...((state.elements.length || draft.prompt || draft.error || state.error) ? [bounds] : []), ...state.elements.map(element => ({ ...element.bounds, width: 28, height: 28 }))]}/></>}
    {!state.elements.length && <div {...stylex.props(styles.hint)}><Crosshair size={14}/><span>Select an element to describe a change</span><button type="button" aria-label="Exit Design Mode" {...stylex.props(styles.icon)} onClick={() => onIntent({ kind: 'stop' })}><X size={14}/></button></div>}
    {(state.elements.length > 0 || draft.prompt || draft.error || state.error) && <form ref={composer} aria-label="Describe a design change" {...stylex.props(styles.composer)} style={{ left: bounds.x, top: bounds.y, width: bounds.width, maxHeight: state.viewport.height - 24 }} onSubmit={event => { event.preventDefault(); send(); }}>
      <div {...stylex.props(styles.chips)}>{state.elements.map(element => <button key={element.id} type="button" disabled={blocked} aria-label={`Remove element ${element.number}: ${element.label}`} {...stylex.props(styles.chip, palette[element.color])} onClick={() => onIntent({ kind: 'remove', id: element.id })}><b>{element.number}</b><span {...stylex.props(styles.truncate)}>{element.label}</span><X size={12}/></button>)}<button type="button" aria-label="Exit Design Mode" {...stylex.props(styles.icon, styles.close)} onClick={() => onIntent({ kind: 'stop' })}><X size={14}/></button></div>
      <textarea ref={input} aria-label="Describe the change" placeholder="Describe the change…" value={prompt} disabled={blocked} maxLength={browserDesignLimits.prompt} {...stylex.props(styles.prompt)} onChange={event => { const value = boundedDesignText(event.target.value, browserDesignLimits.prompt); editingPrompt.current = true; setPrompt(value); onIntent({ kind: 'prompt', value }); }} onKeyDown={event => { if (event.key === 'Enter' && (event.metaKey || event.ctrlKey) && !event.nativeEvent.isComposing) { event.preventDefault(); send(); } }}/>
      <div {...stylex.props(styles.destination)}>
        <span>To</span>
        <Select label="Conversation" placeholder="Choose a conversation…" value={draft.recipientId || null} disabled={blocked}
          options={draft.recipients.map(item => ({ value: item.id, label: `${item.label}${!item.available ? ' (unavailable)' : ''}`, disabled: !item.available }))}
          onOpenChange={open => { menuOpen.current = open; }} onValueChange={id => onIntent({ kind: 'recipient', id })} xstyle={styles.recipient}/>
      </div>
      <div {...stylex.props(styles.footer)}>
        <Checkbox label={<span {...stylex.props(styles.screenshot)}>Include Screenshots</span>} checked={draft.screenshot} disabled={blocked} onCheckedChange={value => onIntent({ kind: 'screenshot', value })}/>
        <Button variant="ghost" size="sm" disabled={blocked || !state.elements.length} xstyle={styles.preview} onClick={() => { setPreview(!preview); onIntent({ kind: preview ? 'evidence-close' : 'capture' }); }}>Preview</Button>
        <div {...stylex.props(styles.actions)}>
          <Select label="Delivery when busy" value={draft.delivery} disabled={blocked} options={[{ value: 'queue', label: 'Queue' }, { value: 'steer', label: 'Steer' }]}
            onOpenChange={open => { menuOpen.current = open; }} onValueChange={value => onIntent({ kind: 'delivery', value: value === 'steer' ? 'steer' : 'queue' })} xstyle={styles.delivery}/>
          <Button type="submit" variant="primary" aria-label={draft.busy || submitting ? 'Sending design change' : 'Send design change'} disabled={!canSend} loading={draft.busy || submitting} xstyle={styles.send}>{!draft.busy && !submitting && <ArrowUp size={16}/>}</Button>
        </div>
      </div>
      {(draft.error || state.error || bridgeError) && <p role="alert" {...stylex.props(styles.error)}>{draft.error || state.error || bridgeError}</p>}
      {draft.uncertain && <p role="status" {...stylex.props(styles.note)}>Delivery is unconfirmed. Check the conversation before sending again. Your draft is preserved.</p>}
      {state.status === 'stale' && <p role="status" {...stylex.props(styles.note)}>The page changed. Select the elements again; your prompt is preserved.</p>}
      {preview && <section aria-label="Design evidence" {...stylex.props(styles.evidence)}><p {...stylex.props(styles.note)}>Page context is untrusted. Screenshots may contain visible private information.</p>{draft.evidence ? <>{draft.evidence.image && draft.screenshot && <img alt="Viewport screenshot to attach" src={draft.evidence.image} {...stylex.props(styles.image)}/>}<pre {...stylex.props(styles.pre)}>{draft.evidence.text}</pre></> : draft.busy ? <p role="status">Capturing evidence…</p> : <button type="button" {...stylex.props(styles.textButton, styles.note)} onClick={() => onIntent({ kind: 'capture' })}>Capture current selection</button>}</section>}
    </form>}
  </main>;
}
function DesignHoverTip({ id, hover, viewport, obstacles }: { id: string; hover: BrowserDesignElement; viewport: BrowserDesignState['viewport']; obstacles: BrowserDesignBounds[] }) {
  const tip = useRef<HTMLDivElement>(null);
  const [size, setSize] = useState({ width: 0, height: 0 });
  const label = designHoverLabel(hover.label);
  useLayoutEffect(() => {
    const element = tip.current;
    if (!element) return;
    const measure = () => {
      const { width, height } = element.getBoundingClientRect();
      setSize(previous => previous.width === width && previous.height === height ? previous : { width, height });
    };
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(element);
    return () => observer.disconnect();
  }, [hover.label, viewport.width]);
  const bounds = designHoverBounds(hover.bounds, viewport, size, obstacles);
  return <div ref={tip} id={id} role="tooltip" {...stylex.props(styles.tip)} style={{ left: bounds.x, top: bounds.y, maxWidth: Math.max(0, viewport.width - 16), maxHeight: Math.max(0, viewport.height - 16), visibility: size.height ? 'visible' : 'hidden' }}>
    <div {...stylex.props(styles.tipIdentity)}>
      <span {...stylex.props(styles.tipKind)}>{label.kind}</span>
      {label.description && <span {...stylex.props(styles.tipDescription)}>{label.description}</span>}
    </div>
    <span {...stylex.props(styles.tipInstruction)}>Click to select · Shift-click to add</span>
  </div>;
}

const palette = stylex.create({ blue: { color: colors.primary }, purple: { color: colors.info }, green: { color: colors.success }, orange: { color: colors.warning } }) satisfies Record<BrowserDesignColor, unknown>;
const styles = stylex.create({
  root: { position: 'fixed', inset: 0, fontFamily: typography.sans, fontSize: typography.size13, color: colors.foreground, backgroundColor: 'transparent', overflow: 'hidden' },
  picker: { position: 'absolute', inset: 0, cursor: 'crosshair', userSelect: 'none' },
  outline: { position: 'absolute', pointerEvents: 'none', borderWidth: 2, borderStyle: 'solid', borderColor: 'currentColor', backgroundColor: 'color-mix(in srgb, currentColor 7%, transparent)' },
  hover: { color: colors.primary, borderWidth: 1, backgroundColor: 'transparent', transitionProperty: 'none' },
  hoverMotion: { transitionProperty: { default: 'left, top, width, height', [scale.reducedMotion]: 'none' }, transitionDuration: { default: appearance.motionFast, [scale.reducedMotion]: '0ms' }, transitionTimingFunction: 'ease-out' },
  hiddenOutline: { visibility: 'hidden' },
  marker: { position: 'absolute', top: 2, left: 2, fontSize: typography.size10, fontWeight: 600, lineHeight: 1.5, minWidth: 18, paddingInline: 3, textAlign: 'center', backgroundColor: colors.panel, borderWidth: 1, borderStyle: 'solid', borderColor: 'currentColor', borderRadius: 4, userSelect: 'none' },
  tip: { position: 'absolute', pointerEvents: 'none', userSelect: 'none', display: 'flex', flexDirection: 'column', gap: 4, width: 'max-content', padding: '8px 10px', borderRadius: scale.radiusControl, borderWidth: 1, borderStyle: 'solid', borderColor: surface.quietBorder, backgroundColor: colors.panel, color: colors.foreground, lineHeight: 1.4, overflow: 'hidden', boxShadow: '0 2px 8px rgb(0 0 0 / .16)' },
  tipIdentity: { display: 'flex', alignItems: 'baseline', gap: 8, minWidth: 0, maxWidth: '24em', fontSize: typography.size12 },
  tipKind: { fontFamily: typography.mono, fontSize: typography.size11, color: surface.secondaryText, flexShrink: 0, maxWidth: '40%', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' },
  tipDescription: { minWidth: 0, fontWeight: 500, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' },
  tipInstruction: { fontSize: typography.size11, color: surface.secondaryText, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' },
  hint: { position: 'absolute', left: 12, bottom: 12, display: 'flex', alignItems: 'center', gap: 8, maxWidth: 'calc(100% - 24px)', padding: '8px 10px', borderRadius: scale.radiusControl, backgroundColor: colors.panel, boxShadow: '0 2px 12px rgb(0 0 0 / .15)' },
  composer: { position: 'absolute', display: 'flex', flexDirection: 'column', backgroundColor: colors.element, borderWidth: 1, borderStyle: 'solid', borderColor: surface.quietBorder, borderRadius: 20, boxShadow: '0 4px 16px rgb(0 0 0 / .12)', overflowY: 'auto', overscrollBehavior: 'contain' },
  chips: { display: 'flex', gap: 6, flexWrap: 'wrap', padding: '12px 12px 0', alignItems: 'center' },
  chip: { display: 'inline-flex', alignItems: 'center', gap: 5, maxWidth: 230, fontFamily: typography.mono, fontSize: typography.size11, padding: '3px 6px', borderRadius: 4, borderWidth: 1, borderStyle: 'solid', borderColor: 'color-mix(in srgb, currentColor 40%, transparent)', backgroundColor: 'color-mix(in srgb, currentColor 8%, transparent)', cursor: 'pointer', opacity: { default: 1, ':disabled': .5 } },
  truncate: { overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' },
  close: { marginLeft: 'auto' },
  icon: { display: 'inline-flex', justifyContent: 'center', alignItems: 'center', width: 24, height: 24, flexShrink: 0, backgroundColor: { default: 'transparent', ':hover': colors.hover }, borderWidth: 0, borderRadius: 4, color: surface.secondaryText, cursor: 'pointer' },
  prompt: { width: '100%', minHeight: 78, maxHeight: 220, flexShrink: 0, resize: 'none', overflowY: 'auto', borderWidth: 0, backgroundColor: 'transparent', color: colors.foreground, fontFamily: typography.sans, fontSize: typography.size14, lineHeight: 1.5, padding: 12, outline: 'none' },
  destination: { display: 'flex', alignItems: 'center', gap: 6, padding: '0 12px 8px', color: surface.secondaryText, fontSize: typography.size12 },
  recipient: { minWidth: 0, flex: 1, justifyContent: 'space-between', textAlign: 'left', fontSize: typography.size12, minHeight: 28, padding: '4px 8px', borderColor: 'transparent', backgroundColor: { default: 'transparent', ':hover': colors.hover } },
  footer: { display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 4, padding: '8px 12px', borderTopWidth: 1, borderTopStyle: 'solid', borderTopColor: surface.quietBorder },
  screenshot: { display: 'block', fontSize: typography.size11, color: surface.secondaryText, whiteSpace: 'nowrap' },
  preview: { color: surface.secondaryText, fontSize: typography.size11, paddingInline: 6 },
  textButton: { padding: 0, color: surface.secondaryText, backgroundColor: 'transparent', borderWidth: 0, fontSize: typography.size11, cursor: 'pointer' },
  actions: { marginLeft: 'auto', display: 'flex', alignItems: 'center', gap: 4 },
  delivery: { minWidth: 0, minHeight: 28, padding: '4px 6px', borderColor: 'transparent', backgroundColor: { default: 'transparent', ':hover': colors.hover }, color: surface.secondaryText, fontSize: typography.size11 },
  send: { borderRadius: '50%', width: 32, height: 32, minHeight: 32, padding: 0, flexShrink: 0 },
  error: { margin: '0 12px 10px', fontSize: typography.size12, color: colors.error },
  note: { margin: '8px 12px', fontSize: typography.size11, color: surface.secondaryText, lineHeight: 1.5 },
  evidence: { borderTopWidth: 1, borderTopStyle: 'solid', borderTopColor: surface.quietBorder },
  image: { display: 'block', width: 'calc(100% - 24px)', margin: 12, borderRadius: 4 },
  pre: { whiteSpace: 'pre-wrap', overflowWrap: 'anywhere', maxHeight: 180, overflow: 'auto', margin: 12, fontFamily: typography.mono, fontSize: typography.size10 },
});
