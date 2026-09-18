import { useEffect, useLayoutEffect, useRef, useState, useSyncExternalStore, type ReactNode } from 'react';
import { useNavigate } from '@tanstack/react-router';
import { AlertDialog, Button, IconButton, Input, Menu, Tooltip } from '@whip/ui';
import { ArrowLeft, ArrowRight, Globe, MoreHorizontal, RotateCw, Search, Server, X } from 'lucide-react';
import * as stylex from '@stylexjs/stylex';
import { colors, scale, surface, typography } from '@whip/ui/tokens.stylex';
import { selectedSessionTab, type BrowserTab } from './session-tabs';
import type { BrowserAction } from './browser-types';
import { browserAddress } from './browser-address';
import { useRuntime } from './context';
import { BrowserPreviewControls } from './browser-preview-controls';
import { tabDestination } from './session-tab-routing';

export function BrowserView({ tab, attachmentControls }: { tab: BrowserTab; attachmentControls?: ReactNode }) {
  const runtime = useRuntime(), browser = runtime.browser, navigate = useNavigate();
  const inventory = useSyncExternalStore(browser.subscribe, browser.getSnapshot, browser.getSnapshot);
  const state = inventory.tabs.find(item => item.id === tab.id);
  const [address, setAddress] = useState(tab.url === 'about:blank' ? '' : tab.url);
  const editing = useRef(false), addressInput = useRef<HTMLInputElement>(null), findInput = useRef<HTMLInputElement>(null);
  const viewport = useRef<HTMLDivElement>(null);
  const [error, setError] = useState('');
  const [findOpen, setFindOpen] = useState(false), [query, setQuery] = useState(''), [matches, setMatches] = useState('');
  const [clear, setClear] = useState(false);
  const available = !!runtime.platform.browser && state?.status !== 'unavailable' && state?.status !== 'crashed';
  const act = (action: BrowserAction) => { setError(''); void browser.act(tab.id, action).catch(error => setError(error instanceof Error ? error.message : String(error))); };
  useEffect(() => { if (!editing.current) setAddress(tab.url === 'about:blank' ? '' : tab.url); }, [tab.url]);
  useLayoutEffect(() => {
    if (available && viewport.current) return browser.register(tab.id, viewport.current);
  }, [browser, tab.id, available]);
  const focusAddress = () => { addressInput.current?.focus(); addressInput.current?.select(); };
  const openFind = () => { setFindOpen(true); requestAnimationFrame(() => findInput.current?.focus()); };
  useEffect(() => browser.onEvent(event => {
    if (event.kind === 'snapshot' || event.tabId !== tab.id || event.generation !== browser.target(tab.id)?.generation) return;
    if (event.kind === 'focused') {
      if (selectedSessionTab(runtime.tabs.workspace())?.id === tab.id) return;
      runtime.tabs.activate(tab.id); void navigate(tabDestination(tab));
    }
    else if (event.kind === 'find') setMatches(event.matches ? `${event.activeMatchOrdinal} of ${event.matches}` : 'No matches');
    else if (event.kind === 'shortcut') {
      if (event.shortcut === 'address') focusAddress();
      else if (event.shortcut === 'find') openFind();
      else if (event.shortcut === 'reload' || event.shortcut === 'back' || event.shortcut === 'forward') act({ kind: event.shortcut });
      else if (event.shortcut === 'zoom-reset') act({ kind: 'zoom', factor: 1 });
      else if (event.shortcut === 'zoom-in' || event.shortcut === 'zoom-out') act({ kind: 'zoom', factor: Math.max(.25, Math.min(5, (state?.zoomFactor ?? 1) + (event.shortcut === 'zoom-in' ? .1 : -.1))) });
    }
  }), [browser, tab.id, navigate, runtime, state?.zoomFactor]);
  const submit = async () => {
    try {
      const url = browserAddress(address); editing.current = false; setError(''); setAddress(url === 'about:blank' ? '' : url);
      await browser.act(tab.id, { kind: 'navigate', url });
      if (document.activeElement === addressInput.current) await browser.act(tab.id, { kind: 'focus' });
    }
    catch (error) { setError(error instanceof Error ? error.message : String(error)); }
  };
  return <section aria-label={`Browser: ${tab.titleHint || tab.url}`} {...stylex.props(styles.root)} onKeyDown={event => {
    if (!(event.metaKey || event.ctrlKey) || event.altKey || event.nativeEvent.isComposing) return;
    if (event.key.toLowerCase() === 'l') { event.preventDefault(); focusAddress(); }
    else if (event.key.toLowerCase() === 'f') { event.preventDefault(); openFind(); }
    else if (event.key.toLowerCase() === 'r') { event.preventDefault(); act({ kind: 'reload' }); }
  }}>
    <div role="group" aria-label="Browser toolbar" {...stylex.props(styles.toolbar)}>
      <form aria-label="Browser navigation" {...stylex.props(styles.navigation)} onSubmit={event => { event.preventDefault(); void submit(); }}>
        <IconButton type="button" variant="ghost" label="Back" disabled={!state?.canGoBack} onClick={() => act({ kind: 'back' })}><ArrowLeft size={15}/></IconButton>
        <IconButton type="button" variant="ghost" label="Forward" disabled={!state?.canGoForward} onClick={() => act({ kind: 'forward' })}><ArrowRight size={15}/></IconButton>
        <IconButton type="button" variant="ghost" label={state?.loading ? 'Stop loading' : 'Reload page'} disabled={!runtime.platform.browser} onClick={() => act({ kind: state?.loading ? 'stop' : 'reload' })}>{state?.loading ? <X size={15}/> : <RotateCw size={15}/>}</IconButton>
        <div {...stylex.props(styles.location)}>
          <Tooltip label={tab.environmentId ? 'SSH preview' : 'This Mac'}><span role="img" aria-label={tab.environmentId ? 'SSH preview' : 'This Mac'} {...stylex.props(styles.provenance)}>{tab.environmentId ? <Server size={14} aria-hidden="true"/> : <Globe size={14} aria-hidden="true"/>}</span></Tooltip>
          <Input ref={addressInput} aria-label="Browser address" placeholder="Enter a web address" value={address} autoComplete="off" spellCheck={false} xstyle={[styles.address, styles.locationInput]}
          readOnly={!runtime.platform.browser} onFocus={() => { editing.current = true; }} onBlur={() => { editing.current = false; }} onChange={event => setAddress(event.target.value)}
          onKeyDown={event => { if (event.key === 'Escape') { editing.current = false; setAddress(tab.url === 'about:blank' ? '' : tab.url); addressInput.current?.blur(); if (available) act({ kind: 'focus' }); } }}/>
        </div>
      </form>
      <BrowserPreviewControls tabId={tab.id}/>
      {attachmentControls}
      <Menu trigger={<IconButton type="button" variant="ghost" label="Browser actions"><MoreHorizontal size={16}/></IconButton>} items={[
        { id: 'find', label: 'Find in page', disabled: !available, onSelect: openFind },
        { id: 'copy', label: 'Copy address', onSelect: () => { void runtime.platform.copy(tab.url).catch(error => setError(String(error))); } },
        { id: 'external', label: 'Open in default browser', disabled: !!tab.environmentId || tab.url === 'about:blank', onSelect: () => { void runtime.platform.openExternal(tab.url).catch(error => setError(String(error))); } },
        { id: 'zoom-in', label: 'Zoom in', disabled: !available, onSelect: () => act({ kind: 'zoom', factor: Math.min(5, (state?.zoomFactor ?? 1) + .1) }) },
        { id: 'zoom-out', label: 'Zoom out', disabled: !available, onSelect: () => act({ kind: 'zoom', factor: Math.max(.25, (state?.zoomFactor ?? 1) - .1) }) },
        { id: 'zoom-reset', label: `Reset zoom (${Math.round((state?.zoomFactor ?? 1) * 100)}%)`, disabled: !available, onSelect: () => act({ kind: 'zoom', factor: 1 }) },
        { id: 'devtools', label: 'Page developer tools', disabled: !available, onSelect: () => act({ kind: 'devtools', open: true }) },
        { id: 'clear', label: 'Clear browser profile data…', disabled: !runtime.platform.browser, onSelect: () => setClear(true) },
        { id: 'forget', label: 'Forget closed Browser addresses', onSelect: () => runtime.tabs.forgetBrowserHistory() },
      ]}/>
    </div>
    {state?.loading && <span role="status" {...stylex.props(styles.srOnly)}>Loading…</span>}
    {findOpen && <form aria-label="Find in page" {...stylex.props(styles.toolbar)} onSubmit={event => { event.preventDefault(); act({ kind: 'find', text: query, findNext: true }); }}>
      <Search size={14} aria-hidden="true"/><Input ref={findInput} aria-label="Find text" value={query} xstyle={styles.address} onChange={event => { setQuery(event.target.value); if (event.target.value) act({ kind: 'find', text: event.target.value }); else act({ kind: 'stop-find', action: 'clear' }); }}/>
      <span role="status" {...stylex.props(styles.findStatus)}>{matches}</span>
      <IconButton type="button" label="Previous match" disabled={!query} onClick={() => act({ kind: 'find', text: query, findNext: true, forward: false })}><ArrowLeft size={14}/></IconButton>
      <IconButton type="submit" label="Next match" disabled={!query}><ArrowRight size={14}/></IconButton>
      <IconButton type="button" label="Close find" onClick={() => { setFindOpen(false); act({ kind: 'stop-find', action: 'clear' }); act({ kind: 'focus' }); }}><X size={14}/></IconButton>
    </form>}
    {(error || state?.error) && <div role="alert" {...stylex.props(styles.notice)}>{error || state?.error?.message}</div>}
    {available ? <div ref={viewport} data-browser-viewport={tab.id} {...stylex.props(styles.viewport)}/> : <div {...stylex.props(styles.unavailable)}>
      <p>{!runtime.platform.browser ? 'Browser pages are available in the desktop app. This saved address is preserved.' : state?.status === 'crashed' ? 'This page stopped responding.' : 'This Browser page is unavailable.'}</p>
      {tab.environmentId && <p>The SSH preview environment must be connected and authorized. This address will not open on This Mac.</p>}
      {runtime.platform.browser && <Button onClick={() => act({ kind: 'reload' })}>Retry</Button>}
      <Button variant="secondary" onClick={() => { void runtime.platform.copy(tab.url).catch(error => setError(String(error))); }}>Copy address</Button>
    </div>}
    <AlertDialog open={clear} onOpenChange={setClear} title="Clear browser profile data?" description="Clears website cookies, logins and storage in this browsing profile. Other tabs in this profile are affected. App credentials and saved servers are not cleared." confirmLabel="Clear site data" danger onConfirm={() => { act({ kind: 'clear-profile' }); setClear(false); }}/>
  </section>;
}
const styles = stylex.create({
  root: { display: 'flex', flexDirection: 'column', height: '100%', minWidth: 0, minHeight: 0, color: colors.foreground, backgroundColor: colors.background },
  toolbar: { display: 'flex', alignItems: 'center', flexWrap: 'wrap', flexShrink: 0, gap: scale.space1, padding: scale.space2, minWidth: 0, borderBottomWidth: 1, borderBottomStyle: 'solid', borderBottomColor: surface.quietBorder },
  navigation: { display: 'flex', alignItems: 'center', flex: '1 1 0', gap: scale.space1, minWidth: 'min-content' },
  location: { display: 'flex', alignItems: 'center', flex: '1 1 0', minWidth: 80, borderRadius: scale.radiusControl, backgroundColor: { default: 'transparent', ':focus-within': colors.panel } },
  address: { flex: '1 1 0', minWidth: 0, width: 0, fontSize: typography.size12 },
  locationInput: { backgroundColor: { default: 'transparent', ':hover': colors.hover, ':focus': colors.panel }, borderColor: { default: 'transparent', ':focus': surface.secondaryText } },
  provenance: { display: 'flex', alignItems: 'center', flexShrink: 0, paddingInline: scale.space1, color: surface.secondaryText },
  srOnly: { position: 'absolute', width: 1, height: 1, padding: 0, margin: -1, overflow: 'hidden', clipPath: 'inset(50%)', whiteSpace: 'nowrap' },
  viewport: { flex: '1 1 auto', minHeight: 80, minWidth: 0, overflow: 'hidden' },
  notice: { padding: scale.space2, fontSize: typography.size12, color: colors.foreground, overflowWrap: 'anywhere' },
  unavailable: { flex: 1, padding: scale.space4, overflow: 'auto', fontSize: typography.size13 },
  findStatus: { fontSize: typography.size11, whiteSpace: 'nowrap' },
});
