import { useEffect, useMemo, useRef, useState } from 'react';
import type { CapturedText, Client, ModelInspection, Session } from '@whip/sdk';
import { Button, CodeBlock } from '@whip/ui';
import * as stylex from '@stylexjs/stylex';
import { useRuntime } from '../context';
import { ErrorNotice } from '../error-feedback';
import { layout } from '../styles';

/** On-demand inspection of one exact attempt. Trace observation owns no bodies. */
export function ModelRequestInspection({
  client,
  sessionID,
  attemptID,
  connected,
}: {
  client: Client;
  sessionID: string;
  attemptID: string;
  connected: boolean;
}) {
  const session = useMemo(() => client.session(sessionID), [client, sessionID]);
  const pending = useRef<AbortController | null>(null);
  const [inspection, setInspection] = useState<ModelInspection>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  useEffect(() => {
    setBusy(false);
    return () => pending.current?.abort();
  }, [client, sessionID, attemptID]);
  useEffect(() => {
    if (!connected) {
      pending.current?.abort();
      setBusy(false);
    }
  }, [connected]);
  async function inspect() {
    pending.current?.abort();
    const controller = new AbortController();
    pending.current = controller;
    setBusy(true);
    setError('');
    try {
      const value = await session.models.inspection(attemptID, { signal: controller.signal });
      controller.signal.throwIfAborted();
      setInspection(value);
    } catch (failure) {
      if (!controller.signal.aborted) setError(String(failure));
    } finally {
      if (!controller.signal.aborted) setBusy(false);
    }
  }
  return (
    <section {...stylex.props(layout.column)} aria-label="Prepared model request">
      <Button
        variant="ghost"
        size="sm"
        disabled={!connected || busy}
        onClick={() => void inspect()}
      >
        {inspection ? 'Refresh request evidence' : 'Inspect prepared request'}
      </Button>
      {error && connected && (
        <ErrorNotice
          type="resource"
          owner={`${client.runtimeID}:${sessionID}:${attemptID}`}
          title="Could not inspect prepared request"
          error={error}
        />
      )}
      {inspection && (
        <>
          <p {...stylex.props(layout.muted)}>
            Prepared request digest: <code>{inspection.request_digest}</code>
          </p>
          {inspection.capture ? (
            <>
              <CapturedTextRead
                key={`instructions:${inspection.capture.instructions.digest}:${inspection.capture.instructions.status}`}
                session={session}
                connected={connected}
                body={inspection.capture.instructions}
                label="System instructions"
              />
              <CapturedTextRead
                key={`notices:${inspection.capture.notices.digest}:${inspection.capture.notices.status}`}
                session={session}
                connected={connected}
                body={inspection.capture.notices}
                label="Ephemeral notices"
              />
              <details>
                <summary>
                  Context provenance · {inspection.capture.messages.length} messages
                </summary>
                <p {...stylex.props(layout.muted)}>
                  These are identifiers and digests captured during preparation. Transcript bodies,
                  private provider state, credentials and attachment bytes are not copied into this
                  record.{' '}
                  {inspection.capture.context_complete
                    ? ''
                    : 'The descriptor list reached its 128-message limit.'}
                </p>
                <ol>
                  {inspection.capture.messages.map((message, index) => (
                    <li key={index}>
                      {message.role} · {message.id ?? 'Prepared context without a transcript ID'} ·{' '}
                      {message.parts_count} parts<code>{message.parts_digest}</code>
                    </li>
                  ))}
                </ol>
                <p>
                  Tools: {inspection.capture.tools_count} ·{' '}
                  <code>{inspection.capture.tools_digest}</code>
                </p>
              </details>
            </>
          ) : (
            <p {...stylex.props(layout.muted)}>
              This attempt has no retained instruction capture. Historical text cannot be
              reconstructed from current settings.
            </p>
          )}
          {inspection.compaction && (
            <>
              <p {...stylex.props(layout.muted)}>
                Compaction covers raw history through sequence{' '}
                {inspection.compaction.through_sequence}. This is the summary produced by this
                attempt, regardless of the current context selection.
              </p>
              <CapturedSummary session={session} inspection={inspection} connected={connected} />
            </>
          )}
        </>
      )}
    </section>
  );
}

const absentText = {
  oversized: 'exceeded the capture size limit',
  quota: 'reached the session content quota',
  storage_error: 'could not be stored',
};
function CapturedTextRead({
  session,
  connected,
  body,
  label,
}: {
  session: Session;
  connected: boolean;
  body: CapturedText;
  label: string;
}) {
  if (body.status !== 'available')
    return (
      <p {...stylex.props(layout.muted)}>
        {label} unavailable: {absentText[body.status]} · {body.bytes} bytes. No substitute text is
        shown.
      </p>
    );
  if (body.bytes === '0')
    return <p {...stylex.props(layout.muted)}>{label}: empty for this request.</p>;
  return (
    <BodyRead
      session={session}
      connected={connected}
      label={label}
      bytes={body.bytes}
      read={(download, signal) =>
        session.models.readText(body, { maxBytes: download ? 16 << 20 : 1 << 20, signal })
      }
    />
  );
}
function CapturedSummary({
  session,
  inspection,
  connected,
}: {
  session: Session;
  inspection: ModelInspection;
  connected: boolean;
}) {
  return (
    <BodyRead
      session={session}
      connected={connected}
      label="Compaction summary"
      bytes={inspection.compaction!.text_bytes}
      read={async (_download, signal) =>
        (await session.models.readCompaction(inspection, { signal })).text
      }
    />
  );
}
function BodyRead({
  session,
  connected,
  label,
  bytes,
  read,
}: {
  session: Session;
  connected: boolean;
  label: string;
  bytes: string;
  read(download: boolean, signal: AbortSignal): Promise<string>;
}) {
  const runtime = useRuntime();
  const pending = useRef<AbortController | null>(null);
  const [body, setBody] = useState<string>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [operation, setOperation] = useState<'resource' | 'action'>('resource');
  useEffect(() => {
    setBusy(false);
    return () => pending.current?.abort();
  }, [session.client, session.id]);
  useEffect(() => {
    if (!connected) {
      pending.current?.abort();
      setBusy(false);
    }
  }, [connected]);
  async function load(download: boolean) {
    pending.current?.abort();
    const controller = new AbortController();
    pending.current = controller;
    setBusy(true);
    setError('');
    setOperation(download ? 'action' : 'resource');
    try {
      const text = await read(download, controller.signal);
      controller.signal.throwIfAborted();
      if (download)
        await runtime.platform.download(
          new TextEncoder().encode(text),
          label.toLowerCase().replaceAll(' ', '-') + '.txt',
          'text/plain',
        );
      else setBody(text);
    } catch (failure) {
      if (!controller.signal.aborted) setError(String(failure));
    } finally {
      if (!controller.signal.aborted) setBusy(false);
    }
  }
  return (
    <div {...stylex.props(layout.column)}>
      <span>
        {label} · {bytes} bytes
      </span>
      {body !== undefined && <CodeBlock label={label} code={body} maxBytes={128 << 10} />}
      <div {...stylex.props(layout.row, layout.wrap)}>
        <Button
          variant="ghost"
          size="sm"
          disabled={busy || !connected || BigInt(bytes) > 1048576n}
          onClick={() => void load(false)}
        >
          Read {label.toLowerCase()}
        </Button>
        <Button
          variant="ghost"
          size="sm"
          disabled={busy || !connected}
          onClick={() => void load(true)}
        >
          Download {label.toLowerCase()}
        </Button>
      </div>
      {BigInt(bytes) > 1048576n && (
        <span {...stylex.props(layout.muted)}>
          This body exceeds the 1 MiB preview limit. Download retains its complete captured text.
        </span>
      )}
      {error && (connected || operation === 'action') && (
        <ErrorNotice
          type={operation}
          owner={`${session.client.runtimeID}:${session.id}:${label}`}
          title={`Could not ${operation === 'action' ? 'download' : 'read'} ${label.toLowerCase()}`}
          error={error}
        />
      )}
    </div>
  );
}
