import { useEffect, useLayoutEffect, useRef, useState, useSyncExternalStore } from 'react';
import { Button, Dialog, Spinner, type DialogProps } from '@whip/ui';
import * as stylex from '@stylexjs/stylex';
import { connectionStyles as styles } from './host-connection.stylex';
import { useAppState, useRuntime } from './context';
import type { ConnectionProfile } from './connections';
import { HostPromptForm } from './host-prompt-form';
import { ErrorNotice } from './error-feedback';
import { errorMessage } from './platform';

export interface SSHConnectionRequest {
  profile: ConnectionProfile;
  save?: boolean;
  acceptIdentity?: boolean;
  onConnected(id: string): void;
  onCancel(): void;
  onEdit?(): void;
}
const noSubscribe = () => () => {};

/** One modal owns configuration, progress, authentication and recovery. */
export function HostConnectionDialog({ request, children, ...props }: DialogProps & { request?: SSHConnectionRequest }) {
  const runtime = useRuntime();
  const state = useAppState();
  const prompts = runtime.platform.hostPrompts;
  const hostId = request?.profile.id;
  const prompt = useSyncExternalStore(prompts?.subscribe ?? noSubscribe,
    () => hostId ? prompts?.forHost(hostId) ?? null : null);
  const [failure, setFailure] = useState<{ request: SSHConnectionRequest; message: string }>();
  const error = failure?.request === request ? failure?.message : undefined;
  const [retry, setRetry] = useState(0);
  const stop = useRef<() => void>(() => {});
  const heading = useRef<HTMLDivElement>(null);
  const previousRequest = useRef(request);
  useEffect(() => {
    if (!request && previousRequest.current && props.initialFocus && typeof props.initialFocus === 'object') props.initialFocus.current?.focus();
    previousRequest.current = request;
  }, [request, props.initialFocus]);
  useLayoutEffect(() => hostId ? prompts?.claim(hostId) : undefined, [hostId, prompts]);
  useEffect(() => {
    if (!request || !props.open) return;
    const controller = new AbortController();
    let retired = false;
    let settled = false;
    let started = false;
    const cancel = () => {
      if (settled || controller.signal.aborted) return;
      controller.abort();
      if (started && !request.save) runtime.connections.disconnect(request.profile.id);
    };
    stop.current = cancel;
    setFailure(undefined);
    if (!prompts?.forHost(request.profile.id)) heading.current?.focus();
    // Defer one microtask so StrictMode's discarded effect never starts SSH.
    void Promise.resolve().then(async () => {
      if (retired) return;
      started = true;
      try {
        const id = request.save
          ? await runtime.connections.saveNative(request.profile, request.acceptIdentity, controller.signal)
          : (await runtime.connections.connect(request.profile.id), request.profile.id);
        if (retired || controller.signal.aborted) return;
        settled = true;
        request.onConnected(id);
      } catch (error) {
        if (retired || controller.signal.aborted) return;
        settled = true;
        setFailure({ request, message: errorMessage(error) });
      }
    });
    return () => { retired = true; cancel(); stop.current = () => {}; };
  }, [request, retry, runtime, prompts, props.open]);
  const cancel = () => { stop.current(); request?.onCancel(); };
  const host = state.hosts.find(item => item.id === hostId);
  return <Dialog {...props} title={request ? `Connect to ${request.profile.label}` : props.title}
    initialFocus={request ? heading : props.initialFocus}
    onOpenChange={open => { if (!open && request) cancel(); else props.onOpenChange(open); }}>
    {children && <div hidden={!!request}>{children}</div>}
    {request && <div {...stylex.props(styles.connection)}>
      {prompt && prompts && !error ? <HostPromptForm key={prompt.serial} active={prompt} prompts={prompts} onCancel={cancel} /> : <>
        <div ref={heading} tabIndex={-1} {...stylex.props(styles.body)}>
          {error ? <ErrorNotice type="action" owner={hostId!} title="Couldn’t connect" error={error} />
            : <div role="status" {...stylex.props(styles.progress)}><span aria-hidden><Spinner /></span><span>{host?.progress || `Connecting to ${request.profile.label}…`}</span></div>}
        </div>
        <div {...stylex.props(styles.footer)}>
          {error ? <>
            <Button variant="ghost" onClick={() => props.onOpenChange(false)}>Close</Button>
            {request.onEdit && <Button variant="secondary" onClick={request.onEdit}>Edit connection</Button>}
            <Button variant="primary" onClick={() => { setFailure(undefined); setRetry(value => value + 1); }}>Try again</Button>
          </> : <Button variant="ghost" onClick={cancel}>Cancel connection</Button>}
        </div>
      </>}
    </div>}
  </Dialog>;
}
