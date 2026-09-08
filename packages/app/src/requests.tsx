import { useState } from 'react';
import type { Session } from '@whip/sdk';
import type { DeepReadonly } from '@whip/sdk/state';
import type { LifecycleEvent, RootSnapshot } from '@whip/protocol';
import { Badge, Button, IconButton, Input, Select } from '@whip/ui';
import * as stylex from '@stylexjs/stylex';
import { CircleHelp, PenLine, X } from 'lucide-react';
import { colors, surface, scale } from '@whip/ui/tokens.stylex';
import { useRuntime } from './context';
import { layout } from './styles';

function captureAnswerFocus() {
  const previous = document.activeElement;
  const composer = document.querySelector<HTMLTextAreaElement>('[data-whip-composer]');
  return () => {
    if (composer?.isConnected && (document.activeElement === previous || document.activeElement === document.body)) composer.focus();
  };
}

export function PendingRequests({
  root,
  session,
  disabled,
  refresh,
}: {
  root: DeepReadonly<RootSnapshot>;
  session: Session;
  disabled: boolean;
  refresh(): Promise<void>;
}) {
  const permissions =
    root.permissions?.filter((item) => item.status === 'pending') ?? [];
  return (
    <section
      aria-label="Needs your attention"
      {...stylex.props(layout.column, layout.requestDock)}
    >
      {permissions.map((permission) => (
        <PermissionRequest
          key={permission.id}
          permission={permission}
          session={session}
          disabled={disabled}
          refresh={refresh}
        />
      ))}
      {root.questions
        ?.filter((question) => question.question_id)
        .map((question) => (
          <QuestionRequest
            key={question.question_id}
            question={question}
            session={session}
            disabled={disabled}
          />
        ))}
    </section>
  );
}
function PermissionRequest({
  permission,
  session,
  disabled,
  refresh,
}: {
  permission: NonNullable<DeepReadonly<RootSnapshot['permissions']>>[number];
  session: Session;
  disabled: boolean;
  refresh(): Promise<void>;
}) {
  const runtime = useRuntime();
  const [pending, setPending] = useState(false);
  const [remember, setRemember] = useState('');
  const [uncertain, setUncertain] = useState(false);
  async function decide(allow: boolean) {
    const restoreFocus = captureAnswerFocus();
    setPending(true);
    try {
      await session.client.permissions.decide({
        root_id: session.rootId,
        permission_id: permission.id,
        allow,
        ...(allow && remember ? { remember } : {}),
      });
      restoreFocus();
      await refresh();
    } catch (error) {
      runtime.report(error);
      setUncertain(true);
    } finally {
      setPending(false);
    }
  }
  return (
    <div {...stylex.props(layout.notice, layout.column)}>
      <div {...stylex.props(layout.row)}>
        <Badge tone="warning">Permission requested</Badge>
        <span>{permission.operation}</span>
      </div>
      <pre {...stylex.props(layout.pre)}>
        {permission.command || permission.canonical_path}
      </pre>
      <span {...stylex.props(layout.muted)}>
        Agent{' '}
        {permission.agent_id === session.rootId ? 'root' : permission.agent_id}
      </span>
      {permission.rule && (
        <>
          <Select
            label="Permission scope"
            placeholder="This request only"
            value={remember}
            onValueChange={setRemember}
            options={[
              { value: '', label: 'This request only' },
              {
                value: 'tree',
                label: 'Remember for this session and children',
              },
              { value: 'global', label: 'Remember on this host' },
            ]}
          />
          {remember && <p>Rule: {permission.rule}</p>}
        </>
      )}
      {uncertain ? (
        <Button
          disabled={disabled || pending}
          onClick={() => {
            setPending(true);
            void refresh()
              .then(() => setUncertain(false))
              .catch((error) => runtime.report(error))
              .finally(() => setPending(false));
          }}
        >
          Refresh permission state before retrying
        </Button>
      ) : (
        <div {...stylex.props(layout.row)}>
          <Button
            disabled={disabled || pending}
            onClick={() => void decide(true)}
          >
            {remember ? 'Allow and remember' : 'Allow once'}
          </Button>
          <Button
            variant="secondary"
            disabled={disabled || pending}
            onClick={() => void decide(false)}
          >
            Deny
          </Button>
        </div>
      )}
    </div>
  );
}
const questionStyles = stylex.create({
  region: {
    width: '100%',
    maxWidth: 864,
    alignSelf: 'center',
    paddingInline: { default: 24, [scale.phone]: 12 },
    flexShrink: 0,
  },
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
  headerLabel: { fontSize: 13, fontWeight: 500 },
  question: { fontSize: 13.5, fontWeight: 600, lineHeight: 1.4 },
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
    fontSize: 13.5,
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
    fontSize: 12,
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
    fontSize: 12,
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
    fontSize: 11,
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
    fontSize: 13.5,
    backgroundColor: { default: 'transparent', ':hover': 'transparent' },
    outline: { default: 'none', ':focus-visible': 'none' },
  },
  action: { borderRadius: 999 },
});

type QuestionDraft = { selected: string[]; text: string; skipped: boolean };

type QuestionItem = {
  question: string;
  options?: DeepReadonly<{ label: string; description?: string; recommended?: boolean }[]> | null;
  multiple?: boolean;
};

function QuestionRequest({
  question,
  session,
  disabled,
}: {
  question: DeepReadonly<LifecycleEvent>;
  session: Session;
  disabled: boolean;
}) {
  const runtime = useRuntime();
  const questions: readonly QuestionItem[] =
    question.questions?.length
      ? question.questions
      : [{ question: question.question ?? '', options: question.options, multiple: question.multiple }];
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
  async function submit(nextDrafts = drafts) {
    if (!question.question_id) return;
    const restoreFocus = captureAnswerFocus();
    setPending(true);
    try {
      await runtime.run(
        questions.length > 1
          ? session.answerQuestions(
              question.question_id,
              nextDrafts.map((item) =>
                item.skipped ? null : { answer: draftAnswer(item) },
              ),
            )
          : session.answerQuestion(question.question_id, draftAnswer(nextDrafts[0]), nextDrafts[0].skipped),
        'Answer question',
      );
      restoreFocus();
    } catch {
    } finally {
      setPending(false);
    }
  }
  async function dismissAll() {
    if (!question.question_id) return;
    const restoreFocus = captureAnswerFocus();
    setPending(true);
    try {
      await runtime.run(
        questions.length > 1
          ? session.answerQuestions(
              question.question_id,
              questions.map(() => ({ answer: [], dismissed: true })),
            )
          : session.answerQuestion(question.question_id, [], true),
        'Dismiss question',
      );
      restoreFocus();
    } catch {
    } finally {
      setPending(false);
    }
  }

  const hasInput = !!draft.selected.length || !!draft.text.trim();
  const canAdvance = !disabled && !pending && (hasInput || draft.skipped);
  return (
    <div {...stylex.props(questionStyles.region)}>
      <div {...stylex.props(questionStyles.card)}>
        <div {...stylex.props(questionStyles.header)}>
          <CircleHelp size={14} />
          <span {...stylex.props(questionStyles.headerLabel)}>
            {questions.length > 1 ? `Question ${index + 1} of ${questions.length}` : 'Question'}
          </span>
          <span {...stylex.props(layout.grow)} />
          <IconButton
            label="Dismiss question"
            variant="ghost"
            disabled={disabled || pending}
            onClick={() => void dismissAll()}
          >
            <X size={14} />
          </IconButton>
        </div>
        <div {...stylex.props(questionStyles.question)}>{current.question}</div>
        {!!current.options?.length && (
          <div
            role={current.multiple ? 'group' : 'radiogroup'}
            aria-label={current.question || 'Answer choices'}
            {...stylex.props(questionStyles.options)}
          >
            {current.options.map((option, optionIndex) => {
              const active = draft.selected.includes(option.label);
              const descriptionId = option.description ? `${question.question_id}-option-${optionIndex}-description` : undefined;
              return (
                <button
                  key={option.label}
                  type="button"
                  role={current.multiple ? 'checkbox' : 'radio'}
                  aria-checked={active}
                  aria-describedby={descriptionId}
                  disabled={disabled || pending}
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
                        <span {...stylex.props(questionStyles.recommended)}>Recommended</span>
                      )}
                    </span>
                    {option.description && (
                      <span id={descriptionId} {...stylex.props(questionStyles.optionDescription)}>
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
              disabled={disabled || pending}
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
              value={draft.text}
              onChange={(event) => patchDraft({ text: event.target.value, selected: current.multiple ? draft.selected : [], skipped: false })}
              placeholder="Or write your own response"
              disabled={disabled || pending}
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
            disabled={disabled || pending}
            onClick={() => {
              if (questions.length === 1) void dismissAll();
              else {
                const skipped = { selected: [], text: '', skipped: true };
                patchDraft(skipped);
                if (last) void submit(drafts.map((item, i) => i === index ? skipped : item));
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
