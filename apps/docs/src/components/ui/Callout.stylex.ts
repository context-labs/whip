import * as stylex from '@stylexjs/stylex';
import { colors, scale } from '~/tokens.stylex';

export const styles = stylex.create({
  callout: {
    paddingTop: '16px',
    paddingBottom: '16px',
    paddingLeft: '20px',
    paddingRight: '20px',
    borderWidth: '1px',
    borderStyle: 'solid',
    borderColor: colors.calloutInfoBorder,
    borderRadius: scale.radiusMd,
    backgroundColor: colors.surfacePanel,
    color: colors.text,
  },
  calloutTip: { borderColor: colors.calloutTipBorder },
  calloutWarning: { borderColor: colors.calloutWarningBorder },
  calloutAlert: { borderColor: colors.calloutAlertBorder },
  calloutTitle: {
    fontSize: '13px',
    lineHeight: '22px',
    fontWeight: 600,
    marginBottom: scale.space2,
  },
  calloutBody: {
    fontSize: '13px',
    lineHeight: '22px',
  },
});
