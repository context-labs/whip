import { Button, Menu } from '@whip/ui';
import { Check, ChevronDown, Monitor, Server, SlidersHorizontal } from 'lucide-react';
import * as stylex from '@stylexjs/stylex';
import { colors, surface, typography } from '@whip/ui/tokens.stylex';
import type { HostConnection } from './hosts';
import { layout } from './styles';

export function WelcomeHostPicker({ hosts, host, disabled, onSelect, onManage }: {
  hosts: readonly HostConnection[]; host?: HostConnection; disabled: boolean;
  onSelect(id: string): void; onManage(): void;
}) {
  return <Menu align="start" trigger={<Button variant="ghost" aria-label="Execution host" disabled={disabled} xstyle={styles.trigger}>
    {host?.local ? <Monitor size={14} /> : <Server size={14} />}<span {...stylex.props(layout.ellipsis)}>{host?.name ?? 'Choose host'}</span><ChevronDown size={14} />
  </Button>} items={[
    ...hosts.map(item => {
      const status = item.state === 'closed' ? 'Disconnected' : item.state[0].toUpperCase() + item.state.slice(1);
      const detail = item.local ? 'This computer' : item.profile.target.kind === 'ssh' ? item.profile.target.host : item.endpoint;
      return { id: item.id, disabled, onSelect: () => onSelect(item.id), label: <span {...stylex.props(styles.row)}>
        <span {...stylex.props(styles.icon)}>{item.local ? <Monitor size={16} /> : <Server size={16} />}</span>
        <span {...stylex.props(styles.identity)}><span {...stylex.props(styles.heading)}><span {...stylex.props(layout.ellipsis)}>{item.name}</span>
          <span {...stylex.props(styles.status)}><span {...stylex.props(styles.dot, item.state === 'connected' && styles.connected)} />{status}</span></span>
          <span {...stylex.props(styles.detail, layout.ellipsis)}>{detail}</span></span>
        <span {...stylex.props(styles.check)}>{item.id === host?.id && <Check size={14} aria-label="Selected host" />}</span>
      </span> };
    }),
    { id: 'separator', label: '', separator: true },
    { id: 'manage', label: 'Manage servers', icon: <SlidersHorizontal size={16} />, onSelect: onManage },
  ]} />;
}

const styles = stylex.create({
  trigger: { maxWidth: '100%', minWidth: 0 },
  row: { display: 'flex', alignItems: 'center', gap: 10, width: 'min(328px, calc(100vw - 64px))', minHeight: 48 },
  icon: { width: 20, flexShrink: 0, display: 'flex', justifyContent: 'center' },
  identity: { flex: 1, minWidth: 0, display: 'flex', flexDirection: 'column', gap: 2 },
  heading: { display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 8 },
  status: { display: 'flex', alignItems: 'center', gap: 5, flexShrink: 0, fontSize: typography.size11, color: surface.secondaryText },
  dot: { width: 5, height: 5, borderRadius: '50%', backgroundColor: surface.secondaryText },
  connected: { backgroundColor: colors.success },
  detail: { fontSize: typography.size12, color: surface.secondaryText },
  check: { display: 'flex', width: 14, flexShrink: 0 },
});
