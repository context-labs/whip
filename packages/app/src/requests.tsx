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
  const [selected, setSelected] = useState<string[]>([]);
  const [text, setText] = useState('');
  const [pending, setPending] = useState(false);
  async function answer(dismissed = false) {
    if (!question.question_id) return;
    const restoreFocus = captureAnswerFocus();
    setPending(true);
    try {
      await runtime.run(
        session.answerQuestion(
          question.question_id,
          [...selected, ...(text.trim() ? [text.trim()] : [])],
          dismissed,
        ),
        dismissed ? 'Dismiss question' : 'Answer question',
      );
      restoreFocus();
    } catch {
    } finally {
      setPending(false);
    }
  }
  function toggle(label: string) {
    setSelected((previous) =>
      question.multiple
        ? previous.includes(label)
          ? previous.filter((value) => value !== label)
          : [...previous, label]
        : [label],
    );
  }
  const canSend = !disabled && !pending && (!!selected.length || !!text.trim());
  return (
    <div {...stylex.props(questionStyles.region)}>
      <div {...stylex.props(questionStyles.card)}>
        <div {...stylex.props(questionStyles.header)}>
          <CircleHelp size={14} />
          <span {...stylex.props(questionStyles.headerLabel)}>Question</span>
          <span {...stylex.props(layout.grow)} />
          <IconButton
            label="Dismiss question"
            variant="ghost"
            disabled={disabled || pending}
            onClick={() => void answer(true)}
          >
            <X size={14} />
          </IconButton>
        </div>
        <div {...stylex.props(questionStyles.question)}>{question.question}</div>
        {!!question.options?.length && (
          <div
            role={question.multiple ? 'group' : 'radiogroup'}
            aria-label={question.question || 'Answer choices'}
            {...stylex.props(questionStyles.options)}
          >
            {question.options.map((option, index) => {
              const active = selected.includes(option.label);
              const descriptionId = option.description ? `${question.question_id}-option-${index}-description` : undefined;
              return (
                <button
                  key={option.label}
                  type="button"
                  role={question.multiple ? 'checkbox' : 'radio'}
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
                    {index + 1}
                  </span>
                  <span {...stylex.props(questionStyles.optionText)}>
                    <span>{option.label}</span>
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
          <label {...stylex.props(questionStyles.custom)}>
            <PenLine size={14} />
            <Input
              aria-label="Write your own response"
              xstyle={questionStyles.customInput}
              value={text}
              onChange={(event) => setText(event.target.value)}
              placeholder="Or write your own response"
              disabled={disabled || pending}
              onKeyDown={(event) => {
                if (event.key === 'Enter' && canSend) {
                  event.preventDefault();
                  void answer();
                }
              }}
            />
          </label>
          <Button
            variant="ghost"
            xstyle={questionStyles.action}
            disabled={disabled || pending}
            onClick={() => void answer(true)}
          >
            Skip
          </Button>
          <Button
            variant="primary"
            xstyle={questionStyles.action}
            disabled={!canSend}
            loading={pending}
            onClick={() => void answer()}
          >
            Send
          </Button>
        </div>
      </div>
    </div>
  );
}
