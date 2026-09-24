import { useId, type ReactNode } from 'react';
export function Callout({ title, type = 'info', children }: { title: string; type?: 'info' | 'tip' | 'warning' | 'alert'; children: ReactNode }) {
  const id = useId();
  return <aside className={`callout callout-${type}`} aria-labelledby={id}><div className="callout-title" id={id}>{title}</div><div className="callout-body">{children}</div></aside>;
}
