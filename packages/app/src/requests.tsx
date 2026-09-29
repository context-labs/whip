import { typography } from '@whip/ui/tokens.stylex';
import { useEffect, useId, useMemo, useRef, useState } from 'react';
import { ErrorNotice } from './error-feedback';
import type {
  HostOperation,
  Operations,
  Permission,
  Question,
  Session,
} from '@whip/sdk';
import type { DeepReadonly } from '@whip/sdk/state';
import { useQuery } from '@tanstack/react-query';
import { Button, IconButton, Input } from '@whip/ui';
import * as stylex from '@stylexjs/stylex';
import { CircleHelp, PenLine, ShieldAlert, X } from 'lucide-react';
import { colors, surface, scale } from '@whip/ui/tokens.stylex';
import { layout } from './styles';
import { operationSubject } from './chat-activity-rows';

function captureAnswerFocus() {
  const previous = document.activeElement;
  const scope = previous instanceof Element ? previous.closest('[data-workspace-view]') ?? document : document;
  const composer = scope.querySelector<HTMLTextAreaElement>(
    '[data-whip-composer]',
  );
  return () => {
    if (
      composer?.isConnected &&
      (document.activeElement === previous ||
        document.activeElement === document.body)
    )
      composer.focus();
  };
}

/** Only root operations can ask for human approval. Child effects use delegation.
 * Counts come from session activity; list pages and operation bodies stay bounded. */
export function PendingRequests({
  session,
  rootId,
  disabled,
  refresh,
  pendingCount = '0',
}: {
  session: Session;
  rootId: string;
  disabled: boolean;
  refresh(): Promise<void>;
  pendingCount?: string;
}) {
  const root = useMemo(
    () => session.client.session(rootId),
    [session.client, rootId],
  );
  const query = useQuery({
    queryKey: [
      'pending-requests',
      session.client.runtimeID,
      session.client.processEpoch,
      rootId,
      session.id,
    ],
    queryFn: async ({ signal }) => {
      const [permissions, questions] = await Promise.all([
        session.id === root.id
          ? session.permissions.list({ pending_only: true, limit: 1 }, { signal })
          : Promise.resolve({ items: [] }),
        root.questions.list({ pending_only: true, limit: 4 }, { signal }),
      ]);
      if (
        questions.items.some(
          (question) =>
            question.session_id !== root.id || question.state !== 'pending',
        )
      )
        throw new Error(
          'Question list belongs to another session or is no longer pending',
        );
      if (
        permissions.items?.some((permission) => permission.state !== 'pending')
      )
        throw new Error('Approval list contains closed requests');
      const permission = permissions.items?.[0];
      const operation = permission
        ? await session.operations.get(permission.operation_id, { signal })
        : undefined;
      return { permission, operation, questions: questions.items };
    },
    enabled: !disabled,
    gcTime: 0,
    retry: false,
    refetchInterval: !disabled ? 1500 : false,
  });
  const update = async () => {
    const result = await query.refetch();
    if (result.error) throw result.error;
    await refresh();
  };
  const data = query.data;
  if (!data?.permission && !data?.questions.length && !query.error) return null;
  return (
    <section
      aria-label="Needs your attention"
      {...stylex.props(layout.column, layout.requestDock)}
    >
      <ErrorNotice
        type="resource"
        owner={`${session.id}:requests`}
        error={query.error}
        title="Pending requests unavailable"
      />
      {data?.permission && data.operation?.state === 'waiting' && (
        <PermissionRequest
          key={`${session.client.processEpoch}:${session.id}:${data.permission.operation_id}`}
          permission={data.permission}
          operation={data.operation}
          waiting={
            BigInt(pendingCount) > 0n ? String(BigInt(pendingCount) - 1n) : '0'
          }
          session={session}
          disabled={disabled || !!query.error}
          refresh={update}
        />
      )}
      {data?.questions.map((question) => (
        <QuestionRequest
          key={`${root.id}:${question.operation_id}`}
          question={question}
          session={root}
          disabled={disabled || !!query.error}
          refresh={update}
        />
      ))}
      {data?.questions.length === 4 && (
        <p {...stylex.props(layout.notice)}>
          Showing up to four pending questions. Further questions appear after
          these close.
        </p>
      )}
    </section>
  );
}

export function PermissionRequest({
  permission,
  operation,
  waiting,
  session,
  disabled,
  refresh,
}: {
  permission: DeepReadonly<Permission>;
  operation: DeepReadonly<HostOperation>;
  waiting: string;
  session: Session;
  disabled: boolean;
  refresh(): Promise<void>;
}) {
  const titleId = useId();
  const args = operation.arguments;
  const command = operation.capability.startsWith('shell.') && args &&
    typeof args === 'object' && !Array.isArray(args) && 'command' in args &&
    typeof args.command === 'string' ? args.command : operationSubject(operation);
  const [pending, setPending] = useState(false);
  const [attempt, setAttempt] = useState<boolean>();
  const [error, setError] = useState<unknown>();
  const active = useRef<Session | undefined>(session);
  useEffect(() => {
    active.current = session;
    return () => {
      active.current = undefined;
    };
  }, [session]);
  async function decide(allow: boolean) {
    if (disabled || pending) return;
    const restoreFocus = captureAnswerFocus();
    setPending(true);
    setError(undefined);
    setAttempt(allow);
    try {
      await session.permissions.resolve(permission.operation_id, allow);
      restoreFocus();
      await refresh();
    } catch (error) {
      if (active.current === session) setError(error);
    } finally {
      if (active.current === session) setPending(false);
    }
  }
  return (
    <div {...stylex.props(requestStyles.region)}>
      <section
        aria-labelledby={titleId}
        aria-busy={pending || undefined}
        {...stylex.props(permissionStyles.card)}
      >
        <div {...stylex.props(permissionStyles.header)}>
          <h2 id={titleId} {...stylex.props(permissionStyles.title)}>
            Your approval is needed{' '}
            <ShieldAlert
              size={14}
              aria-hidden
              {...stylex.props(permissionStyles.icon)}
            />
          </h2>
          {waiting !== '0' && (
            <span role="status" {...stylex.props(permissionStyles.waiting)}>
              {waiting} more waiting
            </span>
          )}
        </div>
        <div {...stylex.props(permissionStyles.identity)}>
          <span {...stylex.props(permissionStyles.agent)}>Root agent</span>
          <span aria-hidden>·</span>
          <span {...stylex.props(permissionStyles.operation)}>
            {operation.capability}
          </span>
        </div>
        <pre
          aria-label="Requested operation"
          tabIndex={0}
          {...stylex.props(layout.pre, permissionStyles.command)}
        >
          {command}
        </pre>
        <details>
          <summary>Operation details</summary>
          <p {...stylex.props(permissionStyles.rule)}>This decision applies to this operation only.</p>
          <pre aria-label="Requested resource" {...stylex.props(layout.pre)}>{operation.resource}</pre>
          <pre
            aria-label="Exact operation arguments"
            tabIndex={0}
            {...stylex.props(layout.pre, permissionStyles.command)}
          >
            {JSON.stringify(operation.arguments, null, 2)}
          </pre>
        </details>
        {error !== undefined && (
          <ErrorNotice
            type="action"
            owner={`permission:${permission.operation_id}`}
            error={error}
            title="Approval status needs checking"
            tone="warning"
          />
        )}
        {attempt !== undefined && error !== undefined ? (
          <div {...stylex.props(permissionStyles.footer)}>
            <Button
              disabled={disabled || pending}
              onClick={() => {
                setPending(true);
                void refresh()
                  .catch(setError)
                  .finally(() => setPending(false));
              }}
            >
              Check approval state
            </Button>
            <Button
              disabled={disabled || pending}
              onClick={() => void decide(attempt)}
            >
              Retry same {attempt ? 'approval' : 'denial'}
            </Button>
          </div>
        ) : (
          <div {...stylex.props(permissionStyles.footer)}>
            <Button
              xstyle={permissionStyles.control}
              disabled={disabled || pending || attempt !== undefined}
              onClick={() => void decide(true)}
            >
              Allow once
            </Button>
            <Button
              xstyle={permissionStyles.control}
              disabled={disabled || pending || attempt !== undefined}
              onClick={() => void decide(false)}
            >
              Deny
            </Button>
          </div>
        )}
      </section>
    </div>
  );
}
const requestStyles = stylex.create({
  region: {
    width: '100%',
    maxWidth: 864,
    alignSelf: 'center',
    paddingInline: { default: 24, [scale.phone]: 12 },
    flexShrink: 0,
  },
});
const permissionStyles = stylex.create({
  card: {
    display: 'flex',
    flexDirection: 'column',
    gap: scale.space2,
    padding: scale.space3,
    borderWidth: 1,
    borderStyle: 'solid',
    borderColor: `color-mix(in srgb, ${colors.warning} 30%, ${colors.background})`,
    borderRadius: 20,
    backgroundColor: `color-mix(in srgb, ${colors.warning} 12%, ${colors.background})`,
    color: colors.foreground,
    fontSize: typography.size13,
    lineHeight: 1.5,
  },
  header: {
    display: 'flex',
    flexWrap: 'wrap',
    alignItems: 'center',
    gap: scale.space2,
  },
  title: {
    display: 'flex',
    alignItems: 'center',
    gap: scale.space2,
    margin: 0,
    fontSize: typography.size13,
    fontWeight: 550,
    color: `color-mix(in srgb, ${colors.warning} 70%, ${colors.foreground})`,
  },
  icon: { flexShrink: 0 },
  waiting: {
    marginInlineStart: 'auto',
    color: surface.secondaryText,
    fontSize: typography.size12,
  },
  identity: {
    display: 'flex',
    alignItems: 'baseline',
    gap: scale.space2,
    color: surface.secondaryText,
    fontSize: typography.size12,
    minWidth: 0,
    overflowWrap: 'anywhere',
  },
  agent: {
    fontWeight: 500,
    minWidth: 0,
    overflow: 'hidden',
    textOverflow: 'ellipsis',
    whiteSpace: 'nowrap',
  },
  operation: { flexShrink: 0, maxWidth: '50%' },
  command: { maxHeight: 'min(24dvh, 200px)', paddingBlock: scale.space1 },
  rule: {
    margin: 0,
    overflowWrap: 'anywhere',
    fontSize: typography.size12,
    color: surface.secondaryText,
  },
  footer: {
    display: 'flex',
    alignItems: 'center',
    flexWrap: 'wrap',
    gap: scale.space2,
    paddingTop: scale.space1,
  },
  control: {
    maxWidth: '100%',
    whiteSpace: 'normal',
    textAlign: 'start',
    borderColor: `color-mix(in srgb, ${colors.warning} 30%, ${colors.background})`,
    backgroundColor: {
      default: 'transparent',
      ':hover': `color-mix(in srgb, ${colors.foreground} 5%, transparent)`,
    },
  },
  scope: {
    flexShrink: 1,
    minWidth: 0,
    fontSize: typography.size12,
    fontWeight: 400,
    color: surface.secondaryText,
    borderColor: 'transparent',
  },
  recovery: { alignSelf: 'flex-start' },
});
const questionStyles = stylex.create({
  card: {
    borderWidth: 1,
    borderStyle: 'solid',
    borderColor: surface.quietBorder,
    borderRadius: 20,
    padding: 16,
    backgroundColor: colors.element,
    display: 'flex',
    flexDirection: 'column',
    gap: 12,
  },
  header: {
    display: 'flex',
    alignItems: 'center',
    gap: 8,
    color: surface.secondaryText,
  },
  headerLabel: { fontSize: typography.size13, fontWeight: 500 },
  question: {
    fontSize: `calc(${typography.size13} * 13.5 / 13)`,
    fontWeight: 600,
    lineHeight: 1.4,
  },
  options: {
    display: 'flex',
    flexDirection: 'column',
    gap: 4,
    borderWidth: 0,
    padding: 0,
    margin: 0,
  },
  option: {
    display: 'flex',
    alignItems: 'flex-start',
    gap: 14,
    padding: '8px 10px',
    marginInline: -10,
    borderWidth: 0,
    borderRadius: 12,
    backgroundColor: { default: 'transparent', ':hover': colors.hover },
    color: 'inherit',
    font: 'inherit',
    fontSize: `calc(${typography.size13} * 13.5 / 13)`,
    textAlign: 'start',
    cursor: 'pointer',
  },
  number: {
    display: 'inline-flex',
    alignItems: 'center',
    justifyContent: 'center',
    width: 24,
    height: 24,
    flexShrink: 0,
    borderRadius: '50%',
    borderWidth: 1,
    borderStyle: 'solid',
    borderColor: surface.quietBorder,
    color: surface.secondaryText,
    fontSize: typography.size12,
    marginTop: 1,
  },
  numberSelected: {
    backgroundColor: colors.foreground,
    borderColor: colors.foreground,
    color: colors.background,
  },
  optionText: {
    minWidth: 0,
    display: 'flex',
    flexDirection: 'column',
    gap: 2,
  },
  optionDescription: {
    color: surface.secondaryText,
    fontSize: typography.size12,
    lineHeight: 1.4,
  },
  optionLabel: {
    display: 'flex',
    alignItems: 'center',
    gap: 8,
  },
  recommended: {
    paddingBlock: 1,
    paddingInline: 8,
    borderRadius: 999,
    borderWidth: 1,
    borderStyle: 'solid',
    borderColor: surface.quietBorder,
    color: surface.secondaryText,
    fontSize: typography.size11,
    lineHeight: 1.5,
  },
  footer: { display: 'flex', alignItems: 'center', gap: 14 },
  custom: {
    display: 'flex',
    alignItems: 'center',
    gap: 14,
    flex: 1,
    minWidth: 0,
    color: surface.secondaryText,
  },
  customInput: {
    flex: 1,
    minWidth: 0,
    borderWidth: 0,
    boxShadow: 'none',
    padding: 4,
    fontSize: `calc(${typography.size13} * 13.5 / 13)`,
    backgroundColor: { default: 'transparent', ':hover': 'transparent' },
    outline: { default: 'none', ':focus-visible': 'none' },
  },
  action: { borderRadius: 999 },
});

type QuestionDraft = { selected: string[]; text: string; skipped: boolean };

type QuestionItem = {
  question: string;
  options?: DeepReadonly<
    { label: string; description?: string; recommended?: boolean }[]
  > | null;
  multiple?: boolean;
};

export function QuestionRequest({
  question,
  session,
  disabled,
  refresh,
}: {
  question: DeepReadonly<Question>;
  session: Session;
  disabled: boolean;
  refresh(): Promise<void>;
}) {
  const questions: readonly QuestionItem[] = question.request.questions;
  const [attempt, setAttempt] =
    useState<Operations['questions.answer']['params']['answers']>();
  const [error, setError] = useState<unknown>();
  const active = useRef<Session | undefined>(session);
  useEffect(() => {
    active.current = session;
    return () => {
      active.current = undefined;
    };
  }, [session]);
  const [index, setIndex] = useState(0);
  const [drafts, setDrafts] = useState<QuestionDraft[]>(() =>
    questions.map(() => ({ selected: [], text: '', skipped: false })),
  );
  const [pending, setPending] = useState(false);
  const current = questions[Math.min(index, questions.length - 1)];
  const draft = drafts[Math.min(index, questions.length - 1)];
  const last = index >= questions.length - 1;

  function patchDraft(patch: Partial<QuestionDraft>) {
    setDrafts((previous) =>
      previous.map((item, i) => (i === index ? { ...item, ...patch } : item)),
    );
  }
  function toggle(label: string) {
    patchDraft({
      selected: current.multiple
        ? draft.selected.includes(label)
          ? draft.selected.filter((value) => value !== label)
          : [...draft.selected, label]
        : [label],
      text: current.multiple ? draft.text : '',
      skipped: false,
    });
  }
  function draftAnswer(item: QuestionDraft): string[] {
    return [...item.selected, ...(item.text.trim() ? [item.text.trim()] : [])];
  }
  async function send(
    answers: Operations['questions.answer']['params']['answers'],
  ) {
    if (disabled || pending) return;
    const restoreFocus = captureAnswerFocus();
    setPending(true);
    setError(undefined);
    setAttempt(answers);
    try {
      await session.questions.answer(question.operation_id, answers);
      restoreFocus();
      await refresh();
    } catch (error) {
      if (active.current === session) setError(error);
    } finally {
      if (active.current === session) setPending(false);
    }
  }
  async function submit(nextDrafts = drafts) {
    const answers = nextDrafts.map((item) => ({
      answer: item.skipped ? [] : draftAnswer(item),
      dismissed: item.skipped,
    }));
    const first = answers[0];
    if (first) await send([first, ...answers.slice(1)]);
  }
  async function dismissAll() {
    await send([
      { answer: [], dismissed: true },
      ...questions.slice(1).map(() => ({ answer: [], dismissed: true })),
    ]);
  }

  const hasInput = !!draft.selected.length || !!draft.text.trim();
  const canAdvance =
    !disabled && !pending && !attempt && (hasInput || draft.skipped);
  return (
    <div {...stylex.props(requestStyles.region)}>
      <div {...stylex.props(questionStyles.card)}>
        <div {...stylex.props(questionStyles.header)}>
          <CircleHelp size={14} />
          <span {...stylex.props(questionStyles.headerLabel)}>
            {questions.length > 1
              ? `Question ${index + 1} of ${questions.length}`
              : 'Question'}
          </span>
          <span {...stylex.props(layout.grow)} />
          <IconButton
            label="Dismiss question"
            variant="ghost"
            disabled={disabled || pending || !!attempt}
            onClick={() => void dismissAll()}
          >
            <X size={14} />
          </IconButton>
        </div>
        {error !== undefined && (
          <ErrorNotice
            type="action"
            owner={`question:${question.operation_id}`}
            error={error}
            title="Could not submit response"
          />
        )}
        {attempt && error !== undefined && (
          <div {...stylex.props(layout.row, layout.wrap)}>
            <Button
              disabled={disabled || pending}
              onClick={() => {
                setPending(true);
                void session.questions
                  .get(question.operation_id)
                  .then(refresh)
                  .catch(setError)
                  .finally(() => setPending(false));
              }}
            >
              Check answer state
            </Button>
            <Button
              disabled={disabled || pending}
              onClick={() => void send(attempt)}
            >
              Retry same response
            </Button>
            <p>
              The exact response is retained until its delivery is resolved.
              Checking does not send it again.
            </p>
          </div>
        )}
        <div {...stylex.props(questionStyles.question)}>{current.question}</div>
        {!!current.options?.length && (
          <div
            role={current.multiple ? 'group' : 'radiogroup'}
            aria-label={current.question || 'Answer choices'}
            {...stylex.props(questionStyles.options)}
          >
            {current.options.map((option, optionIndex) => {
              const active = draft.selected.includes(option.label);
              const descriptionId = option.description
                ? `${question.operation_id}-option-${optionIndex}-description`
                : undefined;
              return (
                <button
                  key={option.label}
                  type="button"
                  role={current.multiple ? 'checkbox' : 'radio'}
                  aria-checked={active}
                  aria-describedby={descriptionId}
                  disabled={disabled || pending || !!attempt}
                  {...stylex.props(questionStyles.option)}
                  onClick={() => toggle(option.label)}
                >
                  <span
                    {...stylex.props(
                      questionStyles.number,
                      active && questionStyles.numberSelected,
                    )}
                  >
                    {optionIndex + 1}
                  </span>
                  <span {...stylex.props(questionStyles.optionText)}>
                    <span {...stylex.props(questionStyles.optionLabel)}>
                      {option.label}
                      {option.recommended && (
                        <span {...stylex.props(questionStyles.recommended)}>
                          Recommended
                        </span>
                      )}
                    </span>
                    {option.description && (
                      <span
                        id={descriptionId}
                        {...stylex.props(questionStyles.optionDescription)}
                      >
                        {option.description}
                      </span>
                    )}
                  </span>
                </button>
              );
            })}
          </div>
        )}
        <div {...stylex.props(questionStyles.footer)}>
          {index > 0 ? (
            <Button
              variant="ghost"
              xstyle={questionStyles.action}
              disabled={disabled || pending || !!attempt}
              onClick={() => setIndex(index - 1)}
            >
              Back
            </Button>
          ) : (
            <span />
          )}
          <label {...stylex.props(questionStyles.custom)}>
            <PenLine size={14} />
            <Input
              aria-label="Write your own response"
              xstyle={questionStyles.customInput}
              maxLength={8192}
              value={draft.text}
              onChange={(event) =>
                patchDraft({
                  text: event.target.value,
                  selected: current.multiple ? draft.selected : [],
                  skipped: false,
                })
              }
              placeholder="Or write your own response"
              disabled={disabled || pending || !!attempt}
              onKeyDown={(event) => {
                if (event.key === 'Enter' && canAdvance) {
                  event.preventDefault();
                  if (last) void submit();
                  else setIndex(index + 1);
                }
              }}
            />
          </label>
          <Button
            variant="ghost"
            xstyle={questionStyles.action}
            disabled={disabled || pending || !!attempt}
            onClick={() => {
              if (questions.length === 1) void dismissAll();
              else {
                const skipped = { selected: [], text: '', skipped: true };
                patchDraft(skipped);
                if (last)
                  void submit(
                    drafts.map((item, i) => (i === index ? skipped : item)),
                  );
                else setIndex(index + 1);
              }
            }}
          >
            Skip
          </Button>
          <Button
            variant="primary"
            xstyle={questionStyles.action}
            disabled={!canAdvance}
            loading={pending}
            onClick={() => {
              if (last) void submit();
              else setIndex(index + 1);
            }}
          >
            {last ? 'Send' : 'Next'}
          </Button>
        </div>
      </div>
    </div>
  );
}
