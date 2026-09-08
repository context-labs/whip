import * as stylex from '@stylexjs/stylex';
import { createContext, useCallback, useContext, useEffect, useMemo, useState, useRef } from 'react';
import type { ReactNode } from 'react';
import { Palette, Check, Monitor } from 'lucide-react';
import { themeCatalog } from './generated/theme-catalog';
import type { ThemeDefinition } from './generated/theme-catalog';
import { themeStyles } from './generated/themes.stylex';
import { colors, typography, surface, syntax, markdown } from './tokens.stylex';
import { Button } from './actions';
import { Dialog } from './overlays';
import { Combobox } from './forms';
import { styles } from './styles.stylex';
import {adaptThemeForWeb, readableColor} from './theme-contrast';
import {colorRoles, syntaxRoles, markdownRoles, validateTheme} from './theme-data';
export {validateTheme} from './theme-data';

export {themeCatalog, themeIds} from './generated/theme-catalog';
export type {ThemeDefinition} from './generated/theme-catalog';
export interface ThemeStorage {getItem(key: string): string | null; setItem(key: string, value: string): void}
export interface ThemeOptions {storage?: ThemeStorage; initialTheme?: string; customThemes?: readonly ThemeDefinition[]; onNotice?: (notice: string) => void}
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
export function applyTheme(input: ThemeDefinition, documentRoot: HTMLElement = document.documentElement) {
  const theme = adaptThemeForWeb(validateTheme(input));
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
  setToken(surface.secondaryText, readableColor(theme.colors.muted, [theme.colors.background, theme.colors.panel, theme.colors.element, theme.colors.hover]));
  setToken(surface.navigation, theme.web?.navigation ?? (theme.dark
    ? `color-mix(in srgb, ${theme.colors.background} 75%, black)`
    : `color-mix(in srgb, ${theme.colors.background} 50%, ${theme.colors.panel})`));
  setToken(surface.quietBorder, theme.web?.quietBorder ?? `color-mix(in srgb, ${theme.colors.border} 55%, ${theme.colors.background})`);
  setToken(surface.inlineCode, theme.web?.inlineCodeBackground ?? theme.colors.element);
  documentRoot.style.colorScheme = theme.dark ? 'dark' : 'light';
  documentRoot.style.backgroundColor = theme.colors.background;
  documentRoot.style.color = theme.colors.foreground;
  documentRoot.style.setProperty('--whip-focus-ring', surface.secondaryText);
  documentRoot.style.setProperty('--whip-selection', theme.colors.primary);
  documentRoot.style.setProperty('--whip-selection-text', theme.colors.onPrimary);
  documentRoot.dataset.theme = theme.id;
}
interface InitialTheme {theme: string; customThemes: readonly ThemeDefinition[]; notice?: string}
export function initializeTheme({storage, initialTheme, customThemes = [], onNotice}: ThemeOptions = {}): InitialTheme {
  let theme = initialTheme ?? 'auto'; let notice: string | undefined;
  const custom = [...customThemes].map(validateTheme).filter(item => !builtins.has(item.id));
  if (!initialTheme && storage) {
    try {
      const text = storage.getItem(themeStorageKey);
      if (text) {
        if (text.length > 262144) throw new Error('Saved theme exceeds the size limit.');
        const saved = JSON.parse(text) as {version: number; id: string; custom?: unknown};
        if (saved.version !== 1 || typeof saved.id !== 'string') throw new Error('Unsupported appearance preference.');
        if (saved.custom) {const cached = validateTheme(saved.custom); if (builtins.has(cached.id)) throw new Error('A custom theme cannot replace a shipped theme.'); custom.push(cached);}
        theme = saved.id;
      }
    } catch {notice = 'Saved appearance could not be read. Following your system appearance.'; theme = 'auto';}
  }
  if (theme !== 'auto' && ![...themeCatalog, ...custom].some(item => item.id === theme)) {notice = `The saved theme is unavailable. Following your system appearance.`; theme = 'auto';}
  if (typeof document !== 'undefined') applyTheme(resolveTheme(theme, custom));
  if (notice) onNotice?.(notice);
  return {theme, customThemes: custom, notice};
}
export interface ThemeContextValue {
  theme: string; resolvedTheme: ThemeDefinition; themes: readonly ThemeDefinition[]; notice?: string;
  setTheme(id: string): void; previewTheme(id: string): void; cancelPreview(): void; addTheme(theme: ThemeDefinition): void;
}
const ThemeContext = createContext<ThemeContextValue | null>(null);
export function ThemeProvider({children, ...options}: ThemeOptions & {children: ReactNode}) {
  const [initial] = useState(() => initializeTheme(options));
  const [theme, setSelected] = useState(initial.theme);
  const [custom, setCustom] = useState(initial.customThemes);
  const customRef = useRef(custom);
  const [preview, setPreview] = useState<string | null>(null);
  const [system, setSystem] = useState(systemTheme);
  const [notice, setNotice] = useState(initial.notice);
  const resolvedTheme = useMemo(() => adaptThemeForWeb(resolveTheme(preview ?? (theme === 'auto' ? system : theme), custom)), [theme, preview, system, custom]);
  useEffect(() => {
    const query = matchMedia('(prefers-color-scheme: dark)');
    const update = () => setSystem(query.matches ? 'dark' : 'light');
    query.addEventListener('change', update); return () => query.removeEventListener('change', update);
  }, []);
  useEffect(() => {applyTheme(resolvedTheme);}, [resolvedTheme]);
  const setTheme = useCallback((id: string) => {
    if (id !== 'auto' && ![...themeCatalog, ...customRef.current].some(item => item.id === id)) throw new Error(`Unknown theme: ${id}`);
    setSelected(id); setPreview(null);
    try {options.storage?.setItem(themeStorageKey, JSON.stringify({version: 1, id, custom: customRef.current.find(item => item.id === id)})); setNotice(undefined);}
    catch {const message = 'Appearance changed for this tab, but could not be saved on this device.'; setNotice(message); options.onNotice?.(message);}
  }, [custom, options.storage, options.onNotice]);
  const addTheme = useCallback((input: ThemeDefinition) => {const value = validateTheme(input); if (builtins.has(value.id) || value.id === 'auto') throw new Error('Custom themes cannot replace shipped themes or system appearance.'); const next = [...customRef.current.filter(item => item.id !== value.id), value]; customRef.current = next; setCustom(next);}, []);
  const themes = useMemo(() => [...themeCatalog, ...custom], [custom]);
  const context = useMemo(() => ({theme, resolvedTheme, themes, notice, setTheme, previewTheme: setPreview, cancelPreview: () => setPreview(null), addTheme}), [theme, resolvedTheme, themes, notice, setTheme, addTheme]);
  return <ThemeContext.Provider value={context}>{children}</ThemeContext.Provider>;
}
export function useTheme(): ThemeContextValue {const value = useContext(ThemeContext); if (!value) throw new Error('useTheme must be used inside ThemeProvider.'); return value;}

const previewStyles = stylex.create({
  panel: {border: `1px solid ${surface.quietBorder}`, borderRadius: 8, backgroundColor: colors.panel, padding: 16, display: 'flex', flexDirection: 'column', gap: 12},
  name: {fontSize: 12, color: surface.secondaryText, display: 'flex', gap: 6, alignItems: 'center'},
  heading: {fontSize: 15, lineHeight: '24px', margin: 0, fontWeight: 550, color: colors.foreground},
  prose: {fontSize: 13, lineHeight: '21px', color: colors.foreground, margin: 0},
  code: {fontFamily: typography.mono, fontSize: 11, lineHeight: '19px', padding: 12, borderRadius: 6, backgroundColor: colors.element, margin: 0, overflow: 'auto'},
  keyword: {color: syntax.keyword}, string: {color: syntax.string},
  swatches: {display: 'flex', gap: 4}, swatch: {width: 12, height: 12, borderRadius: 3, border: `1px solid ${surface.quietBorder}`},
});
export function ThemePreview() {
  const {resolvedTheme} = useTheme();
  return <section aria-label={`${resolvedTheme.name} theme preview`} {...stylex.props(previewStyles.panel)}><span {...stylex.props(previewStyles.name)}><Check size={13}/>Agent finished a focused check</span><h3 {...stylex.props(previewStyles.heading)}>A quiet space for complex work.</h3><p {...stylex.props(previewStyles.prose)}>Follow the conversation, inspect a child agent, and decide what happens next.</p><pre {...stylex.props(previewStyles.code)} style={{backgroundColor: resolvedTheme.code.background, color: resolvedTheme.code.foreground}}><span {...stylex.props(previewStyles.keyword)}>result</span> = agents.inspect(<span {...stylex.props(previewStyles.string)}>"explore"</span>)</pre><div {...stylex.props(styles.row)}><Button size="sm" variant="primary">Allow once</Button><Button size="sm">Deny</Button><div {...stylex.props(previewStyles.swatches)}>{['primary', 'accent', 'success', 'warning', 'error', 'info'].map(role => <span key={role} aria-hidden {...stylex.props(previewStyles.swatch)} style={{backgroundColor: resolvedTheme.colors[role as keyof ThemeDefinition['colors']]}}/>)}</div></div></section>;
}
export function ThemePicker({compact = false}: {compact?: boolean} = {}) {
  const {theme, themes, setTheme, previewTheme, cancelPreview, notice} = useTheme();
  const [open, setOpen] = useState(false);
  const selected = theme === 'auto' ? 'System appearance' : themes.find(item => item.id === theme)?.name ?? theme;
  const options = useMemo(() => [{value: 'auto', label: 'System appearance', icon: <Monitor size={13}/>}, ...themes.map(item => ({value: item.id, label: item.name, description: item.dark ? 'Dark' : 'Light', icon: <span aria-hidden {...stylex.props(previewStyles.swatch)} style={{backgroundColor: item.colors.primary}}/>}))], [themes]);
  return <><Button aria-label={selected} onClick={() => setOpen(true)}><Palette size={14}/>{!compact && selected}</Button><Dialog open={open} onOpenChange={next => {setOpen(next); if (!next) cancelPreview();}} title="Appearance" description="Every WHIP theme. Preview a palette, then choose it for this device." footer={<Button onClick={() => {cancelPreview(); setOpen(false);}}>Done</Button>}><Combobox value={theme} options={options} label="Search themes" placeholder="Search themes…" onHighlightedValueChange={id => {if (id) previewTheme(id);}} onValueChange={setTheme}/><ThemePreview/>{notice && <p role="status" {...stylex.props(styles.description)}>{notice}</p>}</Dialog></>;
}
