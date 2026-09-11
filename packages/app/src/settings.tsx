import { useEffect, useRef, useState } from 'react';
import { useNavigate } from '@tanstack/react-router';
import { ArrowLeft, ChevronDown, Settings2, Palette, Cable, Bot, Network, LifeBuoy, Info, Search } from 'lucide-react';
import { Button, Input, Sheet } from '@whip/ui';
import * as stylex from '@stylexjs/stylex';
import { colors, surface, typography, scale } from '@whip/ui/tokens.stylex';
import { useAppState, useRuntime } from './context';
import { HostSelector } from './host-selector';
import { layout } from './styles';
import { AppearanceSettings } from './settings/appearance';
import { GeneralSettings, AboutSettings, ProvidersSettings, ExecutionSettings, RecoverySettings, ConnectionsSettings, AgentsSettings } from './settings/sections';
import { SettingsEditsProvider } from './settings/unsaved';
import { settingsCategories, searchSettings, settingsBackDestination, type SettingsSearch, type SettingsSection } from './settings/navigation';

const icons = { general: Settings2, appearance: Palette, providers: Cable, execution: Bot, connections: Network, recovery: LifeBuoy, about: Info };

export function Settings(props: SettingsSearch) {
  return <SettingsEditsProvider><SettingsLayout {...props} /></SettingsEditsProvider>;
}
function SettingsLayout({ section = 'appearance', host: target, setting }: SettingsSearch) {
  const runtime = useRuntime();
  const { hosts, home, profilesReady } = useAppState();
  const navigate = useNavigate();
  const [query, setQuery] = useState('');
  const [navigation, setNavigation] = useState(false);
  const content = useRef<HTMLElement>(null);
  const previousSection = useRef(section);
  const category = settingsCategories.find(item => item.id === section)!;
  const preferredRuntime = useRef(runtime.lastSession()?.runtimeId);
  const implicitTarget = useRef<string>(undefined);
  const suggestedHost = hosts.find(item => item.runtimeId === preferredRuntime.current) ?? home;
  if (!implicitTarget.current && suggestedHost && (!preferredRuntime.current || suggestedHost.runtimeId === preferredRuntime.current || profilesReady)) implicitTarget.current = suggestedHost.id;
  const selectedId = target ?? implicitTarget.current;
  const observedHost = hosts.find(item => item.id === selectedId);
  const selectedKey = `${section}:${selectedId ?? ''}`;
  const retained = useRef({ key: selectedKey, host: observedHost });
  if (retained.current.key !== selectedKey) retained.current = { key: selectedKey, host: observedHost };
  const identityChanged = !!retained.current.host?.runtimeId && !!observedHost?.runtimeId && retained.current.host.runtimeId !== observedHost.runtimeId;
  if (!identityChanged && observedHost?.client) retained.current.host = observedHost;
  const host = identityChanged || !observedHost?.client ? retained.current.host ?? observedHost : observedHost;
  const enabled = !identityChanged && !!observedHost?.client && observedHost.state === 'connected';
  const hostSection = section === 'providers' || section === 'execution' || section === 'recovery';
  const results = searchSettings(query, !!runtime.platform.notify, !!runtime.platform.updates);
  const navigateSection = (next: SettingsSection, anchor?: string) => {
    void navigate({ to: '/settings', search: { section: next, ...(target ? { host: target } : {}), ...(anchor ? { setting: anchor } : {}) }, replace: true });
    setQuery(''); setNavigation(false);
  };
  const back = () => {
    const focusId = runtime.settingsReturn?.focusId;
    void navigate(settingsBackDestination(runtime)).then(() => requestAnimationFrame(() => {
      if (document.querySelector('[data-settings-layout]')) return;
      const destination = (focusId ? document.getElementById(focusId) : undefined) ?? document.querySelector<HTMLElement>('[data-whip-composer], #whip-settings-link');
      destination?.focus({ preventScroll: true });
    }));
  };
  useEffect(() => {
    if (previousSection.current !== section) { content.current?.scrollTo?.({ top: 0 }); previousSection.current = section; }
    if (!setting) return;
    const focus = () => {
      const row = document.getElementById(setting);
      if (!row || !content.current?.contains(row)) return false;
      for (let parent = row.parentElement; parent && parent !== content.current; parent = parent.parentElement) {
        if (parent instanceof HTMLDetailsElement) parent.open = true;
      }
      row.scrollIntoView?.({ block: 'center' });
      (row.querySelector<HTMLElement>('input:not(:disabled), select:not(:disabled), [role="slider"]:not([aria-disabled="true"]), [role="switch"]:not([aria-disabled="true"]), [role="combobox"]:not(:disabled)') ?? row.querySelector<HTMLElement>('button:not(:disabled), [tabindex="0"]') ?? row).focus({ preventScroll: true });
      return true;
    };
    if (focus()) return;
    const observer = new MutationObserver(() => { if (focus()) observer.disconnect(); });
    if (content.current) observer.observe(content.current, { childList: true, subtree: true });
    return () => observer.disconnect();
  }, [section, setting, target]);
  const sidebar = <>
    <div {...stylex.props(styles.search)}><Search size={15} aria-hidden /><Input
      aria-label="Search settings" type="search" placeholder="Search settings" value={query}
      onChange={event => setQuery(event.target.value.slice(0, 200))}
      onKeyDown={event => {
        if (event.key === 'Escape') setQuery('');
        if (event.key === 'ArrowDown') { event.preventDefault(); event.currentTarget.closest('[data-settings-navigation]')?.querySelector<HTMLElement>('[data-settings-result]')?.focus(); }
      }} xstyle={styles.searchInput} /></div>
    <nav aria-label={query.trim() ? 'Settings search results' : 'Settings categories'} {...stylex.props(styles.navigation)}>
      {query.trim() ? <>
        <span role="status" {...stylex.props(styles.resultsCount)}>{results.length} {results.length === 1 ? 'setting' : 'settings'} found</span>
        {results.map(result => <button key={result.id} data-settings-result type="button" {...stylex.props(styles.navItem, styles.result)}
          onClick={() => navigateSection(result.section, result.id)}>
          <span>{result.label}</span><small {...stylex.props(styles.resultCategory)}>{settingsCategories.find(item => item.id === result.section)!.label}</small>
        </button>)}
        {!results.length && <p {...stylex.props(styles.resultsCount)}>Try “font”, “model”, or “connection”.</p>}
      </> : settingsCategories.map((item, index) => {
        const Icon = icons[item.id];
        return <button key={item.id} type="button" aria-current={section === item.id ? 'page' : undefined}
          {...stylex.props(styles.navItem, section === item.id && styles.selected, (index === 2 || index === 5) && styles.navBreak)}
          onClick={() => navigateSection(item.id)}><Icon size={16} aria-hidden /><span>{item.label}</span></button>;
      })}
    </nav>
    <div {...stylex.props(styles.footer)}><span>Whip{runtime.platform.updates?.currentVersion ? ` · ${runtime.platform.updates.currentVersion}` : ''}</span></div>
  </>;
  return <div data-settings-layout {...stylex.props(styles.root)}>
    <aside data-settings-navigation {...stylex.props(styles.sidebar)} aria-label="Settings navigation">
      <div aria-hidden {...stylex.props(styles.titlebar, runtime.platform.chrome === 'inset' && layout.windowDrag)} />
      <Button variant="ghost" onClick={back} xstyle={styles.back}><ArrowLeft size={16} />Back to workspace</Button>
      {sidebar}
    </aside>
    <div {...stylex.props(styles.main)}>
      {runtime.platform.chrome === 'inset' && <div aria-hidden {...stylex.props(styles.mobileTitlebar, layout.windowDrag)} />}
      <header {...stylex.props(styles.mobileHeader)}>
        <Button variant="ghost" onClick={back}><ArrowLeft size={16} />Back</Button>
        <Button variant="ghost" aria-expanded={navigation} onClick={() => setNavigation(true)}>{category.label}<ChevronDown size={15} /></Button>
      </header>
      <div aria-hidden {...stylex.props(styles.mainTitlebar, runtime.platform.chrome === 'inset' && layout.windowDrag)} />
      <main ref={content} aria-labelledby="settings-title" {...stylex.props(styles.scroll)}>
        <div {...stylex.props(styles.content)}>
          <h1 id="settings-title" {...stylex.props(styles.heading)}>{category.label}</h1>
          {section !== 'appearance' && <p {...stylex.props(styles.description)}>{category.description}</p>}
          {hostSection && <HostSelector hosts={hosts} host={host}
            state={identityChanged ? 'identity changed' : observedHost?.state ?? 'closed'}
            onValueChange={id => void navigate({ to: '/settings', search: { section, host: id }, replace: true })} />}
          {hostSection && !enabled && <p role="status" {...stylex.props(layout.notice)}>
            {identityChanged ? 'The execution host identity changed. Your edits are preserved. Leave this category and reopen it to review the new host.' : observedHost?.error ?? (host ? `Connect ${host.name} to change its configuration or check commands.` : 'Choose an execution host to use shared settings.')}
            {' '}<Button variant="ghost" onClick={() => navigateSection('connections')}>Manage servers</Button>
          </p>}
          {section === 'appearance' && <AppearanceSettings />}
          {section === 'general' && <GeneralSettings />}
          {section === 'providers' && host?.client && <ProvidersSettings key={`${host.id}:${host.runtimeId}`} client={host.client} enabled={enabled} />}
          {section === 'execution' && host?.client && <ExecutionSettings key={`${host.id}:${host.runtimeId}`} client={host.client} enabled={enabled} />}
          {section === 'execution' && host?.client && <AgentsSettings key={`agents:${host.id}:${host.runtimeId}`} client={host.client} enabled={enabled} />}
          {section === 'connections' && <ConnectionsSettings />}
          {section === 'recovery' && <RecoverySettings key={`${host?.id}:${host?.runtimeId}`} client={host?.client} enabled={enabled} />}
          {section === 'about' && <AboutSettings />}
        </div>
      </main>
    </div>
    <Sheet open={navigation} onOpenChange={setNavigation} title="Settings" xstyle={styles.sheet}><div data-settings-navigation {...stylex.props(styles.sheetNavigation)}>{sidebar}</div></Sheet>
  </div>;
}
const styles = stylex.create({
  root: { display: 'flex', flex: 1, minHeight: 0, minWidth: 0, overflow: 'hidden' },
  sidebar: { width: 248, flexShrink: 0, display: { default: 'flex', [scale.phone]: 'none' }, flexDirection: 'column', gap: 12, paddingInline: 12, borderRightWidth: 1, borderRightStyle: 'solid', borderRightColor: surface.quietBorder, backgroundColor: surface.navigation },
  titlebar: { height: 34, flexShrink: 0, marginInline: -12 },
  mainTitlebar: { height: 34, flexShrink: 0, display: { default: 'block', [scale.phone]: 'none' } },
  back: { justifyContent: 'flex-start', alignSelf: 'stretch', width: '100%', color: surface.secondaryText, fontSize: typography.size12 },
  search: { display: 'flex', alignItems: 'center', gap: 6, paddingInline: 8, backgroundColor: colors.panel, borderRadius: 6, color: surface.secondaryText },
  searchInput: { flex: 1, width: 0, borderWidth: 0, backgroundColor: 'transparent', paddingInline: 0, fontSize: { default: typography.size12, [scale.touch]: typography.size16 } },
  navigation: { display: 'flex', flexDirection: 'column', gap: 3, flex: 1, minHeight: 0, overflowY: 'auto' },
  navItem: { display: 'flex', alignItems: 'center', gap: 10, textAlign: 'left', color: surface.secondaryText, fontFamily: typography.sans, fontSize: typography.size13, lineHeight: 1.4, minHeight: { default: 36, [scale.touch]: 44 }, flexShrink: 0, padding: '8px 10px', borderWidth: 0, borderRadius: 7, cursor: 'pointer', backgroundColor: { default: 'transparent', ':hover': colors.hover }, outlineOffset: -2 },
  selected: { backgroundColor: { default: colors.element, ':hover': colors.hover }, color: colors.foreground },
  navBreak: { marginTop: 18 },
  result: { flexDirection: 'column', alignItems: 'flex-start', gap: 3 },
  resultCategory: { color: surface.secondaryText, fontSize: typography.size11 },
  resultsCount: { fontSize: typography.size12, color: surface.secondaryText, paddingInline: 10 },
  footer: { display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 8, paddingBlock: 12, fontSize: typography.size12, color: surface.secondaryText },
  main: { display: 'flex', flexDirection: 'column', minWidth: 0, minHeight: 0, flex: 1 },
  scroll: { flex: 1, overflowY: 'auto', minWidth: 0, minHeight: 0 },
  content: { maxWidth: 936, marginInline: 'auto', paddingInline: { default: 48, '@media (max-width: 1100px)': 28, [scale.phone]: 16 }, paddingTop: 12, paddingBottom: 64, display: 'flex', flexDirection: 'column', gap: 24 },
  heading: { margin: 0, fontSize: typography.size20, fontWeight: 560, lineHeight: 1.4 },
  description: { margin: 0, marginTop: -12, fontSize: typography.size13, lineHeight: 1.6, color: surface.secondaryText },
  mobileHeader: { display: { default: 'none', [scale.phone]: 'flex' }, justifyContent: 'space-between', flexWrap: 'wrap', alignItems: 'center', gap: 8, padding: 8, borderBottomWidth: 1, borderBottomStyle: 'solid', borderBottomColor: surface.quietBorder },
  mobileTitlebar: { display: { default: 'none', [scale.phone]: 'block' }, height: 36, flexShrink: 0 },
  sheetNavigation: { display: 'flex', flexDirection: 'column', gap: 16, minHeight: 0, flex: 1 },
  sheet: { display: 'flex', flexDirection: 'column', gap: 16, height: '100dvh', maxWidth: 320 },
});
