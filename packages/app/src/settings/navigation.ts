import type { AnyRouter } from '@tanstack/react-router';
import type { AppRuntime } from '../runtime';
import { selectedSessionTab, validateSessionSearch, type SessionSearch } from '../session-tabs';
import { draftDestination, tabDestination } from '../session-tab-routing';

export const settingsCategories = [
  { id: 'general', label: 'General', description: 'Keyboard shortcuts and attention on this device.' },
  { id: 'appearance', label: 'Appearance', description: 'Make Whip comfortable to read and work in.' },
  { id: 'providers', label: 'Providers & models', description: 'Connect providers and choose defaults for this execution host.' },
  { id: 'execution', label: 'Agents & execution', description: 'Configure how agents run on this execution host.' },
  { id: 'connections', label: 'Servers', description: 'Manage the computers where Whip runs.' },
  { id: 'recovery', label: 'Recovery', description: 'Recover saved drafts and check uncertain commands.' },
  { id: 'about', label: 'About & updates', description: 'Application information and available updates.' },
] as const;
export type SettingsSection = (typeof settingsCategories)[number]['id'];
export function isSettingsSection(value: unknown): value is SettingsSection {
  return settingsCategories.some(category => category.id === value);
}
export interface SettingsSearch { section?: SettingsSection; host?: string; setting?: string }
export interface SettingEntry { id: string; section: SettingsSection; label: string; keywords: string; desktopOnly?: boolean }
export const settingEntries: readonly SettingEntry[] = [
  { id: 'commandShortcut', section: 'general', label: 'Open commands', keywords: 'keyboard shortcut command palette' },
  { id: 'composerShortcut', section: 'general', label: 'Focus message composer', keywords: 'keyboard shortcut message' },
  { id: 'attentionAnnouncements', section: 'general', label: 'Attention announcements', keywords: 'accessibility screen reader voiceover' },
  { id: 'desktopNotifications', section: 'general', label: 'Desktop notifications', keywords: 'alerts attention', desktopOnly: true },
  { id: 'theme', section: 'appearance', label: 'Color theme', keywords: 'light dark system colors' },
  { id: 'custom-themes', section: 'appearance', label: 'Custom themes', keywords: 'import json file colors' },
  { id: 'toolDensity', section: 'appearance', label: 'Tool call density', keywords: 'compact comfortable detailed output' },
  { id: 'wrapCode', section: 'appearance', label: 'Code block word wrap', keywords: 'long lines output' },
  { id: 'uiSize', section: 'appearance', label: 'UI font size', keywords: 'typography reading text larger smaller' },
  { id: 'codeSize', section: 'appearance', label: 'Code font size', keywords: 'typography output text larger smaller' },
  { id: 'uiFont', section: 'appearance', label: 'UI font family', keywords: 'typography inter system' },
  { id: 'codeFont', section: 'appearance', label: 'Code font family', keywords: 'typography jetbrains mono monospace system' },
  { id: 'contrast', section: 'appearance', label: 'Contrast', keywords: 'accessibility high increased system' },
  { id: 'motion', section: 'appearance', label: 'Reduce motion', keywords: 'accessibility animation system' },
  { id: 'providers', section: 'providers', label: 'Provider connections', keywords: 'login credentials api key openai anthropic codex claude openrouter inference' },
  { id: 'default_model', section: 'providers', label: 'Default model', keywords: 'default provider model route' },
  { id: 'default_effort', section: 'providers', label: 'Default reasoning effort', keywords: 'thinking low medium high' },
  { id: 'compact_model', section: 'execution', label: 'Compaction model', keywords: 'context summary' },
  { id: 'compact_provider', section: 'execution', label: 'Compaction provider', keywords: 'context summary' },
  { id: 'compact_percent', section: 'execution', label: 'Compaction threshold', keywords: 'context percentage' },
  { id: 'goal_max_rounds', section: 'execution', label: 'Goal rounds', keywords: 'agent limit execution' },
  { id: 'max_retries', section: 'execution', label: 'Maximum retries', keywords: 'agent errors execution' },
  { id: 'import_claude', section: 'execution', label: 'Import Claude configuration', keywords: 'integration' },
  { id: 'import_codex', section: 'execution', label: 'Import Codex configuration', keywords: 'integration' },
  { id: 'hosts', section: 'connections', label: 'Servers', keywords: 'execution hosts connections server remote ssh url tailscale test connection' },
  { id: 'drafts', section: 'recovery', label: 'Saved drafts', keywords: 'storage device offline discard' },
  { id: 'commands', section: 'recovery', label: 'Command recovery', keywords: 'uncertain status forget retry' },
  { id: 'updates', section: 'about', label: 'Application updates', keywords: 'version install restart release', desktopOnly: true },
];
export function searchSettings(query: string, nativeNotifications: boolean, updates: boolean): readonly SettingEntry[] {
  const words = query.trim().toLocaleLowerCase().split(/\s+/).filter(Boolean);
  if (!words.length) return [];
  return settingEntries.filter(entry =>
    (!entry.desktopOnly || (entry.id === 'updates' ? updates : nativeNotifications)) &&
    words.every(word => `${entry.label} ${entry.keywords} ${settingsCategories.find(category => category.id === entry.section)!.label}`.toLocaleLowerCase().includes(word)),
  );
}
export function validateSettingsSearch(value: Record<string, unknown>): SettingsSearch {
  return {
    ...(isSettingsSection(value.section) ? { section: value.section } : {}),
    ...(typeof value.host === 'string' && value.host.length > 0 && value.host.length <= 256 && !/[\u0000-\u001f]/.test(value.host) ? { host: value.host } : {}),
    ...(typeof value.setting === 'string' && settingEntries.some(entry => entry.id === value.setting && entry.section === (value.section ?? 'appearance')) ? { setting: value.setting } : {}),
  };
}

export interface SettingsReturn { runtimeId?: string; rootId?: string; viewId?: string; search: SessionSearch; focusId?: string }
export const settingsReturnKey = 'whip.web.settings-return.v1';
export function parseSettingsReturn(value: unknown): SettingsReturn | undefined {
  if (!value || typeof value !== 'object') return;
  const item = value as Record<string, unknown>;
  const id = (key: string) => typeof item[key] === 'string' && item[key].length <= 256 && !/[\u0000-\u001f]/.test(item[key]) ? item[key] as string : undefined;
  const runtimeId = id('runtimeId'), rootId = id('rootId');
  if (!!runtimeId !== !!rootId) return;
  return { runtimeId, rootId, viewId: id('viewId'), focusId: id('focusId'), search: validateSessionSearch(item.search && typeof item.search === 'object' ? item.search as Record<string, unknown> : {}) };
}
export function settingsBackDestination(runtime: AppRuntime) {
  const saved = runtime.settingsReturn;
  const workspace = runtime.tabs.workspace();
  const exact = saved?.viewId ? workspace.tabs.find(tab => tab.id === saved.viewId && (!saved.rootId || (tab.kind !== 'new' && tab.runtimeId === saved.runtimeId && tab.rootId === saved.rootId))) : undefined;
  const tab = exact ?? (saved && !saved.rootId && !saved.viewId ? undefined : selectedSessionTab(workspace));
  if (!tab) return { to: '/' as const, search: {}, replace: true };
  const destination = tabDestination(tab);
  return { ...destination, ...(exact && exact.kind !== 'new' && saved?.rootId ? { search: saved.search } : {}), replace: true };
}
export function bindSettingsNavigation(runtime: AppRuntime, router: AnyRouter) {
  return router.subscribe('onBeforeNavigate', ({ fromLocation, toLocation }) => {
    if (fromLocation?.pathname === '/settings' && toLocation.pathname !== '/settings') { runtime.clearSettingsReturn(); return; }
    if (toLocation.pathname !== '/settings' || !fromLocation || fromLocation.pathname === '/settings') return;
    const match = /^\/h\/([^/]+)\/s\/([^/]+)\/?$/.exec(fromLocation.pathname);
    const draftId = draftDestination(fromLocation.pathname);
    if (!match && !draftId && fromLocation.pathname !== '/') return;
    try {
      const active = typeof document === 'undefined' ? undefined : document.activeElement;
      runtime.rememberSettingsReturn({
        ...(match ? { runtimeId: decodeURIComponent(match[1]!), rootId: decodeURIComponent(match[2]!) } : {}),
        viewId: draftId ?? (match ? fromLocation.state.whipViewId ?? selectedSessionTab(runtime.tabs.workspace())?.id : undefined),
        search: fromLocation.search, focusId: active?.id || undefined,
      });
    } catch (error) { runtime.report(error); }
  });
}
