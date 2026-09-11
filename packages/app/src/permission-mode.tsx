import { typography } from '@whip/ui/tokens.stylex';
import { useEffect, useRef, useState } from 'react';
import { ErrorNotice } from './error-feedback';
import { Button, Popover } from '@whip/ui';
import { Check, ChevronDown, Hand, ShieldAlert, ShieldCheck } from 'lucide-react';
import * as stylex from '@stylexjs/stylex';
import { colors, surface } from '@whip/ui/tokens.stylex';
import { useRuntime } from './context';
import { layout } from './styles';
import type { InspectorProps } from './details/shared';

type Props = Pick<InspectorProps, 'view' | 'root' | 'connected' | 'agentId'>;

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
    description: 'Access files outside this project and approve actions automatically',
    icon: ShieldAlert,
    danger: true,
  },
];

const styles = stylex.create({
  popup: { width: 'min(340px, calc(100vw - 48px))' },
  trigger: { gap: 6, maxWidth: 200 },
  chevron: { color: surface.secondaryText, flexShrink: 0 },
  danger: { color: colors.warning },
  option: {
    display: 'flex',
    alignItems: 'flex-start',
    gap: 10,
    width: '100%',
    paddingBlock: 8,
    paddingInline: 8,
    borderRadius: 6,
    borderWidth: 0,
    backgroundColor: { default: 'transparent', ':hover': colors.hover },
    color: colors.foreground,
    font: 'inherit',
    fontSize: typography.size13,
    textAlign: 'start',
    cursor: 'default',
  },
  optionIcon: { marginTop: 1, flexShrink: 0, color: surface.secondaryText },
  optionActive: { backgroundColor: colors.hover },
  optionLabel: { display: 'flex', flexDirection: 'column', gap: 2 },
  optionDescription: { color: surface.secondaryText, fontSize: typography.size12 },
  check: { color: surface.secondaryText, flexShrink: 0, marginTop: 2 },
});

/** Composer control: consent mode for the session. Root-only, applies when idle. */
export function PermissionModePicker({ view, root, connected, agentId }: Props) {
  const runtime = useRuntime();
  if (agentId !== view.session.rootId) return null;
  return <PermissionModeControl key={`${view.session.client?.getSnapshot().info?.runtime_id}:${view.session.rootId}`} value={root.permission_mode || ''} disabled={!connected || !!Object.keys(root.active_turns ?? {}).length}
    onChange={mode => runtime.run(view.session.setPermissionMode(mode === 'prompt'), mode === 'automatic' ? 'Enable Full Access' : 'Require approval prompts')} />;
}

/** Shared session/new-session control; the caller owns persistence. */
export function PermissionModeControl({ value: current, disabled, onChange }: { value: string; disabled?: boolean; onChange(mode: string): void | Promise<unknown> }) {
  const [open, setOpen] = useState(false);
  const [error, setError] = useState<unknown>();
  const [pending, setPending] = useState(false);
  const generation = useRef(0);
  useEffect(() => () => { generation.current++; }, []);
  const active = modes.find(mode => mode.value === current);
  const TriggerIcon = active?.danger ? ShieldAlert : ShieldCheck;
  const pick = async (mode: Mode) => {
    if (disabled || pending) return;
    if (mode.value === current) { setOpen(false); return; }
    const id = ++generation.current;
    setError(undefined); setPending(true);
    try { await onChange(mode.value); if (id === generation.current) setOpen(false); }
    catch (error) { if (id === generation.current) setError(error); }
    finally { if (id === generation.current) setPending(false); }
  };
  return (
    <Popover
      open={open}
      onOpenChange={value => { if (!pending) setOpen(value); }}
      title="How should permissions be approved?"
      xstyle={styles.popup}
      trigger={
        <Button
          variant="ghost"
          aria-label="Permission approval mode"
          disabled={disabled || pending}
          title={active ? `Permissions: ${active.label}` : 'Permission approval mode'}
          xstyle={[styles.trigger, active?.danger && styles.danger]}
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
        <div {...stylex.props(layout.column)} role="listbox" aria-label="Permission approval mode" aria-activedescendant={current}>
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
