import * as stylex from '@stylexjs/stylex';
import { colors, typography, scale } from '~/tokens.stylex';

export const styles = stylex.create({
  copyControl: {
    display: 'inline-flex',
    alignItems: 'center',
    flexShrink: 0,
    position: 'relative',
  },
  copyError: {
    position: 'absolute',
    top: 'calc(100% + 8px)',
    right: 0,
    zIndex: 5,
    paddingTop: '8px',
    paddingBottom: '8px',
    paddingLeft: '12px',
    paddingRight: '12px',
    width: '220px',
    borderWidth: '1px',
    borderStyle: 'solid',
    borderColor: colors.calloutAlertBorder,
    borderRadius: scale.radiusMd,
    backgroundColor: colors.bg,
    color: colors.text,
    fontFamily: typography.sans,
    fontSize: '12px',
    lineHeight: '20px',
  },
});
