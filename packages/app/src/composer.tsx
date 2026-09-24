import { typography } from '@whip/ui/tokens.stylex';
import {
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  useSyncExternalStore,
  type ReactNode,
  type RefObject,
} from 'react';
import type { Session } from '@whip/sdk';
import { Button, IconButton, Select, Textarea } from '@whip/ui';
import { ArrowUp, AtSign, Paperclip, Square } from 'lucide-react';
import * as stylex from '@stylexjs/stylex';
import { colors, surface, scale } from '@whip/ui/tokens.stylex';
import { useAppState, useRuntime } from './context';
import { CompletionPicker } from './completion-picker';
import { useSkillCompletion } from './use-skill-completion';
import { layout } from './styles';
import { ErrorNotice, type ErrorType } from './error-feedback';
import { errorMessage } from './platform';
import { selectedSessionTab } from './session-tabs';
import { submitChatInput } from './chat-submission';
import { ComposerAttachments } from './composer-attachments';
import { ChatFileDrop } from './chat-file-drop';

const styles = stylex.create({
  region: {
    position: 'relative',
    width: '100%',
    maxWidth: 864,
    alignSelf: 'center',
    paddingInline: { default: 24, [scale.phone]: 12 },
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
  pending = false,
  notice,
  agents,
  queue,
  queueEnabled = false,
  dropTarget,
  onAccepted,
}: {
  onAccepted?(): void;
  dropTarget?: RefObject<HTMLElement | null>;
  notice?: ReactNode;
  agents?: ReactNode;
  queue?: ReactNode;
  queueEnabled?: boolean;
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
  /** The session is still opening: keep the footprint, skip the unavailable hint. */
  pending?: boolean;
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
  const [delivery, setDelivery] = useState<'queued' | 'steer'>('queued');
  const [failure, setFailure] = useState<{ key: string; type: ErrorType; error: unknown; accepted?: boolean; outcome?: string; turnEventSeq?: string }>();
  const fail = (error: unknown, type: ErrorType = 'submission') => {
    if (mountedKey.current === key && !(error instanceof Error && error.name === 'AbortError')) setFailure({ key, type, error });
  };
  const input = useRef<HTMLTextAreaElement>(null);
  const files = useRef<HTMLInputElement>(null);
  const form = useRef<HTMLFormElement>(null);
  useLayoutEffect(() => {
    const element = input.current;
    const box = element?.parentElement;
    if (!element || !box) return;
    const fit = () => {
      // Measuring a collapsed input must not temporarily enlarge the transcript
      // viewport: the browser would clamp its scrollTop before we restore it.
      const minimum = box.style.minHeight;
      box.style.minHeight = `${box.getBoundingClientRect().height}px`;
      element.style.height = '0px';
      element.style.height = `${Math.min(220, Math.max(40, element.scrollHeight))}px`;
      box.style.minHeight = minimum;
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
  const skills = useSkillCompletion({
    client: session.client, owner: selectionKey, scope: { rootId: session.rootId, agentId },
    input, draft, change, connected, blocked: completion || sending || !active,
    rememberSelection: value => runtime.compositions.rememberSelection(selectionKey, value),
  });
  const attachmentUnavailable = !connected ? 'Reconnect to attach files.'
    : sending ? 'Wait for this message to be accepted before attaching files.'
    : attachments.some(item => !item.value && !item.error) ? 'Wait for the current upload to finish before attaching files.' : undefined;
  async function attach(selected: File[]) {
    if (attachmentUnavailable) { fail(new Error(attachmentUnavailable), 'validation'); return; }
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
    skills.dismiss();
    const text = runtime.draft(key);
    setFailure(undefined);
    const result = await submitChatInput({
      runtime, session, runtimeId, agentId, compositionKey: key, connected,
      text, attachments, delivery: queueEnabled ? 'queued' : delivery, activeTurn,
      onAccepted: () => {
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
        if (mountedKey.current === key) {
          if (!viewId || selectedSessionTab(runtime.tabs.workspace())?.id === viewId) input.current?.focus();
          onAccepted?.();
        }
      },
    });
    if (result.status === 'failed' && mountedKey.current === key
      && !(result.error instanceof Error && result.error.name === 'AbortError')) {
      setFailure({ key, type: 'submission', error: result.error, accepted: result.accepted,
        outcome: result.outcome, turnEventSeq: lastTurn?.event_seq });
    }
  }
  const recordedTurnOutcome = failure?.accepted && lastTurn && lastTurn.event_seq !== failure.turnEventSeq
    && (((failure.outcome === 'cancelled' || failure.outcome === 'interrupted') && lastTurn.status === failure.outcome)
      || (lastTurn.error && (errorMessage(failure.error) === lastTurn.error
        || (lastTurn.error_truncated && errorMessage(failure.error).startsWith(lastTurn.error)))));
  return (
    <form
      ref={form}
      {...stylex.props(styles.region)}
      onSubmit={(event) => {
        event.preventDefault();
        void submit();
      }}
    >
      <ChatFileDrop target={dropTarget ?? form} scope={key} unavailable={attachmentUnavailable}
        onFiles={attach} onError={message => fail(new Error(message), 'validation')} />
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
      {notice}
      {agents}
      {queue}
      <div {...stylex.props(styles.box)}>
        <ComposerAttachments attachments={attachments} owner={key}
          onRemove={id => runtime.compositions.remove(key, id)} />
          <Textarea
            ref={input}
            {...skills.inputProps}
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
            onChange={(event) => { change(event.target.value); skills.onChange(); }}
            onSelect={(event) => {
              runtime.compositions.rememberSelection(selectionKey, {
                start: event.currentTarget.selectionStart,
                end: event.currentTarget.selectionEnd,
              });
              skills.onSelect();
            }}
            placeholder="Describe what you want to do…"
            maxLength={256 * 1024}
            onKeyDown={(event) => {
              if (skills.onKeyDown(event)) return;
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
        {skills.popup}
        {activeTurn && !queueEnabled && (
          <Select
            label="Message delivery"
            xstyle={styles.delivery}
            value={delivery}
            onValueChange={(value) => setDelivery(value === 'steer' ? 'steer' : 'queued')}
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
            disabled={!!attachmentUnavailable}
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
              skills.dismiss();
              setCompletion(true);
            }}
          >
            <AtSign size={16} />
          </IconButton>
          <span {...stylex.props(layout.grow)} />
          {modelControl}
          {activeTurn && !draft.trim() && !attachments.length ? (
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
              aria-label={activeTurn ? !queueEnabled && delivery === 'steer' ? 'Steer current work' : 'Queue message' : 'Send message'}
              disabled={
                !connected ||
                (!draft.trim() && !attachments.length) ||
                sending ||
                !!unresolved ||
                attachments.some((item) => !item.value)
              }
              loading={sending}
            >
              {!sending && <ArrowUp size={16} />}
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
      {!connected && !pending && <div role="status" {...stylex.props(styles.hint)}>
        {unavailableReason ?? 'Reconnecting.'} Your draft stays here; it will not be sent automatically.
      </div>}
    </form>
  );
}
