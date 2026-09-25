export type ThemePreference = 'system' | 'light' | 'dark';
export const themeStorageKey = 'whipcode-docs-theme';

// Runs before paint; CSS has a system-preference fallback when scripts are disabled.
export const themeInitScript = `(function(){var p='system';try{var s=localStorage.getItem('whipcode-docs-theme');if(s==='dark'||s==='light')p=s}catch(e){}var dark=p==='dark'||(p==='system'&&window.matchMedia('(prefers-color-scheme: dark)').matches);document.documentElement.dataset.theme=dark?'dark':'light'})()`;

export function readThemePreference(): ThemePreference {
  try {
    const value = localStorage.getItem(themeStorageKey);
    if (value === 'light' || value === 'dark') return value;
  } catch { /* Storage may be unavailable; system mode still works. */ }
  return 'system';
}

export function applyTheme(preference: ThemePreference) {
  const dark = preference === 'dark' || (preference === 'system' && matchMedia('(prefers-color-scheme: dark)').matches);
  document.documentElement.dataset.theme = dark ? 'dark' : 'light';
}

export function saveThemePreference(preference: ThemePreference) {
  try { localStorage.setItem(themeStorageKey, preference); } catch { /* Keep the current-tab choice. */ }
  applyTheme(preference);
}
