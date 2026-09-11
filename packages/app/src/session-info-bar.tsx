import type { ReactNode } from 'react';
import { Button, IconButton, Menu, Tooltip, type MenuItem } from '@whip/ui';
import { ChevronDown, Code2, MoreHorizontal, PanelRight } from 'lucide-react';
import * as stylex from '@stylexjs/stylex';
import { colors, scale, surface, typography } from '@whip/ui/tokens.stylex';

export function SessionInfoBar({ host, cwd, agentName, kind, activity, onAgents, onRoot, onRepl, onDetails, actions = [], onPrepare }: {
  host: string;
  cwd?: string;
  agentName?: string;
  kind: 'chat' | 'repl' | 'new';
  activity?: ReactNode;
  onAgents?(): void;
  onRoot?(): void;
  onRepl?(): void;
  onDetails?(): void;
  actions?: readonly MenuItem[];
  onPrepare?(open: boolean): void;
}) {
  const missingProject = kind === 'new' ? 'Choose a project' : 'Directory unavailable';
  const project = cwd?.split(/[\\/]/).filter(Boolean).at(-1) || cwd || missingProject;
  const identity = `${host} / ${cwd || missingProject}`;
  const menu: MenuItem[] = [
    ...(onRepl ? [{ id: 'repl', label: 'Open REPL', onSelect: onRepl }] : []),
    ...(onDetails ? [{ id: 'details', label: 'Session details', onSelect: onDetails }] : []),
    ...(onRoot ? [{ id: 'root', label: 'Root conversation', onSelect: onRoot }] : []),
    ...actions,
  ];
  return <header aria-label="Session information" data-session-info-bar {...stylex.props(styles.container)}>
    <div {...stylex.props(styles.row)}>
      <Tooltip label={identity}>
        <span tabIndex={0} aria-label={identity} {...stylex.props(styles.project)}>
          <span {...stylex.props(styles.host)}>{host}<span aria-hidden="true"> / </span></span>{project}
        </span>
      </Tooltip>
      {agentName && <><span aria-hidden="true" {...stylex.props(styles.separator)}>/</span>
        {onAgents ? <Button variant="ghost" size="sm" xstyle={styles.agent} aria-label={`Agent: ${agentName}`} onClick={onAgents}>
          <span {...stylex.props(styles.truncate)}>{agentName}</span><ChevronDown size={12} />
        </Button> : <span aria-label={`Agent: ${agentName}`} {...stylex.props(styles.truncate, styles.agent)}>{agentName}</span>}</>}
      <div {...stylex.props(styles.activity)}>{activity ?? (kind === 'new' ? <span>Not started</span> : null)}</div>
      <div {...stylex.props(styles.actions)}>
        {kind === 'repl' && <span {...stylex.props(styles.mode)}><Code2 size={14} />REPL</span>}
        {onRepl && <span {...stylex.props(styles.secondaryAction)}><Tooltip label="Open REPL in a new tab">
          <IconButton variant="ghost" size="sm" label="Open REPL" onClick={onRepl}><Code2 size={16} /></IconButton>
        </Tooltip></span>}
        {onDetails && <span {...stylex.props(styles.secondaryAction)}><Tooltip label="Session details">
          <IconButton variant="ghost" size="sm" label="Session details" onClick={onDetails}><PanelRight size={16} /></IconButton>
        </Tooltip></span>}
        {!!menu.length && <Menu onOpenChange={onPrepare} trigger={<IconButton variant="ghost" size="sm" label="Session actions"><MoreHorizontal size={16} /></IconButton>} items={menu} />}
      </div>
    </div>
  </header>;
}

const styles = stylex.create({
  container: { containerType: 'inline-size', flexShrink: 0, minWidth: 0, backgroundColor: colors.background, borderBottomWidth: 1, borderBottomStyle: 'solid', borderBottomColor: surface.quietBorder },
  row: { minHeight: { default: 36, [scale.touch]: 48 }, display: 'flex', alignItems: 'center', gap: scale.space1, paddingInline: scale.space3, paddingBlock: scale.space1, color: surface.secondaryText, fontFamily: typography.sans, fontSize: typography.size12 },
  project: { minWidth: 0, maxWidth: '32%', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', outlineOffset: 2 },
  host: { display: { default: 'inline', '@container (max-width: 600px)': 'none' } },
  separator: { flexShrink: 0 },
  agent: { minWidth: 0, maxWidth: '28%', flexShrink: 1, paddingInline: scale.space1, fontSize: typography.size12 },
  truncate: { overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', minWidth: 0 },
  activity: { display: 'flex', alignItems: 'center', flex: { default: '1 1 0', '@container (max-width: 600px)': '0 0 auto' }, marginInlineStart: 'auto', minWidth: 0, justifyContent: 'flex-end', paddingInline: scale.space1 },
  actions: { display: 'flex', alignItems: 'center', flexShrink: 0, gap: scale.space1 },
  secondaryAction: { display: { default: 'inline-flex', '@container (max-width: 600px)': 'none' } },
  mode: { display: 'inline-flex', alignItems: 'center', gap: scale.space1, fontSize: typography.size11 },
});
