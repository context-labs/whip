import { useState, type ReactNode } from 'react';
import { Dialog } from '@base-ui/react/dialog';
import { Icon } from '../ui/Icons';
import { useHydrated } from '../ui/useHydrated';
export function MobileNavigation({ title, children, className = '' }: { title: string; children: ReactNode; className?: string }) {
  const [open, setOpen] = useState(false);
  const hydrated = useHydrated();
  if (!hydrated) return <details className={`mobile-navigation mobile-navigation-fallback ${className}`}><summary>{title}</summary>{children}</details>;
  return <div className={`mobile-navigation ${className}`}><Dialog.Root open={open} onOpenChange={setOpen}>
    <Dialog.Trigger className="button button-secondary"><Icon name="menu" />{title}</Dialog.Trigger>
    <Dialog.Portal><Dialog.Backdrop className="dialog-backdrop" /><Dialog.Popup className="navigation-dialog">
      <div className="dialog-heading"><Dialog.Title>{title}</Dialog.Title><Dialog.Close className="icon-button" aria-label={`Close ${title.toLowerCase()}`}><Icon name="close" /></Dialog.Close></div>
      <div onClick={event => { if ((event.target as HTMLElement).closest('a')) setOpen(false); }}>{children}</div>
    </Dialog.Popup></Dialog.Portal>
  </Dialog.Root></div>;
}
