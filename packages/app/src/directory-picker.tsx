import { ErrorNotice } from './error-feedback';
import { useLayoutEffect, useRef, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { useWhipConnection } from '@whip/sdk/react';
import type { WhipClient } from '@whip/sdk';
import { Button, Dialog, Field, Input } from '@whip/ui';
import { ChevronUp, Folder, FolderOpen } from 'lucide-react';
import * as stylex from '@stylexjs/stylex';
import { layout } from './styles';

export function DirectoryPicker({
  client,
  value,
  onSelect,
  disabled,
  native = true,
  pickDirectory,
  compact = false,
}: {
  client: WhipClient;
  value: string;
  onSelect(path: string): void;
  disabled: boolean;
  native?: boolean;
  pickDirectory?(): Promise<string | undefined>;
  compact?: boolean;
}) {
  const connected = useWhipConnection(client).state === 'connected';
  const request = useRef<symbol | undefined>(undefined);
  useLayoutEffect(() => { setPicking(false); return () => { request.current = undefined; }; }, [client]);
  const [open, setOpen] = useState(false);
  const [picking, setPicking] = useState(false);
  const [path, setPath] = useState('');
  const [typed, setTyped] = useState('');
  const [after, setAfter] = useState<string>();
  const query = useQuery({
    queryKey: ['directories', client.getSnapshot().info?.runtime_id, path, after],
    queryFn: ({ signal }) =>
      client.host.directories(
        { path: path || undefined, after, limit: 64 },
        { signal },
      ),
    enabled: open && !disabled,
  });
  const navigate = (target: string) => {
    setPath(target);
    setTyped(target);
    setAfter(undefined);
  };
  const pickNative = async () => {
    const id = Symbol();
    request.current = id;
    setPicking(true);
    try {
      const result = pickDirectory ? { path: await pickDirectory() } : await client.host.pickDirectory({ start: value || undefined });
      if (request.current !== id) return;
      if (result.path) {
        onSelect(result.path);
      }
    } catch {
      if (request.current !== id) return;
      // Host has no desktop picker (headless, unsupported platform); use the web browser dialog.
      navigate(value);
      setOpen(true);
    } finally {
      if (request.current === id) { request.current = undefined; setPicking(false); }
    }
  };
  return (
    <>
      {native && <Button variant={compact ? "ghost" : "primary"} disabled={disabled || picking} onClick={pickNative}>
        <FolderOpen size={14} /> {picking ? 'Choosing folder…' : 'Choose folder…'}
      </Button>}
      {compact && native ? <details><summary>Folder options</summary><Button variant="ghost" disabled={disabled} onClick={() => { navigate(value); setOpen(true); }}>Browse host or enter a path</Button></details> : <Button
        variant={compact ? "ghost" : "secondary"}
        disabled={disabled}
        onClick={() => {
          navigate(value);
          setOpen(true);
        }}
      >
        <Folder size={14} /> {compact ? "Choose folder…" : "Browse host"}
      </Button>}
      <Dialog
        open={open}
        onOpenChange={setOpen}
        title="Choose a working directory"
        description="Directories on the execution host."
        footer={
          <Button
            disabled={!query.data || disabled}
            onClick={() => {
              if (query.data) {
                onSelect(query.data.path);
                setOpen(false);
              }
            }}
          >
            Use this folder
          </Button>
        }
      >
        <form
          {...stylex.props(layout.row)}
          onSubmit={(event) => {
            event.preventDefault();
            event.stopPropagation();
            navigate(typed);
          }}
        >
          <Field label="Host path">
            <Input
              value={typed}
              onChange={(event) => setTyped(event.target.value)}
            />
          </Field>
          <Button type="submit" variant="secondary">
            Go
          </Button>
        </form>
        <div {...stylex.props(layout.column)}>
          <p {...stylex.props(layout.muted)}>{query.data?.path}</p>
          {query.data?.parent && (
            <Button
              variant="ghost"
              onClick={() => navigate(query.data!.parent!)}
            >
              <ChevronUp size={14} /> Parent folder
            </Button>
          )}
          {query.isFetching && <p role="status">Loading directories…</p>}
          {query.error && connected && <ErrorNotice type="resource" owner={`directories:${path}`} title="Could not load folders" error={query.error} action={<Button variant="ghost" onClick={() => void query.refetch()}>Retry</Button>} />}
          {!connected && <p role="status">Folders are unavailable while this host is offline.</p>}
          {query.data?.entries?.map((entry) => (
            <Button
              variant="ghost"
              key={entry.path}
              onClick={() => navigate(entry.path)}
            >
              <Folder size={14} /> {entry.name}
            </Button>
          ))}
          {query.data?.has_more && (
            <Button
              variant="secondary"
              onClick={() => setAfter(query.data?.next_after)}
            >
              Next folders
            </Button>
          )}
          {after && (
            <Button variant="ghost" onClick={() => setAfter(undefined)}>
              First folders
            </Button>
          )}
          {query.data?.truncated && (
            <p>
              Directory listing is bounded; choose a subfolder or enter a path.
            </p>
          )}
        </div>
      </Dialog>
    </>
  );
}
