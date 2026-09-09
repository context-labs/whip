import { typography } from '@whip/ui/tokens.stylex';
import { useId, useState } from 'react';
import type { Session } from '@whip/sdk';
import type { DeepReadonly } from '@whip/sdk/state';
import type { LifecycleEvent, RootSnapshot } from '@whip/protocol';
import { Button, IconButton, Input, Select } from '@whip/ui';
import * as stylex from '@stylexjs/stylex';
import { CircleHelp, PenLine, ShieldAlert, X } from 'lucide-react';
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
  const permission = permissions[0];
  return (
    <section
      aria-label="Needs your attention"
      {...stylex.props(layout.column, layout.requestDock)}
    >
      {permission && (
        <PermissionRequest
          key={`${session.rootId}:${permission.id}`}
          permission={permission}
          agentName={root.agents?.find(agent => agent.id === permission.agent_id)?.name?.trim()
            || (permission.agent_id === session.rootId ? 'Root agent' : 'Unnamed agent')}
          waiting={permissions.length - 1}
          hasMore={!!root.omitted?.permissions}
          session={session}
          disabled={disabled}
          refresh={refresh}
        />
      )}
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
  agentName,
  waiting,
  hasMore,
  session,
  disabled,
  refresh,
}: {
  permission: NonNullable<DeepReadonly<RootSnapshot['permissions']>>[number];
  agentName: string;
  waiting: number;
  hasMore: boolean;
  session: Session;
  disabled: boolean;
  refresh(): Promise<void>;
}) {
  const runtime = useRuntime();
  const titleId = useId();
  const [pending, setPending] = useState(false);
  const [remember, setRemember] = useState('');
  const [uncertain, setUncertain] = useState(false);
  async function decide(allow: boolean) {
    if (disabled || pending) return;
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
    <div {...stylex.props(requestStyles.region)}>
      <section aria-labelledby={titleId} aria-busy={pending || undefined} {...stylex.props(permissionStyles.card)}>
        <div {...stylex.props(permissionStyles.header)}>
          <h2 id={titleId} {...stylex.props(permissionStyles.title)}>
            Your approval is needed
            <ShieldAlert size={14} aria-hidden {...stylex.props(permissionStyles.icon)} />
          </h2>
          {(waiting > 0 || hasMore) && <span role="status" {...stylex.props(permissionStyles.waiting)}>
            {waiting > 0 ? `${hasMore ? 'At least ' : ''}${waiting} more waiting` : 'More approvals pending'}
          </span>}
        </div>
        <div {...stylex.props(permissionStyles.identity)}>
          <span title={agentName} {...stylex.props(permissionStyles.agent)}>{agentName}</span>
          <span aria-hidden>·</span>
          <span {...stylex.props(permissionStyles.operation)}>{permission.operation}</span>
        </div>
        <pre aria-label="Requested operation" tabIndex={0} {...stylex.props(layout.pre, permissionStyles.command)}>
          {permission.command || permission.canonical_path || permission.operation}
        </pre>
        {permission.rule && remember && <p {...stylex.props(permissionStyles.rule)}>Rule: {permission.rule}</p>}
        {uncertain ? (
          <Button
            xstyle={[permissionStyles.control, permissionStyles.recovery]}
            disabled={disabled || pending}
            loading={pending}
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
          <div {...stylex.props(permissionStyles.footer)}>
            <div {...stylex.props(layout.row, layout.wrap)}>
              <Button
                xstyle={permissionStyles.control}
                disabled={disabled || pending}
                onClick={() => void decide(true)}
              >
                {remember ? 'Allow and remember' : 'Allow once'}
              </Button>
              <Button
                xstyle={permissionStyles.control}
                disabled={disabled || pending}
                onClick={() => void decide(false)}
              >
                Deny
              </Button>
            </div>
            {permission.rule && <Select
              label="Permission scope"
              placeholder="This request only"
              value={remember}
              onValueChange={setRemember}
              disabled={disabled || pending}
              xstyle={[permissionStyles.control, permissionStyles.scope]}
              options={[
                { value: '', label: 'This request only' },
                { value: 'tree', label: 'Remember for this session and children' },
                { value: 'global', label: 'Remember on this host' },
              ]}
            />}
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
  header: { display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: scale.space2 },
  title: {
    display: 'flex', alignItems: 'center', gap: scale.space2,
    margin: 0, fontSize: typography.size13, fontWeight: 550,
    color: `color-mix(in srgb, ${colors.warning} 70%, ${colors.foreground})`,
  },
  icon: { flexShrink: 0 },
  waiting: { marginInlineStart: 'auto', color: surface.secondaryText, fontSize: typography.size12 },
  identity: { display: 'flex', alignItems: 'baseline', gap: scale.space2, color: surface.secondaryText, fontSize: typography.size12, minWidth: 0, overflowWrap: 'anywhere' },
  agent: { fontWeight: 500, minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' },
  operation: { flexShrink: 0, maxWidth: '50%' },
  command: { maxHeight: 'min(24dvh, 200px)', paddingBlock: scale.space1 },
  rule: { margin: 0, overflowWrap: 'anywhere', fontSize: typography.size12, color: surface.secondaryText },
  footer: { display: 'flex', alignItems: 'center', flexWrap: 'wrap', gap: scale.space2, paddingTop: scale.space1 },
  control: {
    maxWidth: '100%', whiteSpace: 'normal', textAlign: 'start',
    borderColor: `color-mix(in srgb, ${colors.warning} 30%, ${colors.background})`,
    backgroundColor: { default: 'transparent', ':hover': `color-mix(in srgb, ${colors.foreground} 5%, transparent)` },
  },
  scope: {
    flexShrink: 1, minWidth: 0, fontSize: typography.size12, fontWeight: 400,
    color: surface.secondaryText, borderColor: 'transparent',
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
  question: { fontSize: `calc(${typography.size13} * 13.5 / 13)`, fontWeight: 600, lineHeight: 1.4 },
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
        question.questions?.length
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
        question.questions?.length
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
    <div {...stylex.props(requestStyles.region)}>
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
