import { useEffect, useRef, useState } from 'react';
import * as stylex from '@stylexjs/stylex';
import type { StyleXStyles } from '@stylexjs/stylex';
import { IconButton } from './Button';
import { Icon } from './Icons';
import { useHydrated } from './useHydrated';
import { styles } from './CopyButton.stylex';

export function CopyButton({ text, label = 'Copy code', failureMessage = 'Copy failed. Select and copy the code manually.', buttonStyle, errorStyle }: { text: string; label?: string; failureMessage?: string; buttonStyle?: StyleXStyles; errorStyle?: StyleXStyles }) {
  const hydrated = useHydrated();
  const [status, setStatus] = useState<'idle' | 'copied' | 'failed'>('idle');
  const reset = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const mounted = useRef(true);
  useEffect(() => {
    mounted.current = true;
    return () => { mounted.current = false; clearTimeout(reset.current); };
  }, []);
  useEffect(() => { setStatus('idle'); clearTimeout(reset.current); }, [text]);
  async function copy() {
    clearTimeout(reset.current);
    try {
      if (!navigator.clipboard?.writeText) throw new Error('Clipboard unavailable');
      await navigator.clipboard.writeText(text);
      if (mounted.current) setStatus('copied');
    } catch {
      if (mounted.current) setStatus('failed');
    }
    if (mounted.current) reset.current = setTimeout(() => setStatus('idle'), 4000);
  }
  if (!hydrated) return null;
  const message = status === 'copied' ? 'Copied' : status === 'failed' ? failureMessage : '';
  return <span {...stylex.props(styles.copyControl)}>
    <IconButton label={status === 'copied' ? 'Copied' : label} xstyle={buttonStyle} onClick={() => void copy()}><Icon name={status === 'copied' ? 'check' : 'copy'} /></IconButton>
    <span {...stylex.props(status === 'failed' && styles.copyError, status === 'failed' && errorStyle)} className={status === 'failed' ? undefined : 'sr-only'} role="status">{message}</span>
  </span>;
}
