import { useState } from 'react';
import type { Session } from '@whip/sdk';
import type { DeepReadonly } from '@whip/sdk/state';
import type { LifecycleEvent, RootSnapshot } from '@whip/protocol';
import { Badge, Button, Checkbox, Field, Input, RadioGroup, Select } from '@whip/ui';
import * as stylex from '@stylexjs/stylex';
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
  return (
    <div {...stylex.props(layout.notice, layout.column)}>
      <Badge tone="info">Question</Badge>
      <strong>{question.question}</strong>
      {question.multiple ? question.options?.map((option) => (
        <Checkbox
          key={option.label}
          label={option.label}
          description={option.description}
          checked={selected.includes(option.label)}
          onCheckedChange={(checked) =>
            setSelected((previous) =>
              checked
                ? [...previous, option.label]
                : previous.filter((value) => value !== option.label),
            )
          }
        />
      )) : !!question.options?.length && (
        <RadioGroup
          label={question.question || 'Choose one answer'}
          options={question.options.map(option => ({value: option.label, label: option.label, description: option.description}))}
          value={selected[0] ?? ''}
          onValueChange={value => setSelected([value])}
        />
      )}
      <Field label="Your answer">
        <Input
          value={text}
          onChange={(event) => setText(event.target.value)}
          placeholder="Add a response…"
        />
      </Field>
      <div {...stylex.props(layout.row)}>
        <Button
          disabled={disabled || pending || (!selected.length && !text.trim())}
          onClick={() => void answer()}
        >
          Respond
        </Button>
        <Button
          variant="ghost"
          disabled={disabled || pending}
          onClick={() => void answer(true)}
        >
          Dismiss
        </Button>
      </div>
    </div>
  );
}
