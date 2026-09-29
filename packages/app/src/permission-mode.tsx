import { typography } from '@whip/ui/tokens.stylex';
import { useEffect, useRef, useState } from 'react';
import { ErrorNotice } from './error-feedback';
import { Button, Popover } from '@whip/ui';
import { Check, ChevronDown, Hand, ShieldAlert, ShieldCheck } from 'lucide-react';
import * as stylex from '@stylexjs/stylex';
import { colors, scale, surface } from '@whip/ui/tokens.stylex';
import { useAppState, useRuntime } from './context';
import { useQuery } from '@tanstack/react-query';
import type { Session, SessionRecord } from '@whip/sdk';
import type { DeepReadonly } from '@whip/sdk/state';
import { layout } from './styles';
type Props = { session: Session; rootId: string; selected: DeepReadonly<SessionRecord>; connected: boolean };

type Mode = {
  value: string;
  label: string;
  description: string;
  icon: typeof Hand;
  danger?: boolean;
};

const modes: Mode[] = [
  {
    value: 'prompt',
    label: 'Ask for approval',
    description: 'Permission prompts go to connected clients',
    icon: Hand,
  },
  {
    value: 'automatic',
    label: 'Full Access',
    description: 'Approve eligible actions automatically, including default children in the same working directory. Explicit child restrictions still apply.',
    icon: ShieldAlert,
    danger: true,
  },
];

const styles = stylex.create({
  popup: { width: 'min(340px, calc(100vw - 48px))' },
  title: { display: 'block', paddingBlock: scale.space1, paddingInline: scale.space2 },
  options: { display: 'flex', flexDirection: 'column', gap: scale.space1, minWidth: 0 },
  trigger: { gap: 6, maxWidth: 200 },
  settingsTrigger: { width: { default: 220, [scale.phone]: '100%' }, maxWidth: '100%', minWidth: 0, justifyContent: 'space-between' },
  chevron: { color: surface.secondaryText, flexShrink: 0 },
  danger: { color: colors.warning },
  option: {
    display: 'grid',
    gridTemplateColumns: '16px minmax(0, 1fr) 16px',
    alignItems: 'start',
    gap: scale.space2,
    width: '100%',
    paddingBlock: scale.space2,
    paddingInline: scale.space2,
    borderRadius: scale.radiusControl,
    borderWidth: 0,
    backgroundColor: { default: 'transparent', ':hover': colors.hover },
    color: colors.foreground,
    font: 'inherit',
    fontSize: typography.size13,
    lineHeight: '1.5385',
    textAlign: 'start',
    cursor: 'default',
  },
  optionIcon: { marginTop: 2, color: surface.secondaryText },
  optionActive: { backgroundColor: colors.hover },
  optionLabel: { display: 'flex', flexDirection: 'column', gap: scale.space1 },
  optionDescription: { color: surface.secondaryText, fontSize: typography.size12, lineHeight: '1.5' },
  check: { color: surface.secondaryText, alignSelf: 'start', marginTop: 3 },
});

/** The root owns the shared policy; mode changes retain the observed revision. */
export function PermissionModePicker(props: Props) {
  if (props.session.id !== props.rootId || props.selected.id !== props.session.id || props.selected.parent_id !== null) return null;
  return <RootPermissionPicker key={`${props.session.client.runtimeID}:${props.session.client.processEpoch}:${props.session.id}`} {...props} />;
}
function RootPermissionPicker({ session, selected, connected }: Props) {
  const runtime = useRuntime();
  const { commands } = useAppState();
  const owner = `${session.client.runtimeID}:${session.id}:permission-mode`;
  const unresolved = commands.find(command => command.draftKey === owner && command.delivery);
  const [base, setBase] = useState<string>();
  const policy = useQuery({
    queryKey: ['permission-mode', session.client.runtimeID, session.client.processEpoch, session.id],
    queryFn: async ({ signal }) => {
      const result = await session.permissions.policy({ signal });
      if (result.tree_id !== selected.tree_id) throw new Error('Permission policy belongs to another tree');
      return result;
    },
    enabled: connected, gcTime: 0, retry: false, refetchInterval: connected ? 3000 : false,
  });
  return <>
    <PermissionModeControl value={policy.data?.mode ?? ''} disabled={!connected || !policy.data || !!policy.error || !!unresolved}
      onOpenChange={open => setBase(open ? policy.data?.revision : undefined)}
      onChange={async mode => {
        if (!policy.data || !base) throw new Error('Reload the permission policy before changing it');
        await runtime.run(runtime.command(session.client, 'permissions.set_mode', {
          session_id: session.id, edit_id: crypto.randomUUID(), expected_revision: base,
          mode: mode === 'automatic' ? 'automatic' : 'prompt',
        }), mode === 'automatic' ? 'Enable Full Access' : 'Require approval prompts', undefined, owner);
        await policy.refetch();
      }} />
    {policy.error && <ErrorNotice type="resource" owner={owner} error={policy.error}
      action={<Button variant="ghost" onClick={() => void policy.refetch()}>Reload policy</Button>} />}
    {base && policy.data && base !== policy.data.revision && <span role="status">Policy changed. Close and reopen this control to use the current policy.</span>}
    {unresolved && <span role="status">Resolve the pending permission change in command recovery before changing it again.</span>}
  </>;
}

/** Shared session, new-session and settings control; the caller owns persistence. */
export function PermissionModeControl({ value: current, disabled, onChange, presentation = 'composer', label = 'Permission approval mode', inherited = false, onOpenChange }: {
  value: string; disabled?: boolean; onChange(mode: string): void | Promise<unknown>;
  presentation?: 'composer' | 'settings'; label?: string; inherited?: boolean; onOpenChange?(open: boolean): void;
}) {
  const [open, setOpen] = useState(false);
  const [error, setError] = useState<unknown>();
  const [pending, setPending] = useState(false);
  const generation = useRef(0);
  useEffect(() => () => { generation.current++; }, []);
  const active = modes.find(mode => mode.value === current);
  const TriggerIcon = active?.danger ? ShieldAlert : ShieldCheck;
  const pick = async (mode: Mode) => {
    if (disabled || pending) return;
    // Choosing the displayed host default can still pin an explicit draft override.
    if (mode.value === current && !inherited) { setOpen(false); return; }
    const id = ++generation.current;
    setError(undefined); setPending(true);
    try { await onChange(mode.value); if (id === generation.current) setOpen(false); }
    catch (error) { if (id === generation.current) setError(error); }
    finally { if (id === generation.current) setPending(false); }
  };
  return (
    <Popover
      open={open}
      onOpenChange={value => { if (!pending) { setOpen(value); onOpenChange?.(value); } }}
      title={<span {...stylex.props(styles.title)}>How should permissions be approved?</span>}
      xstyle={styles.popup}
      trigger={
        <Button
          variant={presentation === 'settings' ? 'secondary' : 'ghost'}
          aria-label={label}
          disabled={disabled || pending}
          title={active ? `Permissions: ${active.label}` : 'Permission approval mode'}
          xstyle={[styles.trigger, presentation === 'settings' && styles.settingsTrigger, active?.danger && styles.danger]}
        >
          <TriggerIcon size={14} {...stylex.props(active?.danger ? undefined : styles.chevron)} />
          <span {...stylex.props(layout.ellipsis)}>
            {active ? active.label : 'Permissions unavailable'}
          </span>
          <ChevronDown size={14} {...stylex.props(styles.chevron)} />
        </Button>
      }
    >
      {error !== undefined && <ErrorNotice type="action" owner="permission-mode" error={error} title="Could not change permission mode" />}
      {open && (
        <div {...stylex.props(styles.options)} role="listbox" aria-label="Permission approval mode" aria-activedescendant={current}>
          {modes.map((mode) => (
            <button
              key={mode.value}
              id={mode.value}
              role="option"
              aria-selected={mode.value === current}
              {...stylex.props(styles.option, mode.value === current && styles.optionActive)}
              disabled={disabled || pending}
              onClick={() => void pick(mode)}
            >
              <mode.icon size={16} {...stylex.props(styles.optionIcon, mode.danger && styles.danger)} />
              <span {...stylex.props(layout.grow, styles.optionLabel)}>
                <span>{mode.label}</span>
                <span {...stylex.props(styles.optionDescription)}>{mode.description}</span>
              </span>
              {mode.value === current && <Check size={14} {...stylex.props(styles.check)} />}
            </button>
          ))}
        </div>
      )}
    </Popover>
  );
}
