import * as stylex from '@stylexjs/stylex';
import { colors, typography } from '~/tokens.stylex';

// Shared typographic styles replacing the global .label and .page-lead classes.
// Components that previously received these via className now compose them with
// stylex.props; the CSS classes remain for any consumer not yet migrated.
export const text = stylex.create({
  label: {
    fontSize: typography.label,
    lineHeight: 'var(--leading-label)',
    fontWeight: 'var(--weight-medium)',
    letterSpacing: 'var(--tracking-label)',
    textTransform: 'uppercase',
    color: colors.textMuted,
  },
  pageLead: {
    fontSize: typography.lead,
    lineHeight: 'var(--leading-lead)',
    color: colors.text,
  },
});
