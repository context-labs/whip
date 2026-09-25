import * as stylex from '@stylexjs/stylex';
import { colors, scale } from '~/tokens.stylex';

export const styles = stylex.create({
  skipLink: {
    position: 'fixed',
    top: 12,
    left: 16,
    padding: '8px 12px',
    borderWidth: 1,
    borderStyle: 'solid',
    borderColor: colors.accent,
    borderRadius: scale.radiusMd,
    backgroundColor: colors.bg,
    color: colors.text,
    zIndex: 100,
    transform: { default: 'translateY(-200%)', ':focus': 'translateY(0)' },
  },
});
