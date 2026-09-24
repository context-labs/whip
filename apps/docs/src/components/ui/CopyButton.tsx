import { useEffect, useRef, useState } from 'react';
import { IconButton } from './Button';
import { Icon } from './Icons';
import { useHydrated } from './useHydrated';

export function CopyButton({ text, label = 'Copy code', failureMessage = 'Copy failed. Select and copy the code manually.' }: { text: string; label?: string; failureMessage?: string }) {
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
  return <span className="copy-control">
    <IconButton label={status === 'copied' ? 'Copied' : label} onClick={() => void copy()}><Icon name={status === 'copied' ? 'check' : 'copy'} /></IconButton>
    <span className={status === 'failed' ? 'copy-error' : 'sr-only'} role="status">{message}</span>
  </span>;
}
