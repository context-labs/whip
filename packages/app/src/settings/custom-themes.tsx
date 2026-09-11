import { ErrorNotice } from '../error-feedback';
import { useEffect, useRef, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { useWhipConnection } from '@whip/sdk/react';
import type { WhipClient } from '@whip/sdk';
import type { Resolved } from '@whip/protocol';
import { Button, Combobox, Dialog, Field, useTheme } from '@whip/ui';
import { FileJson, Upload } from 'lucide-react';
import * as stylex from '@stylexjs/stylex';
import { colors, surface, typography } from '@whip/ui/tokens.stylex';

export function themeFromHost(value: Resolved, namespace: string) {
  const { on_primary, border_focus, diff_add, diff_del, ...colors } =
    value.colors;
  return {
    ...value,
    id: `${namespace}:${value.id}`,
    web: value.web ? {
      ...(value.web.navigation ? {navigation: value.web.navigation} : {}),
      ...(value.web.quiet_border ? {quietBorder: value.web.quiet_border} : {}),
      ...(value.web.code_background ? {codeBackground: value.web.code_background} : {}),
      ...(value.web.inline_code_background ? {inlineCodeBackground: value.web.inline_code_background} : {}),
    } : undefined,
    colors: {
      ...colors,
      onPrimary: on_primary,
      borderFocus: border_focus,
      diffAdd: diff_add,
      diffDel: diff_del,
    },
  };
}
export function CustomThemes({ client }: { client: WhipClient }) {
  const connection = useWhipConnection(client);
  const theme = useTheme();
  const [open, setOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [errorType, setErrorType] = useState<'action' | 'validation'>('action');
  const [filename, setFilename] = useState('');
  const input = useRef<HTMLInputElement>(null);
  const trigger = useRef<HTMLButtonElement>(null);
  const request = useRef<AbortController | null>(null);
  useEffect(() => () => request.current?.abort(), [client]);
  function changeOpen(next: boolean) {
    request.current?.abort();
    request.current = null;
    setBusy(false); setError(''); setFilename(''); setOpen(next);
  }
  async function resolve(run: (signal: AbortSignal) => Promise<Resolved>, namespace: string) {
    request.current?.abort();
    const controller = new AbortController();
    request.current = controller; setBusy(true); setError(''); setErrorType('action');
    try {
      const value = await run(controller.signal);
      if (!controller.signal.aborted) { install(value, namespace); changeOpen(false); }
    } catch (error) { if (!controller.signal.aborted) setError(error instanceof Error ? error.message : String(error)); }
    finally { if (!controller.signal.aborted) { request.current = null; setBusy(false); } }
  }
  const enabled = connection.state === 'connected';
  const themes = useQuery({
    queryKey: ['host-themes', client.getSnapshot().info?.runtime_id],
    queryFn: ({ signal }) => client.host.themes.list({ signal }),
    enabled: enabled && open,
  });
  const custom =
    themes.data?.themes?.filter((item) => item.source !== 'builtin') ?? [];
  function install(resolved: Resolved, namespace: string) {
    const value = themeFromHost(resolved, namespace);
    theme.addTheme(value);
    theme.setTheme(value.id);
  }
  return (
    <>
      <Button ref={trigger} disabled={!enabled} onClick={() => changeOpen(true)}><Upload size={14} />Import theme</Button>
      <Dialog open={open} onOpenChange={changeOpen} title="Import theme" description="Make Whip your own with a custom color palette." finalFocus={trigger}
        footer={<Button onClick={() => changeOpen(false)}>Cancel</Button>}>
        <div {...stylex.props(styles.upload)}>
          <span {...stylex.props(styles.fileIcon)}><FileJson size={24} aria-hidden /></span>
          <div {...stylex.props(styles.copy)}>
            <strong {...stylex.props(styles.filename)}>{filename || 'Choose a theme JSON file'}</strong>
            <p {...stylex.props(styles.hint)}>Whip theme format · Up to 64 KiB</p>
          </div>
          <Button variant="primary" loading={busy} disabled={!enabled} onClick={() => input.current?.click()}><Upload size={14} />{busy ? 'Importing…' : 'Choose file'}</Button>
          <input ref={input} hidden type="file" aria-label="Theme JSON file" accept=".json,application/json" disabled={!enabled || busy}
            onChange={event => {
              const file = event.target.files?.[0];
              event.target.value = '';
              if (!file) return;
              setFilename(file.name);
              if (file.size > 64 * 1024) {
                setErrorType('validation');
                setError('This file is too large. Choose a theme JSON file smaller than 64 KiB.');
                return;
              }
              void resolve(async signal => {
                const json = await file.text();
                signal.throwIfAborted();
                return client.host.themes.resolveJSON(json, { signal });
              }, `import:${crypto.randomUUID()}`);
            }} />
        </div>
        <p {...stylex.props(styles.hint)}>Your imported theme is applied immediately and saved on this device.</p>
        {error && <ErrorNotice type={errorType} owner="theme-import" title={errorType === 'action' ? 'Could not import theme' : undefined} error={error} />}
        {!enabled && <p role="status">Theme import is unavailable while this host is offline.</p>}
      {custom.length > 0 && (
        <Field label="Or choose a local theme" description="Themes in your local Whip themes folder."><Combobox
          label="Local theme"
          placeholder="Search local themes…"
          options={custom.map((item) => ({ value: item.id, label: item.name }))}
          disabled={!enabled || busy}
          onValueChange={name => void resolve(signal => client.host.themes.resolve(name, { signal }), `host:${client.getSnapshot().info?.runtime_id}`)}
        /></Field>
      )}
      {themes.error && enabled && <ErrorNotice type="resource" owner="local-themes" title="Could not load local themes" error={themes.error} />}
      {themes.data?.errors?.map((error) => (
        <ErrorNotice key={error.file} type="resource" owner={`local-theme:${error.file}`} title={`Could not load ${error.file}`} error={error.message} />
      ))}
      {themes.data?.truncated && <p {...stylex.props(styles.hint)}>Some local themes could not be listed. You can still import a file directly.</p>}
      </Dialog>
    </>
  );
}

const styles = stylex.create({
  upload: { display: 'flex', flexDirection: 'column', alignItems: 'center', gap: 16, padding: 28, borderWidth: 1, borderStyle: 'dashed', borderColor: surface.quietBorder, borderRadius: 8, backgroundColor: colors.background, textAlign: 'center' },
  fileIcon: { display: 'flex', padding: 12, borderRadius: 12, backgroundColor: colors.element, color: surface.secondaryText },
  copy: { display: 'flex', flexDirection: 'column', gap: 6, minWidth: 0, maxWidth: '100%' },
  filename: { fontSize: typography.size14, fontWeight: 550, overflowWrap: 'anywhere' },
  hint: { margin: 0, color: surface.secondaryText, fontSize: typography.size12, lineHeight: 1.6 },
});
