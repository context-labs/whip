import { Badge, Select } from '@whip/ui';
import * as stylex from '@stylexjs/stylex';
import { colors, scale, surface } from '@whip/ui/tokens.stylex';
import type { HostConnection } from './hosts';

/** Host identity and connection state, shared by settings and new-session setup. */
export function HostSelector({ hosts, host, state, onValueChange }: {
  hosts: readonly HostConnection[]; host?: HostConnection; state?: HostConnection['state'] | 'identity changed'; onValueChange(value: string): void;
}) {
  const status = state ?? host?.state ?? 'closed';
  const options = hosts.map(item => ({ value: item.id, label: item.name, disabled: false }));
  if (host && !hosts.some(item => item.id === host.id)) options.push({ value: host.id, label: host.name, disabled: true });
  const connected = status === 'connected';
  const label = status === 'closed' ? 'Disconnected' : status[0].toUpperCase() + status.slice(1);
  return <div {...stylex.props(styles.row)}>
    <Select label="Execution host" value={host?.id ?? ''} options={options}
      onValueChange={onValueChange} valueSuffix={<span aria-hidden {...stylex.props(styles.dot, connected && styles.connected)} />} />
    <Badge tone={connected ? 'success' : 'neutral'}>{label}</Badge>
  </div>;
}

const styles = stylex.create({
  row: { display: 'flex', alignItems: 'center', flexWrap: 'wrap', gap: scale.space2 },
  dot: { width: 6, height: 6, flexShrink: 0, borderRadius: '50%', backgroundColor: surface.secondaryText },
  connected: { backgroundColor: colors.success },
});
