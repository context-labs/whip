import type { ReactNode } from 'react';
import { ServerManager } from '../host-dialog';

export function ConnectionsSettings({ header }: { header?: ReactNode }) {
  return <div id="hosts" tabIndex={-1}><ServerManager header={header} /></div>;
}
