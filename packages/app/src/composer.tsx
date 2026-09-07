import { useEffect, useRef, useState } from 'react';
import type { Session, InputAttachment } from '@whip/sdk';
import { Button, Field, IconButton, Select, Textarea } from '@whip/ui';
import { ArrowUp, Paperclip, X } from 'lucide-react';
import * as stylex from '@stylexjs/stylex';
import { colors, surface, scale } from '@whip/ui/tokens.stylex';
import { useAppState, useRuntime } from './context';
import { CompletionPicker } from './completion-picker';
import { layout } from './styles';

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
    borderColor: colors.border,
    borderRadius: 12,
    padding: 12,
    backgroundColor: colors.background,
    display: 'flex',
    flexDirection: 'column',
    gap: 8,
  },
  input: {
    borderWidth: 0,
    boxShadow: 'none',
    resize: 'none',
    minHeight: 64,
    maxHeight: 220,
    fontSize: { default: 14, [scale.phone]: 16 },
    backgroundColor: 'transparent',
  },
  hint: {
    color: surface.secondaryText,
    fontSize: 11,
    paddingTop: 8,
    textAlign: 'center',
  },
  hidden: { display: 'none' },
});

export function Composer({
  session,
  agentId,
  connected,
  activeTurn,
  runtimeId,
}: {
  session: Session;
  agentId: string;
  connected: boolean;
  activeTurn?: string;
  runtimeId: string;
}) {
  const runtime = useRuntime();
  const app = useAppState();
  const key = `${runtimeId}:${session.rootId}:${agentId}`;
  const [draft, setDraft] = useState(() => runtime.draft(key));
  const draftRef = useRef(draft);
  const [sending, setSending] = useState(false);
  const submission = useRef(0);
  const [completion, setCompletion] = useState(false);
  const selection = useRef({ start: 0, end: 0 });
  const [delivery, setDelivery] = useState('queued');
  const input = useRef<HTMLTextAreaElement>(null);
  const files = useRef<HTMLInputElement>(null);
  const [attachments, setAttachments] = useState<
    {
      id: string;
      name: string;
      size: number;
      value?: InputAttachment;
      error?: string;
    }[]
  >([]);
  const transfer = useRef(new AbortController());
  useEffect(() => {
    transfer.current = new AbortController();
    return () => transfer.current.abort();
  }, []);
  const unresolved = app.commands.find(
    (command) => command.draftKey === key && command.delivery,
  );
  useEffect(() => {
    const text = runtime.draft(key);
    draftRef.current = text;
    setDraft(text);
  }, [runtime, key]);
  const change = (text: string) => {
    try {
      runtime.setDraft(key, text);
      draftRef.current = text;
      setDraft(text);
    } catch (error) {
      runtime.report(error);
    }
  };
  async function attach(selected: File[]) {
    if (
      !connected ||
      sending ||
      attachments.some((item) => !item.value && !item.error)
    )
      return;
    const used = attachments.reduce((sum, item) => sum + item.size, 0);
    if (
      attachments.length + selected.length > 16 ||
      used + selected.reduce((sum, file) => sum + file.size, 0) >
        20 * 1024 * 1024
    ) {
      runtime.report('Attach at most 16 files totaling 20 MiB.');
      return;
    }
    const pending = selected.map((file) => ({
      id: crypto.randomUUID(),
      name: file.name,
      size: file.size,
    }));
    setAttachments((previous) => [...previous, ...pending]);
    for (const [index, file] of selected.entries()) {
      const id = pending[index]!.id;
      try {
        const kind = file.type.startsWith('image/') ? 'image' : 'text';
        if (kind === 'text' && file.size > 256 * 1024)
          throw new Error('Text attachments are limited to 256 KiB.');
        const bytes = new Uint8Array(await file.arrayBuffer());
        if (kind === 'text')
          new TextDecoder('utf-8', { fatal: true }).decode(bytes);
        const content = await session.client.upload(bytes, {
          rootId: session.rootId,
          agentId,
          mediaType: kind === 'image' ? file.type : 'text/plain',
          signal: transfer.current.signal,
        });
        setAttachments((previous) =>
          previous.map((item) =>
            item.id === id
              ? { ...item, value: content.asAttachment(kind, file.name) }
              : item,
          ),
        );
      } catch (error) {
        if (!transfer.current.signal.aborted)
          setAttachments((previous) =>
            previous.map((item) =>
              item.id === id
                ? {
                    ...item,
                    error:
                      error instanceof Error ? error.message : String(error),
                  }
                : item,
            ),
          );
      }
    }
  }
  async function submit() {
    const text = draftRef.current;
    if (
      !connected ||
      sending ||
      unresolved ||
      (!text.trim() && !attachments.length) ||
      attachments.some((item) => !item.value)
    )
      return;
    const generation = ++submission.current;
    setSending(true);
    try {
      const payload = {
        text,
        ...(attachments.length
          ? { attachments: attachments.map((item) => item.value!) }
          : {}),
      };
      const sentIds = new Set(attachments.map((item) => item.id));
      const command =
        agentId !== session.rootId
          ? session.agents.submit(agentId, payload, delivery)
          : delivery === 'steer' && activeTurn
            ? session.steer(payload)
            : session.submit(payload);
      await runtime.run(
        command,
        agentId === session.rootId ? 'Send message' : 'Message child',
        () => {
          if (draftRef.current === text) change('');
          setAttachments((previous) =>
            previous.filter((item) => !sentIds.has(item.id)),
          );
          if (generation === submission.current) setSending(false);
          input.current?.focus();
        },
        key,
      );
    } catch {
      /* Preserve drafts when acceptance is uncertain. Do not resubmit automatically. */
    } finally {
      if (generation === submission.current) setSending(false);
    }
  }
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
      {unresolved && (
        <div role="status" {...stylex.props(layout.notice)}>
          Delivery is {unresolved.delivery}. Check this command before sending
          again.
          <Button
            type="button"
            variant="ghost"
            disabled={!connected}
            onClick={() =>
              void runtime
                .checkCommand(unresolved.id)
                .catch((error) => runtime.report(error))
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
                  .catch((error) => runtime.report(error))
              }
            >
              Retry original submission
            </Button>
          )}
        </div>
      )}
      <div {...stylex.props(styles.box)}>
        {attachments.map((item) => (
          <div key={item.id} {...stylex.props(layout.row)}>
            <Paperclip size={13} />
            <span {...stylex.props(layout.grow)}>
              {item.name} ·{' '}
              {item.error || (item.value ? 'Ready' : 'Uploading…')}
            </span>
            <IconButton
              label={`Remove ${item.name}`}
              onClick={() =>
                setAttachments((previous) =>
                  previous.filter((value) => value.id !== item.id),
                )
              }
            >
              <X size={13} />
            </IconButton>
          </div>
        ))}
        <Field
          label={
            agentId === session.rootId ? 'Message WHIP' : 'Message this agent'
          }
        >
          <Textarea
            ref={input}
            data-whip-composer
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
        </Field>
        <div {...stylex.props(layout.row)}>
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
            disabled={
              !connected ||
              sending ||
              attachments.some((item) => !item.value && !item.error)
            }
            onClick={() => files.current?.click()}
          >
            <Paperclip size={15} />
          </IconButton>
          <Button
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
            @ Context
          </Button>
          {activeTurn && (
            <Select
              label="Message delivery"
              value={delivery}
              onValueChange={setDelivery}
              options={[
                { value: 'queued', label: 'Queue message' },
                { value: 'steer', label: 'Steer current work' },
              ]}
            />
          )}
          <span {...stylex.props(layout.grow)} />
          <Button
            type="submit"
            size="sm"
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
        </div>
      </div>
      {attachments.length > 0 && (
        <p {...stylex.props(styles.hint)}>
          Attachments remain in this open view. Removing one or leaving the view
          will not submit it; reselect files after reopening.
        </p>
      )}
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
      <div {...stylex.props(styles.hint)}>
        {connected
          ? 'Enter to send · Shift + Enter for a new line'
          : 'Reconnecting. Your draft stays here; it will not be sent automatically.'}
      </div>
    </form>
  );
}
