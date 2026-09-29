import { RemoteDirectoryDialog, type RemoteDirectoryHost } from './remote-directory-dialog';
import { useEffect, useLayoutEffect, useRef, useState } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import type { Client } from '@whip/sdk';
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
  connected,
  native = true,
  pickDirectory,
  compact = false,
  sessionTrigger = false,
  host,
}: {
  client: Client;
  value: string;
  onSelect(path: string): void;
  disabled: boolean;
  connected: boolean;
  native?: boolean;
  pickDirectory?(): Promise<string | undefined>;
  compact?: boolean;
  sessionTrigger?: boolean;
  host?: RemoteDirectoryHost;
}) {
  const cache = directoryCache(useQueryClient());
  useEffect(() => () => cache.cancel(client), [cache, client, client.runtimeID]);
  useEffect(() => { if (!connected || disabled) cache.cancel(client); }, [cache, client, connected, disabled]);
  const warm = () => { if (!native && connected && !disabled) cache.warm(client, { path: value }); };
  const request = useRef<symbol | undefined>(undefined);
  useLayoutEffect(() => { setPicking(false); setOpen(false); return () => { request.current = undefined; }; }, [client, client.runtimeID, connected]);
  const [open, setOpen] = useState(false);
  const [picking, setPicking] = useState(false);
  const [browserClient, setBrowserClient] = useState<Client>();
  const [browserRuntime, setBrowserRuntime] = useState<string>();
  const browse = () => { setBrowserClient(client); setBrowserRuntime(client.runtimeID); setOpen(true); };
  const pickNative = async () => {
    const id = Symbol();
    request.current = id;
    setPicking(true);
    try {
      const result = pickDirectory ? { path: await pickDirectory() } : await client.call('host.directory.pick', { start: value });
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
      {sessionTrigger ? <Button variant="ghost" aria-label="Project folder" title={value || 'Choose a project folder'} disabled={!connected || disabled || picking} xstyle={styles.trigger}
        onPointerEnter={warm} onFocus={warm} onClick={native ? pickNative : browse}>
        <FolderOpen size={14} /><span {...stylex.props(layout.ellipsis)}>{value.split(/[\\/]/).filter(Boolean).at(-1) || value || 'Choose folder'}</span><ChevronDown size={14} />
      </Button> : native && <Button variant={compact ? "ghost" : "primary"} disabled={!connected || disabled || picking} onClick={pickNative}>
        <FolderOpen size={14} /> {picking ? 'Choosing folder…' : 'Choose folder…'}
      </Button>}
      {!sessionTrigger && (compact && native ? <details><summary>Folder options</summary><Button variant="ghost" disabled={!connected || disabled} onClick={browse}>Browse host or enter a path</Button></details> : <Button
        variant={compact ? "ghost" : "secondary"}
        disabled={!connected || disabled}
        onPointerEnter={warm} onFocus={warm}
        onClick={browse}
      >
        <Folder size={14} /> {compact ? "Choose folder…" : "Browse host"}
      </Button>)}
      {open && browserClient === client && browserRuntime === client.runtimeID && <RemoteDirectoryDialog
        client={client} value={value} disabled={!connected || disabled} connected={connected} host={host} onClose={() => setOpen(false)}
        onSelect={path => { onSelect(path); setOpen(false); }} />}

    </>
  );
}

const styles = stylex.create({ trigger: { maxWidth: '100%', minWidth: 0 } });
