import { useState } from 'react';
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
    description: 'Every permission prompt is approved without asking',
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
    fontSize: 13,
    textAlign: 'start',
    cursor: 'default',
  },
  optionIcon: { marginTop: 1, flexShrink: 0, color: surface.secondaryText },
  optionActive: { backgroundColor: colors.hover },
  optionLabel: { display: 'flex', flexDirection: 'column', gap: 2 },
  optionDescription: { color: surface.secondaryText, fontSize: 12 },
  check: { color: surface.secondaryText, flexShrink: 0, marginTop: 2 },
});

/** Composer control: consent mode for the session. Root-only, applies when idle. */
export function PermissionModePicker({ view, root, connected, agentId }: Props) {
  const runtime = useRuntime();
  const [open, setOpen] = useState(false);
  const idle = !Object.keys(root.active_turns ?? {}).length;
  if (agentId !== view.session.rootId) return null;
  const current = root.permission_mode || '';
  const active = modes.find((mode) => mode.value === current);
  const TriggerIcon = active?.danger ? ShieldAlert : ShieldCheck;
  const pick = (mode: Mode) => {
    setOpen(false);
    if (mode.value === current) return;
    runtime
      .run(
        view.session.setPermissionMode(mode.value === 'prompt'),
        mode.value === 'automatic' ? 'Enable Full Access' : 'Require approval prompts',
      )
      .catch((error) => runtime.report(error));
  };
  return (
    <Popover
      open={open}
      onOpenChange={setOpen}
      title="How should permissions be approved?"
      xstyle={styles.popup}
      trigger={
        <Button
          variant="ghost"
          aria-label="Permission approval mode"
          disabled={!connected || !idle}
          title={
            !idle
              ? 'Wait for active turns to finish before changing the permission mode'
              : active
                ? `Permissions: ${active.label}`
                : 'Permission approval mode'
          }
          xstyle={[styles.trigger, active?.danger && styles.danger]}
        >
          <TriggerIcon size={14} {...stylex.props(active?.danger ? undefined : styles.chevron)} />
          <span {...stylex.props(layout.ellipsis)}>
            {active ? active.label : connected ? 'Permissions' : 'Permissions unavailable'}
          </span>
          <ChevronDown size={14} {...stylex.props(styles.chevron)} />
        </Button>
      }
    >
      {open && (
        <div {...stylex.props(layout.column)} role="listbox" aria-label="Permission approval mode" aria-activedescendant={current}>
          {modes.map((mode) => (
            <button
              key={mode.value}
              id={mode.value}
              role="option"
              aria-selected={mode.value === current}
              {...stylex.props(styles.option, mode.value === current && styles.optionActive)}
              onClick={() => pick(mode)}
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
