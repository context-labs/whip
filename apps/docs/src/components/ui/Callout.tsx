import { useId, type ReactNode } from 'react';
import * as stylex from '@stylexjs/stylex';
import { styles } from './Callout.stylex';

const typeStyles = { info: null, tip: styles.calloutTip, warning: styles.calloutWarning, alert: styles.calloutAlert } as const;
export function Callout({ title, type = 'info', children }: { title: string; type?: 'info' | 'tip' | 'warning' | 'alert'; children: ReactNode }) {
  const id = useId();
  return <aside {...stylex.props(styles.callout, typeStyles[type])} aria-labelledby={id}><div {...stylex.props(styles.calloutTitle)} id={id}>{title}</div><div {...stylex.props(styles.calloutBody)}>{children}</div></aside>;
}
