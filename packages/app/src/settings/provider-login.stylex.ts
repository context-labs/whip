import * as stylex from '@stylexjs/stylex';
import { colors, scale, surface, typography } from '@whip/ui/tokens.stylex';

export const loginStyles = stylex.create({
  dialog: { height: { default: 'min(384px, 85dvh)', '@media (max-width: 600px)': 'min(460px, 85dvh)' } },
  body: { flex: 1, minHeight: 0 },
  flow: { display: 'flex', flexDirection: 'column', gap: scale.space5, flex: 1, minHeight: 0 },
  content: { display: 'flex', flexDirection: 'column', gap: scale.space3, flex: '1 1 auto', minHeight: 0, overflowY: 'auto' },
  title: { display: 'flex', alignItems: 'center', gap: scale.space2, fontSize: typography.size14, fontWeight: 600, margin: 0, outline: 'none', flexShrink: 0 },
  text: { color: surface.secondaryText, fontSize: typography.size13, lineHeight: '20px', margin: 0, overflowWrap: 'anywhere' },
  footer: { display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: scale.space2, marginTop: 'auto', flexWrap: 'wrap', flexShrink: 0 },
  submit: { minWidth: 132 },
  full: { width: '100%' },
  codePanel: { display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: scale.space3, padding: scale.space4, backgroundColor: colors.panel, border: '1px solid', borderColor: surface.quietBorder, borderRadius: scale.radiusControl },
  code: { fontFamily: typography.mono, fontSize: 22, letterSpacing: '0.08em', overflowWrap: 'anywhere' },
  choices: { maxHeight: 180, minHeight: 0, flexShrink: 1, padding: scale.space3, border: '1px solid', borderColor: surface.quietBorder, borderRadius: scale.radiusControl },
});
