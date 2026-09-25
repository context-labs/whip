import * as stylex from '@stylexjs/stylex';

// Docs-local tokens. These reference the site's semantic custom properties
// (defined in styles/tokens.css) with light-theme fallbacks, so runtime theme
// switching via [data-theme] and prefers-color-scheme keeps working unchanged.
// The dark-theme values are not repeated here; they come from tokens.css.

export const colors = stylex.defineVars({
  bg: 'var(--color-bg, #FFFFFF)',
  surfacePanel: 'var(--color-surface-panel, #F4F4F4)',
  surfaceElement: 'var(--color-surface-element, #F4F4F4)',
  surfaceControl: 'var(--color-surface-control, #E8E8E8)',
  surfaceHover: 'var(--color-surface-hover, #E0E0E0)',
  border: 'var(--color-border, #DCDCDC)',
  borderControl: 'var(--color-border-control, #C6C6C6)',
  divider: 'var(--color-divider, #1616161A)',
  text: 'var(--color-text, #161616)',
  textSecondary: 'var(--color-text-secondary, #525252)',
  textMuted: 'var(--color-text-muted, #6F6F6F)',
  accent: 'var(--color-accent, #0043CE)',
  link: 'var(--color-link, #0072C3)',
  code: 'var(--color-code, #198038)',
  success: 'var(--color-success, #198038)',
  warning: 'var(--color-warning, #8E6A00)',
  error: 'var(--color-error, #9F1853)',
  emphasis: 'var(--color-emphasis, #6929C4)',
  calloutInfoBorder: 'var(--color-callout-info-border, #DCDCDC)',
  calloutTipBorder: 'var(--color-callout-tip-border, #19803859)',
  calloutWarningBorder: 'var(--color-callout-warning-border, #8E6A0059)',
  calloutAlertBorder: 'var(--color-callout-alert-border, #9F185366)',
});

export const typography = stylex.defineVars({
  sans: "var(--font-sans, system-ui, -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif)",
  mono: "var(--font-mono, ui-monospace, 'SF Mono', Menlo, Consolas, 'Liberation Mono', monospace)",
  h1: 'var(--text-h1, 32px)',
  h2: 'var(--text-h2, 22px)',
  h3: 'var(--text-h3, 16px)',
  lead: 'var(--text-lead, 15px)',
  body: 'var(--text-body, 13px)',
  small: 'var(--text-small, 12px)',
  label: 'var(--text-label, 11px)',
});

// Static compile-time constants: layout dimensions, radii, breakpoints.
export const scale = stylex.defineConsts({
  space1: 'var(--space-1, 4px)',
  space2: 'var(--space-2, 8px)',
  space3: 'var(--space-3, 12px)',
  space4: 'var(--space-4, 16px)',
  space5: 'var(--space-5, 20px)',
  space6: 'var(--space-6, 24px)',
  space8: 'var(--space-8, 32px)',
  space16: 'var(--space-16, 64px)',
  radiusSm: 'var(--radius-sm, 4px)',
  radiusMd: 'var(--radius-md, 6px)',
  siteMaxWidth: 'var(--site-max-width, 1280px)',
  tablet: '@media (max-width: 1100px)',
  phone: '@media (max-width: 760px)',
  tiny: '@media (max-width: 370px)',
  print: '@media print',
});
