import * as stylex from '@stylexjs/stylex';

// Fixed variable indirection is used only for validated, imported theme colors.
// Built-in themes replace these values with statically compiled createTheme classes.
export const colors = stylex.defineVars({
  background: 'var(--whip-background, #fafafa)', foreground: 'var(--whip-foreground, #1a1a1a)',
  muted: 'var(--whip-muted, #737373)', faint: 'var(--whip-faint, #b0b0b0)',
  primary: 'var(--whip-primary, #3b7dd8)', onPrimary: 'var(--whip-on-primary, #ffffff)',
  accent: 'var(--whip-accent, #d68c27)', success: 'var(--whip-success, #3d9a57)',
  warning: 'var(--whip-warning, #d68c27)', error: 'var(--whip-error, #c4314b)',
  info: 'var(--whip-info, #7b5bb6)', link: 'var(--whip-link, #318795)', emphasis: 'var(--whip-emphasis, #b0851f)',
  border: 'var(--whip-border, #d0d0d0)', borderFocus: 'var(--whip-border-focus, #3b7dd8)',
  diffAdd: 'var(--whip-diff-add, #d7ffd7)', diffDel: 'var(--whip-diff-del, #ffd7d7)',
  panel: 'var(--whip-panel, #f4f4f4)', element: 'var(--whip-element, #eeeeee)', hover: 'var(--whip-hover, #e8e8e8)',
});
export const syntax = stylex.defineVars({
  keyword: 'var(--whip-syntax-keyword, #7b5bb6)', string: 'var(--whip-syntax-string, #3d9a57)',
  number: 'var(--whip-syntax-number, #b0851f)', comment: 'var(--whip-syntax-comment, #737373)',
  function: 'var(--whip-syntax-function, #3b7dd8)', type: 'var(--whip-syntax-type, #318795)',
  operator: 'var(--whip-syntax-operator, #7b5bb6)', punctuation: 'var(--whip-syntax-punctuation, #737373)',
});
export const markdown = stylex.defineVars({
  heading: 'var(--whip-markdown-heading, #1a1a1a)', strong: 'var(--whip-markdown-strong, #1a1a1a)',
  code: 'var(--whip-markdown-code, #3d9a57)', quote: 'var(--whip-markdown-quote, #737373)',
});
export const typography = stylex.defineVars({
  sans: "'Inter Variable', -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif",
  mono: "'JetBrains Mono Variable', ui-monospace, SFMono-Regular, monospace",
});
export const scale = stylex.defineConsts({
  space1: '4px', space2: '8px', space3: '12px', space4: '16px', space5: '20px', space6: '24px', space8: '32px',
  radiusSmall: '4px', radiusControl: '6px', radiusPanel: '10px', radiusDialog: '12px',
  motionFast: '100ms', motionNormal: '160ms',
  phone: '@media (max-width: 767px)', touch: '@media (pointer: coarse), (max-width: 767px)', reducedMotion: '@media (prefers-reduced-motion: reduce)',
});
// These roles deliberately derive from the palette's foreground/background,
// rather than using the TUI's low-contrast faint color for small browser text.
export const surface = stylex.defineVars({
  secondaryText: `color-mix(in srgb, ${colors.foreground} 76%, ${colors.background})`,
  quietBorder: `color-mix(in srgb, ${colors.border} 55%, ${colors.background})`,
  // applyTheme chooses the recessed surface for the palette's declared mode.
  navigation: `color-mix(in srgb, ${colors.background} 50%, ${colors.panel})`,
  inlineCode: colors.element,
  overlay: 'rgb(0 0 0 / 0.38)',
});
