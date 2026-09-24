import { RemoteDirectoryDialog, type RemoteDirectoryHost } from './remote-directory-dialog';
import { useEffect, useLayoutEffect, useRef, useState } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { useWhipConnection } from '@whip/sdk/react';
import type { WhipClient } from '@whip/sdk';
import { Button } from '@whip/ui';
import { ChevronDown, Folder, FolderOpen } from 'lucide-react';
import * as stylex from '@stylexjs/stylex';
import { layout } from './styles';
import { directoryCache } from './directory-queries';

export function DirectoryPicker({
  client,
  value,
  onSelect,
  disabled,
  native = true,
  pickDirectory,
  compact = false,
  sessionTrigger = false,
  host,
}: {
  client: WhipClient;
  value: string;
  onSelect(path: string): void;
  disabled: boolean;
  native?: boolean;
  pickDirectory?(): Promise<string | undefined>;
  compact?: boolean;
  sessionTrigger?: boolean;
  host?: RemoteDirectoryHost;
}) {
  const connection = useWhipConnection(client);
  const cache = directoryCache(useQueryClient());
  useEffect(() => () => cache.cancel(client), [cache, client, connection.info?.runtime_id]);
  const warm = () => { if (!native && !disabled) cache.warm(client, { path: value }); };
  const request = useRef<symbol | undefined>(undefined);
  useLayoutEffect(() => { setPicking(false); setOpen(false); return () => { request.current = undefined; }; }, [client, connection.info?.runtime_id]);
  const [open, setOpen] = useState(false);
  const [picking, setPicking] = useState(false);
  const [browserClient, setBrowserClient] = useState<WhipClient>();
  const [browserRuntime, setBrowserRuntime] = useState<string>();
  const browse = () => { setBrowserClient(client); setBrowserRuntime(connection.info?.runtime_id); setOpen(true); };
  const pickNative = async () => {
    const id = Symbol();
    request.current = id;
    setPicking(true);
    try {
      const result = pickDirectory ? { path: await pickDirectory() } : await client.host.pickDirectory({ start: value || undefined });
      if (request.current !== id) return;
      if (result.path) {
        onSelect(result.path);
        setOpen(false);
      }
    } catch {
      if (request.current !== id) return;
      // Host has no desktop picker (headless, unsupported platform); use the web browser dialog.
      browse();
    } finally {
      if (request.current === id) { request.current = undefined; setPicking(false); }
    }
  };
  return (
    <>
      {sessionTrigger ? <Button variant="ghost" aria-label="Project folder" title={value || 'Choose a project folder'} disabled={disabled || picking} xstyle={styles.trigger}
        onPointerEnter={warm} onFocus={warm} onClick={native ? pickNative : browse}>
        <FolderOpen size={14} /><span {...stylex.props(layout.ellipsis)}>{value.split(/[\\/]/).filter(Boolean).at(-1) || value || 'Choose folder'}</span><ChevronDown size={14} />
      </Button> : native && <Button variant={compact ? "ghost" : "primary"} disabled={disabled || picking} onClick={pickNative}>
        <FolderOpen size={14} /> {picking ? 'Choosing folder…' : 'Choose folder…'}
      </Button>}
      {!sessionTrigger && (compact && native ? <details><summary>Folder options</summary><Button variant="ghost" disabled={disabled} onClick={browse}>Browse host or enter a path</Button></details> : <Button
        variant={compact ? "ghost" : "secondary"}
        disabled={disabled}
        onPointerEnter={warm} onFocus={warm}
        onClick={browse}
      >
        <Folder size={14} /> {compact ? "Choose folder…" : "Browse host"}
      </Button>)}
      {open && browserClient === client && browserRuntime === connection.info?.runtime_id && <RemoteDirectoryDialog
        client={client} value={value} disabled={disabled} host={host} onClose={() => setOpen(false)}
        onSelect={path => { onSelect(path); setOpen(false); }} />}

    </>
  );
}

const styles = stylex.create({ trigger: { maxWidth: '100%', minWidth: 0 } });
