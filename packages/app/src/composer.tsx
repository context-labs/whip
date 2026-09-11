import { typography } from '@whip/ui/tokens.stylex';
import {
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  useSyncExternalStore,
  type ReactNode,
} from 'react';
import type { Session } from '@whip/sdk';
import { Button, IconButton, Select, Textarea } from '@whip/ui';
import { ArrowUp, AtSign, Paperclip, Square, X } from 'lucide-react';
import * as stylex from '@stylexjs/stylex';
import { colors, surface, scale } from '@whip/ui/tokens.stylex';
import { useAppState, useRuntime } from './context';
import { CompletionPicker } from './completion-picker';
import { layout } from './styles';
import { ErrorNotice, type ErrorType } from './error-feedback';
import { errorMessage } from './platform';
import { selectedSessionTab } from './session-tabs';

const styles = stylex.create({
  region: {
    width: '100%',
    maxWidth: 864,
    alignSelf: 'center',
    paddingInline: { default: 24, [scale.phone]: 12 },
    paddingTop: 8,
    paddingBottom: 'max(16px, env(safe-area-inset-bottom))',
  },
  box: {
    borderWidth: 1,
    borderStyle: 'solid',
    borderColor: surface.quietBorder,
    borderRadius: 20,
    padding: 12,
    backgroundColor: colors.element,
    display: 'flex',
    flexDirection: 'column',
    gap: 8,
  },
  input: {
    borderWidth: 0,
    boxShadow: 'none',
    resize: 'none',
    minHeight: 40,
    maxHeight: 220,
    fontSize: { default: typography.size14, [scale.phone]: typography.size16 },
    padding: 4,
    backgroundColor: { default: 'transparent', ':hover': 'transparent' },
    outline: { default: 'none', ':focus-visible': 'none' },
  },
  toolbar: { display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 4, minWidth: 0 },
  delivery: { alignSelf: 'flex-start', maxWidth: '100%' },
  send: { borderRadius: '50%', width: { default: 32, [scale.touch]: 44 }, paddingInline: 0 },
  hint: {
    color: surface.secondaryText,
    fontSize: typography.size11,
    paddingTop: 8,
    textAlign: 'center',
  },
  hidden: { display: 'none' },
});

export function Composer({
  session,
  agentId,
  connected,
  unavailableReason,
  activeTurn,
  lastTurn,
  runtimeId,
  viewId,
  modelControl,
  active = false,
}: {
  session: Session;
  agentId: string;
  connected: boolean;
  unavailableReason?: string;
  activeTurn?: string;
  lastTurn?: { event_seq: string; status?: string; error?: string; error_truncated?: boolean };
  runtimeId: string;
  viewId?: string;
  modelControl?: ReactNode;
  active?: boolean;
}) {
  const runtime = useRuntime();
  const app = useAppState();
  const key = `${runtimeId}:${session.rootId}:${agentId}`;
  const selectionKey = viewId ? `${runtimeId}:${viewId}:${agentId}` : key;
  const [draft, setDraft] = useState(() => runtime.draft(key));
  const draftRef = useRef(draft);
  const { sending, attachments } = useSyncExternalStore(
    runtime.compositions.subscribe,
    () => runtime.compositions.get(key),
  );
  const mountedKey = useRef<string | undefined>(undefined);
  const [completion, setCompletion] = useState(false);
  const selection = useRef({ start: 0, end: 0 });
  const [delivery, setDelivery] = useState('queued');
  const [failure, setFailure] = useState<{ key: string; type: ErrorType; error: unknown; accepted?: boolean; outcome?: string; turnEventSeq?: string }>();
  const fail = (error: unknown, type: ErrorType = 'submission') => {
    if (mountedKey.current === key && !(error instanceof Error && error.name === 'AbortError')) setFailure({ key, type, error });
  };
  const input = useRef<HTMLTextAreaElement>(null);
  const files = useRef<HTMLInputElement>(null);
  useLayoutEffect(() => {
    const element = input.current;
    if (!element) return;
    const fit = () => {
      element.style.height = '0px';
      element.style.height = `${Math.min(220, Math.max(40, element.scrollHeight))}px`;
    };
    fit();
    let width = element.clientWidth;
    const observer = new ResizeObserver(() => {
      if (element.clientWidth === width) return;
      width = element.clientWidth;
      fit();
    });
    observer.observe(element);
    return () => observer.disconnect();
  }, [draft]);
  useLayoutEffect(() => {
    mountedKey.current = key;
    const saved = runtime.compositions.selection(selectionKey);
    if (saved) input.current?.setSelectionRange(saved.start, saved.end);
    return () => {
      mountedKey.current = undefined;
    };
  }, [runtime, key, selectionKey]);
  useEffect(() => {
    if (!active || !window.matchMedia('(min-width: 768px)').matches) return;
    const frame = requestAnimationFrame(() => {
      const focused = document.activeElement;
      if (focused instanceof HTMLElement && focused.closest(
        '[aria-modal="true"], [role="dialog"], [role="alertdialog"], [role="menu"], [role="listbox"]',
      )) return;
      input.current?.focus({ preventScroll: true });
    });
    return () => cancelAnimationFrame(frame);
  }, [active, key]);
  useEffect(() => {
    const update = () => { const text = runtime.draft(key); draftRef.current = text; setDraft(text); };
    update();
    return runtime.subscribeDraft(key, update);
  }, [runtime, key]);
  const unresolved = app.commands.find(
    (command) => command.draftKey === key && command.delivery,
  );
  useEffect(() => {
    const text = runtime.draft(key);
    draftRef.current = text;
    setDraft(text);
  }, [runtime, key, sending, app]);
  const change = (text: string) => {
    try {
      runtime.setDraft(key, text);
      draftRef.current = text;
      setDraft(text);
      setFailure(undefined);
    } catch (error) {
      fail(error, 'validation');
    }
  };
  async function attach(selected: File[]) {
    if (
      !connected ||
      sending ||
      attachments.some((item) => !item.value && !item.error)
    )
      return;
    try {
      setFailure(undefined);
      await runtime.compositions.add(
        key,
        session,
        runtimeId,
        agentId,
        selected,
      );
    } catch (error) {
      fail(error, 'validation');
    }
  }
  async function submit() {
    const text = runtime.draft(key);
    if (
      !connected ||
      sending ||
      unresolved ||
      (!text.trim() && !attachments.length) ||
      attachments.some((item) => !item.value)
    )
      return;
    let token: symbol | undefined;
    try {
      token = runtime.compositions.beginSubmission(key);
    } catch (error) {
      fail(error);
      return;
    }
    if (!token) return;
    let inputId: string | undefined;
    let accepted = false;
    setFailure(undefined);
    try {
      const payload = {
        text,
        ...(attachments.length
          ? { attachments: attachments.map((item) => item.value!) }
          : {}),
      };
      const sentIds = attachments.map((item) => item.id);
      inputId = runtime.submittedInputs.add({ runtimeId, rootId: session.rootId, agentId },
        text + (attachments.length ? `\n${attachments.length} attached files` : ''), !!activeTurn);
      const command =
        agentId !== session.rootId
          ? session.command('agent.submit', { id: agentId, ...payload, delivery }, { commandId: inputId })
          : delivery === 'steer' && activeTurn
            ? session.steer(payload, { commandId: inputId })
            : session.submit(payload, { commandId: inputId });
      await runtime.run(
        command,
        agentId === session.rootId ? 'Send message' : 'Message child',
        () => {
          accepted = true;
          try {
            if (runtime.draft(key) === text) {
              runtime.setDraft(key, '');
              if (mountedKey.current === key) {
                draftRef.current = '';
                setDraft('');
              }
            }
          } catch (error) {
            runtime.report(error);
          }
          runtime.compositions.clear(key, sentIds);
          runtime.compositions.finishSubmission(key, token!);
          if (mountedKey.current === key && (!viewId || selectedSessionTab(runtime.tabs.workspace())?.id === viewId)) input.current?.focus();
        },
        key,
      );
    } catch (error) {
      const command = runtime.getSnapshot().commands.find(item => item.commandId === inputId && item.runtimeId === runtimeId);
      if (inputId && !command?.delivery) runtime.submittedInputs.remove(inputId, runtimeId);
      if (mountedKey.current === key && !(error instanceof Error && error.name === 'AbortError'))
        setFailure({ key, type: 'submission', error, accepted, outcome: command?.status, turnEventSeq: lastTurn?.event_seq });
      /* Preserve drafts when acceptance is uncertain. Do not resubmit automatically. */
    } finally {
      runtime.compositions.finishSubmission(key, token);
    }
  }
  const recordedTurnOutcome = failure?.accepted && lastTurn && lastTurn.event_seq !== failure.turnEventSeq
    && (((failure.outcome === 'cancelled' || failure.outcome === 'interrupted') && lastTurn.status === failure.outcome)
      || (lastTurn.error && (errorMessage(failure.error) === lastTurn.error
        || (lastTurn.error_truncated && errorMessage(failure.error).startsWith(lastTurn.error)))));
  return (
    <form
      {...stylex.props(styles.region)}
      onDragOver={(event) => {
        if (event.dataTransfer.types.includes('Files')) event.preventDefault();
      }}
      onDrop={(event) => {
        if (event.dataTransfer.files.length) {
          event.preventDefault();
          void attach(Array.from(event.dataTransfer.files));
        }
      }}
      onSubmit={(event) => {
        event.preventDefault();
        void submit();
      }}
    >
      {failure?.key === key && !unresolved && !recordedTurnOutcome && <ErrorNotice type={failure.type} owner={key} error={failure.error}
        tone={failure.outcome === 'cancelled' || failure.outcome === 'interrupted' ? 'neutral' : 'error'}
        title={failure.outcome === 'cancelled' ? 'Your message was cancelled' : failure.outcome === 'interrupted' ? 'Your message was interrupted' : failure.accepted ? 'Your message could not complete' : undefined}
        onDismiss={() => setFailure(undefined)} />}
      {unresolved && (
        <ErrorNotice type="submission" owner={key} tone="warning"
          title={unresolved.delivery === 'absent' ? 'Your message was not received' : 'Checking whether your message was received'}
          error={(failure?.key === key ? failure.error : undefined) || unresolved.error || 'Check this command before sending again. Your draft is preserved.'} action={<>
          <Button
            type="button"
            variant="ghost"
            disabled={!connected}
            onClick={() =>
              void runtime
                .checkCommand(unresolved.id)
                .catch((error) => fail(error))
            }
          >
            Check status
          </Button>
          {unresolved.delivery === 'absent' && (
            <Button
              type="button"
              variant="secondary"
              disabled={!connected}
              onClick={() =>
                void runtime
                  .retryCommand(unresolved.id)
                  .catch((error) => fail(error))
              }
            >
              Retry original submission
            </Button>
          )}
        </>} />
      )}
      <div {...stylex.props(styles.box)}>
        {attachments.map((item) => (
          <div key={item.id} {...stylex.props(layout.row)}>
            <Paperclip size={13} />
            <span {...stylex.props(layout.grow)}>
              {item.name} ·{' '}
              {item.value ? 'Ready' : item.error ? 'Upload failed' : 'Uploading…'}
              {item.error && <ErrorNotice type="resource" owner={`${key}:${item.id}`} title={`${item.name} could not upload`} error={item.error} />}
            </span>
            <IconButton
              label={`Remove ${item.name}`}
              onClick={() => runtime.compositions.remove(key, item.id)}
            >
              <X size={13} />
            </IconButton>
          </div>
        ))}
          <Textarea
            ref={input}
            data-whip-composer
            aria-label={agentId === session.rootId ? 'Message WHIP' : 'Message this agent'}
            rows={1}
            onPaste={(event) => {
              const images = Array.from(event.clipboardData.files).filter(
                (file) => file.type.startsWith('image/'),
              );
              if (images.length) {
                event.preventDefault();
                void attach(images);
              }
            }}
            xstyle={styles.input}
            value={draft}
            onChange={(event) => change(event.target.value)}
            onSelect={(event) =>
              runtime.compositions.rememberSelection(selectionKey, {
                start: event.currentTarget.selectionStart,
                end: event.currentTarget.selectionEnd,
              })
            }
            placeholder="Describe what you want to do…"
            maxLength={256 * 1024}
            onKeyDown={(event) => {
              if (
                event.key === 'Enter' &&
                !event.shiftKey &&
                !event.nativeEvent.isComposing &&
                !event.altKey &&
                !event.ctrlKey &&
                !event.metaKey
              ) {
                event.preventDefault();
                void submit();
              }
            }}
          />
        {activeTurn && (
          <Select
            label="Message delivery"
            xstyle={styles.delivery}
            value={delivery}
            onValueChange={setDelivery}
            options={[
              { value: 'queued', label: 'Queue message' },
              { value: 'steer', label: 'Steer current work' },
            ]}
          />
        )}
        <div {...stylex.props(styles.toolbar)}>
          <input
            ref={files}
            type="file"
            multiple
            accept="image/*,.txt,.md,.go,.py,.js,.ts,.tsx,.json,.yaml,.yml,.toml,.csv,.log"
            {...stylex.props(styles.hidden)}
            onChange={(event) => {
              const selected = Array.from(event.target.files ?? []);
              event.target.value = '';
              void attach(selected);
            }}
          />
          <IconButton
            label="Attach text or images"
            variant="ghost"
            disabled={
              !connected ||
              sending ||
              attachments.some((item) => !item.value && !item.error)
            }
            onClick={() => files.current?.click()}
          >
            <Paperclip size={15} />
          </IconButton>
          <IconButton
            label="Add context"
            type="button"
            variant="ghost"
            disabled={!connected || sending}
            onClick={() => {
              selection.current = {
                start: input.current?.selectionStart ?? draft.length,
                end: input.current?.selectionEnd ?? draft.length,
              };
              setCompletion(true);
            }}
          >
            <AtSign size={16} />
          </IconButton>
          <span {...stylex.props(layout.grow)} />
          {modelControl}
          {activeTurn ? (
            <Button
              type="button"
              variant="primary"
              xstyle={styles.send}
              aria-label="Pause this turn"
              title="Pause this turn"
              disabled={!connected}
              onClick={() =>
                void runtime
                  .run(
                    agentId === session.rootId
                      ? session.cancelTurn(activeTurn)
                      : session.agents.cancelTurn(agentId, activeTurn),
                    'Stop turn',
                  )
                  .catch(error => fail(error, 'action'))
              }
            >
              <Square size={14} fill="currentColor" strokeWidth={0} />
            </Button>
          ) : (
            <Button
              type="submit"
              variant="primary"
              xstyle={styles.send}
              aria-label="Send message"
              disabled={
                !connected ||
                (!draft.trim() && !attachments.length) ||
                sending ||
                !!unresolved ||
                attachments.some((item) => !item.value)
              }
              loading={sending}
            >
              <ArrowUp size={16} />
            </Button>
          )}
        </div>
      </div>
      {completion && (
        <CompletionPicker
          session={session}
          agentId={agentId}
          onClose={() => setCompletion(false)}
          onSelect={(text) => {
            const current = draftRef.current;
            const { start, end } = selection.current;
            change(`${current.slice(0, start)}${text} ${current.slice(end)}`);
            requestAnimationFrame(() => {
              input.current?.focus();
              input.current?.setSelectionRange(
                start + text.length + 1,
                start + text.length + 1,
              );
            });
          }}
        />
      )}
      {!connected && <div role="status" {...stylex.props(styles.hint)}>
        {unavailableReason ?? 'Reconnecting.'} Your draft stays here; it will not be sent automatically.
      </div>}
    </form>
  );
}
