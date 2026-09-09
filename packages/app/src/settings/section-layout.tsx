import { useId, type ReactNode } from 'react';
import * as stylex from '@stylexjs/stylex';
import { SettingsRow } from '@whip/ui';
import { colors, scale, surface, typography } from '@whip/ui/tokens.stylex';

export function SettingsGroup({ title, children }: { title?: string; children: ReactNode }) {
  const titleId = useId();
  return <section aria-labelledby={title ? titleId : undefined} {...stylex.props(settingsSection.group)}>
    {title && <h2 id={titleId} {...stylex.props(settingsSection.heading)}>{title}</h2>}
    <div {...stylex.props(settingsSection.panel)}>{children}</div>
  </section>;
}

export function SettingRow({ id, label, description, children }: {
  id: string; label: ReactNode; description?: ReactNode; children: ReactNode;
}) {
  return <div id={id} tabIndex={-1} role="group" aria-label={typeof label === 'string' ? `${label} setting` : 'Preference setting'} aria-describedby={description ? `${id}-description` : undefined} {...stylex.props(settingsSection.row)}>
    <SettingsRow xstyle={settingsSection.rowContent} label={<span id={`${id}-label`}>{label}</span>} description={description ? <span id={`${id}-description`}>{description}</span> : undefined}>{children}</SettingsRow>
  </div>;
}

export const settingsSection = stylex.create({
  group: { display: 'flex', flexDirection: 'column', gap: scale.space2, minWidth: 0 },
  heading: { color: surface.secondaryText, fontSize: typography.size13, fontWeight: 500, lineHeight: 1.5, margin: 0, paddingInline: scale.space2 },
  panel: { backgroundColor: colors.panel, borderRadius: scale.radiusDialog, padding: scale.space4, display: 'flex', flexDirection: 'column', gap: scale.space3, minWidth: 0 },
  row: { borderBottomWidth: { default: 1, ':last-child': 0 }, borderBottomStyle: 'solid', borderBottomColor: surface.quietBorder, paddingBlock: scale.space2, minWidth: 0, scrollMarginBlock: scale.space6, outlineOffset: 4 },
  rowContent: { paddingBlock: 0, borderBottomWidth: 0 },
  control: { width: { default: 220, [scale.phone]: '100%' }, maxWidth: '100%', minWidth: 0 },
});
