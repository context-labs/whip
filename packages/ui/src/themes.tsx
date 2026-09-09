import * as stylex from '@stylexjs/stylex';
import { createContext, useCallback, useContext, useEffect, useLayoutEffect, useMemo, useState, useRef } from 'react';
import type { ReactNode } from 'react';
import { Palette, Check, Monitor, ChevronDown, Search } from 'lucide-react';
import { Combobox as BaseCombobox } from '@base-ui/react/combobox';
import { themeCatalog } from './generated/theme-catalog';
import type { ThemeDefinition } from './generated/theme-catalog';
import { themeStyles } from './generated/themes.stylex';
import { appearance, colors, typography, surface, syntax, markdown, scale } from './tokens.stylex';
import { Button } from './actions';
import { Dialog } from './overlays';
import { Combobox } from './forms';
import type { Option } from './forms';
import { styles } from './styles.stylex';
import {adaptThemeForWeb, browserSurfaces} from './theme-contrast';
import {defaultDisplayPreferences, displayStorageKey, parseDisplayPreferences, validateDisplayPreferences} from './appearance-data';
import type {DisplayPreferences} from './appearance-data';
export {defaultDisplayPreferences, displayStorageKey, prefersReducedMotion} from './appearance-data';
export type {DisplayPreferences} from './appearance-data';
import {colorRoles, syntaxRoles, markdownRoles, validateTheme} from './theme-data';
export {validateTheme} from './theme-data';

export {themeCatalog, themeIds} from './generated/theme-catalog';
export type {ThemeDefinition} from './generated/theme-catalog';
export interface ThemeStorage {getItem(key: string): string | null; setItem(key: string, value: string): void}
export interface ThemeOptions {storage?: ThemeStorage; initialTheme?: string; customThemes?: readonly ThemeDefinition[]; onNotice?: (notice: string) => void; systemContrast?: boolean}
export const themeStorageKey = 'whip.appearance.theme.v1';
const builtins = new Set(themeCatalog.map(theme => theme.id));
const kebab = (value: string) => value.replace(/[A-Z]/g, char => `-${char.toLowerCase()}`);
function systemTheme() {return typeof matchMedia !== 'undefined' && matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';}
function resolveTheme(id: string, custom: readonly ThemeDefinition[]): ThemeDefinition {
  const found = [...themeCatalog, ...custom].find(theme => theme.id === (id === 'auto' ? systemTheme() : id));
  return found ?? themeCatalog.find(theme => theme.id === systemTheme())!;
}
const appliedClasses = new WeakMap<HTMLElement, string[]>();
/** Apply only validated data to fixed CSS variables, never arbitrary CSS. Call
 * before createRoot to avoid a light/dark flash; portals inherit the document. */
export function applyTheme(input: ThemeDefinition, documentRoot: HTMLElement = document.documentElement, increased = false) {
  const theme = adaptThemeForWeb(validateTheme(input), increased);
  for (const name of appliedClasses.get(documentRoot) ?? []) documentRoot.classList.remove(name);
  const variables: [string, string][] = [
    ...colorRoles.map(key => [`--whip-${kebab(key)}`, theme.colors[key]] as [string, string]),
    ...syntaxRoles.map(key => [`--whip-syntax-${key}`, theme.syntax[key]] as [string, string]),
    ...markdownRoles.map(key => [`--whip-markdown-${key}`, theme.markdown[key]] as [string, string]),
  ];
  for (const [name, color] of variables) documentRoot.style.setProperty(name, color);
  const compiled = themeStyles[theme.id as keyof typeof themeStyles];
  const classes = compiled ? (stylex.props(...compiled).className ?? '').split(' ').filter(Boolean) : [];
  appliedClasses.set(documentRoot, classes);
  for (const name of classes) documentRoot.classList.add(name);
  // StyleX exports references to generated variables. This fixed role allowlist
  // applies accessible presentation deltas without changing generated catalogs.
  const setToken = (reference: string, value: string) => documentRoot.style.setProperty(reference.slice(4, -1), value);
  for (const role of colorRoles) setToken(colors[role], theme.colors[role]);
  for (const role of syntaxRoles) setToken(syntax[role], theme.syntax[role]);
  for (const role of markdownRoles) setToken(markdown[role], theme.markdown[role]);
  const surfaces = browserSurfaces(theme, increased);
  for (const role of Object.keys(surfaces) as (keyof typeof surfaces)[]) setToken(surface[role], surfaces[role]);
  documentRoot.style.colorScheme = theme.dark ? 'dark' : 'light';
  documentRoot.style.backgroundColor = theme.colors.background;
  documentRoot.style.color = theme.colors.foreground;
  documentRoot.style.setProperty('--whip-focus-ring', increased ? theme.colors.borderFocus : surface.secondaryText);
  documentRoot.dataset.contrast = increased ? 'more' : 'standard';
  documentRoot.style.setProperty('--whip-selection', theme.colors.primary);
  documentRoot.style.setProperty('--whip-selection-text', theme.colors.onPrimary);
  documentRoot.dataset.theme = theme.id;
}
const uiSizes = [10, 11, 12, 13, 14, 15, 16, 17, 18, 20, 24, 28] as const;
function systemContrast() {return typeof matchMedia !== 'undefined' && matchMedia('(prefers-contrast: more)').matches;}
/** Fixed, validated variables also serve CSS reset and portaled controls. */
export function applyDisplayPreferences(input: DisplayPreferences, root: HTMLElement = document.documentElement) {
  const display = validateDisplayPreferences(input);
  const systemSans = "-apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif";
  const systemMono = 'ui-monospace, SFMono-Regular, Menlo, Consolas, monospace';
  root.style.setProperty('--whip-font-sans', display.uiFont === 'inter' ? `"Inter Variable", ${systemSans}` : systemSans);
  root.style.setProperty('--whip-font-mono', display.codeFont === 'jetbrains-mono' ? `"JetBrains Mono Variable", ${systemMono}` : systemMono);
  for (const size of uiSizes) root.style.setProperty(`--whip-size-${size}`, `${Number((size * display.uiSize / 13).toFixed(4))}px`);
  root.style.setProperty('--whip-code-size', `${display.codeSize}px`);
  root.style.setProperty('--whip-code-white-space', display.wrapCode ? 'pre-wrap' : 'pre');
  root.style.setProperty('--whip-code-overflow-wrap', display.wrapCode ? 'anywhere' : 'normal');
  root.style.setProperty('--whip-motion-fast', display.motion === 'reduce' ? '0ms' : '100ms');
  root.style.setProperty('--whip-motion-normal', display.motion === 'reduce' ? '0ms' : '160ms');
  root.dataset.motion = display.motion;
}
interface InitialTheme {theme: string; customThemes: readonly ThemeDefinition[]; display: DisplayPreferences; notice?: string; displayNotice?: string}
function savedTheme(id: string, custom: readonly ThemeDefinition[]) {
  const text = JSON.stringify({version: 1, id, customs: custom});
  if (custom.length > 16 || text.length > 262144 || new TextEncoder().encode(text).byteLength > 262144) throw new Error('Imported themes exceed this device’s limit of 16 themes / 256 KiB.');
  return text;
}
export function initializeTheme({storage, initialTheme, customThemes = [], onNotice, systemContrast: nativeContrast}: ThemeOptions = {}): InitialTheme {
  let theme = initialTheme ?? 'auto'; let notice: string | undefined; let displayNotice: string | undefined;
  let display = {...defaultDisplayPreferences};
  const custom = [...customThemes].map(validateTheme).filter(item => !builtins.has(item.id));
  if (!initialTheme && storage) {
    try {
      const text = storage.getItem(themeStorageKey);
      if (text) {
        if (text.length > 262144 || new TextEncoder().encode(text).byteLength > 262144) throw new Error('Saved theme exceeds the size limit.');
        const saved = JSON.parse(text) as {version: number; id: string; custom?: unknown; customs?: unknown};
        if (saved.version !== 1 || typeof saved.id !== 'string') throw new Error('Unsupported appearance preference.');
        const cached = saved.customs === undefined ? saved.custom ? [saved.custom] : [] : saved.customs;
        if (!Array.isArray(cached) || cached.length > 16) throw new Error('Invalid imported themes.');
        for (const value of cached) {
          const item = validateTheme(value);
          if (builtins.has(item.id) || item.id === 'auto') throw new Error('A custom theme cannot replace a shipped theme.');
          if (!custom.some(existing => existing.id === item.id)) custom.push(item);
        }
        savedTheme(saved.id, custom);
        theme = saved.id;
      }
    } catch {notice = 'Saved theme could not be read. Following your system appearance.'; theme = 'auto';}
  }
  try {display = parseDisplayPreferences(storage?.getItem(displayStorageKey) ?? null);}
  catch {displayNotice = 'Saved display preferences could not be read. Defaults are active; choose your preferences again to save them.';}
  if (theme !== 'auto' && ![...themeCatalog, ...custom].some(item => item.id === theme)) {notice = 'The saved theme is unavailable. Following your system appearance.'; theme = 'auto';}
  if (typeof document !== 'undefined') {
    applyDisplayPreferences(display);
    applyTheme(resolveTheme(theme, custom), document.documentElement, display.contrast === 'more' || (display.contrast === 'system' && (nativeContrast ?? systemContrast())));
  }
  if (notice || displayNotice) onNotice?.([notice, displayNotice].filter(Boolean).join(' '));
  return {theme, customThemes: custom, display, notice, displayNotice};
}
export interface ThemeContextValue {
  theme: string; resolvedTheme: ThemeDefinition; themes: readonly ThemeDefinition[]; notice?: string;
  increasedContrast: boolean;
  display: DisplayPreferences; setDisplay(patch: Partial<DisplayPreferences>): void; resetDisplay(): void; resetAppearance(): void;
  setTheme(id: string): void; previewTheme(id: string): void; cancelPreview(): void; addTheme(theme: ThemeDefinition): void;
}
const ThemeContext = createContext<ThemeContextValue | null>(null);
export function ThemeProvider({children, ...options}: ThemeOptions & {children: ReactNode}) {
  const [initial] = useState(() => initializeTheme(options));
  const [theme, setSelected] = useState(initial.theme);
  const selectedRef = useRef(theme);
  const [display, setDisplayState] = useState(initial.display);
  const displayRef = useRef(display);
  const [custom, setCustom] = useState(initial.customThemes);
  const customRef = useRef(custom);
  const [preview, setPreview] = useState<string | null>(null);
  const [system, setSystem] = useState(systemTheme);
  const [contrast, setContrast] = useState(systemContrast);
  const [themeNotice, setThemeNotice] = useState(initial.notice);
  const [displayNotice, setDisplayNotice] = useState(initial.displayNotice);
  const increased = display.contrast === 'more' || (display.contrast === 'system' && (options.systemContrast ?? contrast));
  const baseTheme = useMemo(() => resolveTheme(preview ?? (theme === 'auto' ? system : theme), custom), [theme, preview, system, custom]);
  const resolvedTheme = useMemo(() => adaptThemeForWeb(baseTheme, increased), [baseTheme, increased]);
  useEffect(() => {
    if (typeof matchMedia === 'undefined') return;
    const query = matchMedia('(prefers-color-scheme: dark)');
    const contrastQuery = matchMedia('(prefers-contrast: more)');
    const update = () => {setSystem(query.matches ? 'dark' : 'light'); setContrast(contrastQuery.matches);};
    update(); query.addEventListener('change', update); contrastQuery.addEventListener('change', update);
    return () => {query.removeEventListener('change', update); contrastQuery.removeEventListener('change', update);};
  }, []);
  useLayoutEffect(() => {applyTheme(baseTheme, document.documentElement, increased); applyDisplayPreferences(display);}, [baseTheme, increased, display]);
  const persistTheme = useCallback((id: string, themes: readonly ThemeDefinition[]) => {
    try {options.storage?.setItem(themeStorageKey, savedTheme(id, themes)); setThemeNotice(undefined);}
    catch {const message = 'Theme changed for this window, but could not be saved. Check device storage and choose it again to retry.'; setThemeNotice(message); options.onNotice?.(message);}
  }, [options.storage, options.onNotice]);
  const setTheme = useCallback((id: string) => {
    if (id !== 'auto' && ![...themeCatalog, ...customRef.current].some(item => item.id === id)) throw new Error(`Unknown theme: ${id}`);
    selectedRef.current = id; setSelected(id); setPreview(null); persistTheme(id, customRef.current);
  }, [persistTheme]);
  const addTheme = useCallback((input: ThemeDefinition) => {
    const value = validateTheme(input);
    if (builtins.has(value.id) || value.id === 'auto') throw new Error('Custom themes cannot replace shipped themes or system appearance.');
    const next = [...customRef.current.filter(item => item.id !== value.id), value];
    savedTheme(selectedRef.current, next);
    customRef.current = next; setCustom(next); persistTheme(selectedRef.current, next);
  }, [persistTheme]);
  const setDisplay = useCallback((patch: Partial<DisplayPreferences>) => {
    const next = validateDisplayPreferences({...displayRef.current, ...patch});
    displayRef.current = next; setDisplayState(next);
    try {options.storage?.setItem(displayStorageKey, JSON.stringify({version: 1, display: next})); setDisplayNotice(undefined);}
    catch {const message = 'Display changed for this window, but could not be saved. Check device storage and change a preference again to retry.'; setDisplayNotice(message); options.onNotice?.(message);}
  }, [options.storage, options.onNotice]);
  const resetDisplay = useCallback(() => setDisplay({...defaultDisplayPreferences}), [setDisplay]);
  const resetAppearance = useCallback(() => {setTheme('auto'); resetDisplay();}, [setTheme, resetDisplay]);
  const themes = useMemo(() => [...themeCatalog, ...custom], [custom]);
  const notice = [themeNotice, displayNotice].filter(Boolean).join(' ') || undefined;
  const context = useMemo(() => ({theme, resolvedTheme, themes, notice, display, increasedContrast: increased, setDisplay, resetDisplay, resetAppearance, setTheme,
    previewTheme: setPreview, cancelPreview: () => setPreview(null), addTheme}),
  [theme, resolvedTheme, themes, notice, display, increased, setDisplay, resetDisplay, resetAppearance, setTheme, addTheme]);
  return <ThemeContext.Provider value={context}>{children}</ThemeContext.Provider>;
}
export function useTheme(): ThemeContextValue {const value = useContext(ThemeContext); if (!value) throw new Error('useTheme must be used inside ThemeProvider.'); return value;}

const previewStyles = stylex.create({
  panel: {border: `1px solid ${surface.quietBorder}`, borderRadius: 8, backgroundColor: colors.panel, padding: 16, display: 'flex', flexDirection: 'column', gap: 12},
  name: {fontSize: typography.size12, color: surface.secondaryText, display: 'flex', gap: 6, alignItems: 'center'},
  heading: {fontSize: typography.size15, lineHeight: '1.6', margin: 0, fontWeight: 550, color: colors.foreground},
  prose: {fontSize: typography.size13, lineHeight: '1.6154', color: colors.foreground, margin: 0},
  code: {fontFamily: typography.mono, fontSize: typography.codeSize, lineHeight: '1.6667', whiteSpace: appearance.codeWhiteSpace, overflowWrap: appearance.codeOverflowWrap, padding: 12, borderRadius: 6, backgroundColor: colors.element, margin: 0, overflow: 'auto'},
  keyword: {color: syntax.keyword}, string: {color: syntax.string},
  swatches: {display: 'flex', gap: 5}, swatch: {width: 16, height: 16, borderRadius: 4, borderWidth: 1, borderStyle: 'solid', borderColor: surface.quietBorder},
});
function ThemeSwatch({theme}: {theme: ThemeDefinition}) {
  const {increasedContrast} = useTheme();
  const preview = useMemo(() => adaptThemeForWeb(theme, increasedContrast), [theme, increasedContrast]);
  const surfaces = useMemo(() => browserSurfaces(preview, increasedContrast), [preview, increasedContrast]);
  return <span aria-hidden="true" data-theme-swatch={theme.id} {...stylex.props(pickerStyles.swatch)} style={{backgroundColor: preview.colors.background, borderColor: surfaces.quietBorder}}>
    <span data-swatch-role="navigation" {...stylex.props(pickerStyles.swatchNavigation)} style={{backgroundColor: surfaces.navigation, color: surfaces.secondaryText}}>
      <span {...stylex.props(pickerStyles.swatchNavMark)}/>
      <span {...stylex.props(pickerStyles.swatchNavLine)}/>
      <span {...stylex.props(pickerStyles.swatchNavLine)}/>
    </span>
    <span {...stylex.props(pickerStyles.swatchContent)}>
      <span {...stylex.props(pickerStyles.swatchMessage)}>
        <span data-swatch-role="primary" {...stylex.props(pickerStyles.swatchDot)} style={{backgroundColor: preview.colors.primary}}/>
        <span data-swatch-role="foreground" {...stylex.props(pickerStyles.swatchText)} style={{backgroundColor: preview.colors.foreground}}/>
      </span>
      <span {...stylex.props(pickerStyles.swatchReply)} style={{backgroundColor: surfaces.secondaryText}}/>
      <span data-swatch-role="composer" {...stylex.props(pickerStyles.swatchComposer)} style={{backgroundColor: preview.colors.element}}>
        <span {...stylex.props(pickerStyles.swatchPrompt)} style={{backgroundColor: surfaces.secondaryText}}/>
        <span {...stylex.props(pickerStyles.swatchSend)} style={{backgroundColor: preview.colors.foreground}}/>
      </span>
    </span>
  </span>;
}
export function ThemePreview() {
  const {resolvedTheme} = useTheme();
  return <section aria-label={`${resolvedTheme.name} theme preview`} {...stylex.props(previewStyles.panel)}><span {...stylex.props(previewStyles.name)}><Check size={13}/>Agent finished a focused check</span><h3 {...stylex.props(previewStyles.heading)}>A quiet space for complex work.</h3><p {...stylex.props(previewStyles.prose)}>Follow the conversation, inspect a child agent, and decide what happens next.</p><pre {...stylex.props(previewStyles.code)} style={{backgroundColor: resolvedTheme.code.background, color: resolvedTheme.code.foreground}}><span {...stylex.props(previewStyles.keyword)}>result</span> = agents.inspect(<span {...stylex.props(previewStyles.string)}>"explore"</span>)</pre><div {...stylex.props(styles.row)}><Button size="sm" variant="primary">Allow once</Button><Button size="sm">Deny</Button><div {...stylex.props(previewStyles.swatches)}>{['primary', 'accent', 'success', 'warning', 'error', 'info'].map(role => <span key={role} aria-hidden {...stylex.props(previewStyles.swatch)} style={{backgroundColor: resolvedTheme.colors[role as keyof ThemeDefinition['colors']]}}/>)}</div></div></section>;
}
export function ThemePicker({compact = false, presentation = 'dialog'}: {compact?: boolean; presentation?: 'dialog' | 'popover'} = {}) {
  const {theme, themes, setTheme, previewTheme, cancelPreview, notice} = useTheme();
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState('');
  const search = useRef<HTMLInputElement>(null);
  const selectedTheme = themes.find(item => item.id === theme);
  const selected = theme === 'auto' ? 'System appearance' : themes.find(item => item.id === theme)?.name ?? theme;
  const options = useMemo<Option[]>(() => [{value: 'auto', label: 'System appearance', icon: <Monitor size={18} aria-hidden/>}, ...themes.map(item => ({value: item.id, label: item.name, description: item.dark ? 'Dark' : 'Light', icon: <ThemeSwatch theme={item}/>}))], [themes]);
  const collection = useMemo(() => BaseCombobox.createItems(options, {getValue: item => item.value, getLabel: item => item.label}), [options]);
  if (presentation === 'popover') return <BaseCombobox.Root items={collection} value={theme} open={open} inputValue={query}
    onInputValueChange={setQuery} onOpenChange={next => {setOpen(next); setQuery(''); if (!next) cancelPreview();}}
    onItemHighlighted={(id, details) => {if (id && details.reason !== 'none') previewTheme(id);}}
    onValueChange={id => {if (id) setTheme(id); setOpen(false); setQuery('');}} autoHighlight>
    <BaseCombobox.Trigger render={<Button aria-label={`Color theme: ${selected}`} xstyle={pickerStyles.trigger}/>}>
      <span {...stylex.props(pickerStyles.label)}>{selected}</span>
      {selectedTheme ? <ThemeSwatch theme={selectedTheme}/> : <Monitor size={18} aria-hidden/>}
      <ChevronDown size={14} aria-hidden {...stylex.props(styles.chevron)}/>
    </BaseCombobox.Trigger>
    <BaseCombobox.Portal><BaseCombobox.Positioner align="end" sideOffset={6} collisionPadding={12} {...stylex.props(styles.positioner)}>
      <BaseCombobox.Popup initialFocus={search} {...stylex.props(styles.popup, pickerStyles.popup)}>
        <div {...stylex.props(pickerStyles.search)}><Search size={15} aria-hidden {...stylex.props(pickerStyles.searchIcon)}/>
          <BaseCombobox.Input ref={search} aria-label="Search themes" placeholder="Search themes…" {...stylex.props(styles.input, pickerStyles.input)}/>
        </div>
        <BaseCombobox.Empty><span {...stylex.props(pickerStyles.empty)}>No matching themes</span></BaseCombobox.Empty>
        <BaseCombobox.List {...stylex.props(pickerStyles.list)}>{(option: Option) => <BaseCombobox.Item key={option.value} value={option.value}
          className={state => stylex.props(styles.item, pickerStyles.item, state.selected && pickerStyles.selected, state.highlighted && styles.highlighted).className}>
          <span {...stylex.props(styles.checkSlot, pickerStyles.check)}><BaseCombobox.ItemIndicator><Check size={16}/></BaseCombobox.ItemIndicator></span>
          <span {...stylex.props(pickerStyles.label)}>{option.label}</span>
          {option.description && <span {...stylex.props(styles.description, pickerStyles.mode)}>{option.description}</span>}
          <span {...stylex.props(pickerStyles.palette)}>{option.icon}</span>
        </BaseCombobox.Item>}</BaseCombobox.List>
        {notice && <p role="status" {...stylex.props(styles.description, pickerStyles.notice)}>{notice}</p>}
      </BaseCombobox.Popup>
    </BaseCombobox.Positioner></BaseCombobox.Portal>
  </BaseCombobox.Root>;
  return <><Button aria-label={selected} onClick={() => setOpen(true)}><Palette size={14}/>{!compact && selected}</Button><Dialog open={open} onOpenChange={next => {setOpen(next); if (!next) cancelPreview();}} title="Appearance" description="Every WHIP theme. Preview a palette, then choose it for this device." footer={<Button onClick={() => {cancelPreview(); setOpen(false);}}>Done</Button>}><Combobox value={theme} options={options} label="Search themes" placeholder="Search themes…" onOpenChange={next => {if (!next) cancelPreview();}} onHighlightedValueChange={id => {if (id) previewTheme(id);}} onValueChange={setTheme}/><ThemePreview/>{notice && <p role="status" {...stylex.props(styles.description)}>{notice}</p>}</Dialog></>;
}

const pickerStyles = stylex.create({
  trigger: { width: 240, maxWidth: '100%', justifyContent: 'flex-start', gap: 10, textAlign: 'start' },
  popup: { width: 340, maxWidth: 'calc(100vw - 24px)', maxHeight: 'var(--available-height)', padding: 0, overflow: 'hidden', borderWidth: 1, borderStyle: 'solid', borderColor: surface.quietBorder, borderRadius: 10 },
  search: { display: 'flex', alignItems: 'center', gap: 8, margin: 8, paddingInline: 10, borderWidth: 1, borderStyle: 'solid', borderRadius: scale.radiusControl, borderColor: { default: surface.quietBorder, ':focus-within': surface.secondaryText }, backgroundColor: colors.background },
  searchIcon: { color: surface.secondaryText, flexShrink: 0 },
  input: { backgroundColor: { default: 'transparent', ':hover': 'transparent' }, borderWidth: 0, borderRadius: 0, outline: 'none', paddingBlock: 8, paddingInline: 0 },
  list: { padding: 6, overflowY: 'auto', maxHeight: 'min(320px, calc(var(--available-height) - 64px))', overscrollBehavior: 'contain', scrollPaddingBlock: 6 },
  item: { gap: 10, paddingInline: 10, paddingBlock: 8, minHeight: { default: 40, [scale.touch]: 44 }, borderRadius: 6 },
  selected: { backgroundColor: colors.element },
  label: { flex: 1, minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' },
  palette: { width: 64, flexShrink: 0, display: 'flex', justifyContent: 'center' },
  swatch: { display: 'flex', width: 64, height: 36, flexShrink: 0, overflow: 'hidden', borderRadius: scale.radiusControl, borderWidth: 1, borderStyle: 'solid' },
  swatchNavigation: { display: 'flex', flexDirection: 'column', gap: 3, width: 13, padding: 4, flexShrink: 0 },
  swatchNavMark: { width: 4, height: 3, borderRadius: 1, backgroundColor: 'currentColor', opacity: 0.7, marginBottom: 3 },
  swatchNavLine: { height: 1, width: '100%', backgroundColor: 'currentColor', opacity: 0.4 },
  swatchContent: { display: 'flex', flex: 1, minWidth: 0, flexDirection: 'column', gap: 4, padding: 5 },
  swatchMessage: { display: 'flex', alignItems: 'center', gap: 3 },
  swatchDot: { width: 3, height: 3, borderRadius: '50%', flexShrink: 0 },
  swatchText: { width: 22, height: 2, borderRadius: 1 },
  swatchReply: { width: '62%', height: 1, borderRadius: 1, opacity: 0.7, marginLeft: 6 },
  swatchComposer: { display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginTop: 'auto', height: 10, paddingInline: 3, borderRadius: 3 },
  swatchPrompt: { width: 13, height: 1, opacity: 0.5 },
  swatchSend: { width: 4, height: 4, borderRadius: '50%' },
  mode: { fontSize: typography.size12, flexShrink: 0 },
  check: { color: colors.primary, display: 'flex', alignItems: 'center' },
  empty: { display: 'block', padding: 16, color: surface.secondaryText, fontSize: typography.size13 },
  notice: { margin: 0, padding: 12, borderTopWidth: 1, borderTopStyle: 'solid', borderTopColor: surface.quietBorder },
});
