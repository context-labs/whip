import type { CellExecutionRow, DeepReadonly } from '@whip/sdk/state';
import type { Session, SessionRecord } from '@whip/sdk';
import { ContentRead } from './details/content-read';
import { cellOutput, recordedDuration } from './execution-output';
import { useState } from 'react';
import { Button, CodeBlock } from '@whip/ui';
import {
  ArrowUpRight,
  Bot,
  Brain,
  ChevronRight,
  CircleAlert,
  FilePenLine,
  FileSearch,
  FileText,
  Globe,
  Terminal,
} from 'lucide-react';
import * as stylex from '@stylexjs/stylex';
import { colors, scale, surface, typography } from '@whip/ui/tokens.stylex';
import {
  activityItems,
  activitySummary,
  fileOperationPath,
  spawnedSession,
  type ActivityGroup,
  type ActivityItem,
  type AgentActivityRow,
} from './chat-activity-rows';
import type { TimelineRow } from './conversation-rows';
import { Shimmer } from './transcript-motion';

export type TranscriptAgent = DeepReadonly<SessionRecord>;
export type Density = 'compact' | 'comfortable' | 'detailed';

export function ActivityHeader({
  group,
  open,
  toggle,
  connected,
  density,
}: {
  group: ActivityGroup;
  open: boolean;
  toggle(): void;
  connected: boolean;
  density: Density;
}) {
  const preview = activityItems(group).at(-1);
  const snippet = (
    preview?.host?.result?.failure ||
    (preview?.host && (fileOperationPath(preview.host) ?? preview.host.resource)) ||
    preview?.row?.text ||
    (preview?.cell
      ? cellOutput(preview.cell, connected).output
      : '')
  )
    .slice(0, 512)
    .split('\n')
    .slice(0, 3)
    .join('\n');
  return (
    <article
      data-activity-group={group.id}
      data-activity-owner={group.id}
      data-message-role="activity"
      {...stylex.props(styles.header)}
    >
      <button
        type="button"
        data-activity-content
        aria-expanded={open}
        onClick={(event) => {
          const selection = window.getSelection();
          if (
            event.detail &&
            selection &&
            !selection.isCollapsed &&
            event.currentTarget.contains(selection.anchorNode)
          )
            return;
          toggle();
        }}
        {...stylex.props(styles.button)}
      >
        <ChevronRight
          size={14}
          aria-hidden="true"
          {...stylex.props(styles.chevron, open && styles.rotated)}
        />
        <Shimmer active={!!group.live && !!group.autoOpen && connected}>
          {activitySummary(group)}
        </Shimmer>
        {group.live && group.autoOpen && (
          <span {...stylex.props(styles.muted)}>
            {connected ? 'Working' : 'Updates paused'}
          </span>
        )}
      </button>
      {!open && density === 'comfortable' && snippet && (
        <div data-tool-preview {...stylex.props(styles.preview)}>
          {snippet}
        </div>
      )}
    </article>
  );
}

function operation(item: ActivityItem) {
  const name = item.host?.capability ?? '';
  if (item.kind === 'reasoning')
    return { label: 'Thought', icon: Brain, detail: '' };
  if (item.kind === 'mailbox')
    return { label: 'Agent messages', icon: Bot, detail: '' };
  if (item.kind === 'execution')
    return { label: 'Execution', icon: Terminal, detail: '' };
  const label =
    name === 'files.read'
      ? 'Read'
      : ['files.search', 'files.list'].includes(name)
        ? 'Search'
        : name === 'files.write'
          ? 'Write'
          : name === 'files.patch'
            ? 'Edit'
            : ['shell.run', 'shell.start'].includes(name)
              ? 'Run'
              : name === 'shell.wait'
                ? 'Wait'
                : name.startsWith('browser.')
                  ? 'Browser'
                  : name;
  const icon =
    label === 'Read'
      ? FileText
      : label === 'Search'
        ? FileSearch
        : ['Write', 'Edit'].includes(label)
          ? FilePenLine
          : label === 'Browser'
            ? Globe
            : Terminal;
  return {
    label, icon,
    detail: item.host ? fileOperationPath(item.host) ?? item.host.resource : '',
  };
}

export function ActivityStep({
  item,
  groupId,
  open,
  toggle,
  connected,
  last,
}: {
  item: ActivityItem;
  groupId: string;
  open: boolean;
  toggle(): void;
  connected: boolean;
  last: boolean;
}) {
  const { label, icon: Icon, detail } = operation(item);
  const status = item.host?.state ?? item.cell?.cell.state;
  return (
    <div
      data-activity-owner={groupId}
      data-activity-step={item.id}
      {...stylex.props(styles.step)}
    >
      <svg
        width="28"
        height="32"
        viewBox="0 0 28 32"
        aria-hidden="true"
        {...stylex.props(styles.connector)}
      >
        <path
          data-connector
          d={`M9 0V12Q9 19 16 19H27${last ? '' : 'M9 12V32'}`}
          fill="none"
          stroke="currentColor"
          strokeWidth="1"
        />
      </svg>
      <button
        type="button"
        data-activity-content
        aria-expanded={open}
        onClick={toggle}
        {...stylex.props(styles.button, styles.stepButton)}
      >
        <Icon size={14} aria-hidden="true" {...stylex.props(styles.fixed)} />
        <span {...stylex.props(styles.fixed)}>{label}</span>
        {detail && (
          <span title={detail} {...stylex.props(styles.subject)}>
            {detail}
          </span>
        )}
        {status && (
          <span
            {...stylex.props(
              styles.muted,
              styles.fixed,
              status === 'failed' && styles.error,
            )}
          >
            {['running', 'waiting', 'ready', 'dispatched'].includes(status)
              ? connected
                ? 'Running'
                : 'Paused'
              : status === 'uncertain'
                ? 'Outcome uncertain'
                : status === 'succeeded'
                  ? 'Done'
                  : status}
          </span>
        )}
        {item.host &&
          recordedDuration(item.host.created_at, item.host.finished_at) && (
            <span
              {...stylex.props(styles.muted, styles.fixed, styles.duration)}
            >
              {recordedDuration(item.host!.created_at, item.host!.finished_at)}
            </span>
          )}
        <ChevronRight
          size={12}
          aria-hidden="true"
          {...stylex.props(styles.chevron, open && styles.rotated)}
        />
      </button>
    </div>
  );
}

function ExecutionDetails({
  cell,
  onOpenRepl,
  connected,
}: {
  connected: boolean;
  cell: CellExecutionRow;
  onOpenRepl?(): void;
}) {
  const [full, setFull] = useState(false);
  const code =
    typeof cell.call?.value.arguments.code === 'string'
      ? cell.call.value.arguments.code
      : undefined;
  const output = cellOutput(cell, connected);
  const language =
    cell.cell.checkpoint?.engine === 'quickjs'
      ? 'javascript'
      : cell.cell.checkpoint?.engine === 'starlark'
        ? 'python'
        : undefined;
  return (
    <div {...stylex.props(styles.stack)}>
      {code !== undefined && (
        <CodeBlock code={code} language={language} label="Executed code" />
      )}
      {output.output && (
        <CodeBlock code={output.output} language="text" label={output.provisional ? "Live output · provisional" : "Output"} />
      )}
      {output.provisional && <p {...stylex.props(styles.muted)}>Provisional stdout; the committed result will replace it.</p>}
      {output.truncated && <p {...stylex.props(styles.muted)}>Live output is truncated to the first 64 KiB.</p>}
      {output.value !== undefined && (
        <CodeBlock code={output.value} language="json" label="Value" />
      )}
      {[code ?? '', output.output, output.value ?? ''].some(
        (text) => new TextEncoder().encode(text).length > 16384,
      ) && (
        <details
          open={full}
          onToggle={(event) => setFull(event.currentTarget.open)}
        >
          <summary>Show all retained code and output</summary>
          {full && (
            <pre {...stylex.props(styles.output)}>
              {[code, output.output, output.value].filter(Boolean).join('\n\n')}
            </pre>
          )}
        </details>
      )}
      {output.error && <p {...stylex.props(styles.error)}>{output.error}</p>}
      {output.restored && (
        <p {...stylex.props(styles.muted)}>{output.restored}</p>
      )}
      {(!cell.call ||
        (cell.cell.result_message_id !== null && !cell.result)) && (
        <p {...stylex.props(styles.muted)}>
          The exact call or result is outside the loaded transcript window.
        </p>
      )}
      {cell.cell.state === 'running' && !cell.result && (
        <p {...stylex.props(styles.muted)}>
          Execution is in progress. Its committed output is not available yet.
        </p>
      )}
      {onOpenRepl && (
        <Button size="sm" variant="ghost" xstyle={styles.replAction} onClick={onOpenRepl}>
          Open in REPL <ArrowUpRight size={13} />
        </Button>
      )}
    </div>
  );
}

export function ActivityDetail({
  item,
  groupId,
  readBody,
  onOpenRepl,
  session,
  connected,
}: {
  session?: Session;
  connected: boolean;
  item: ActivityItem;
  groupId: string;
  readBody(row: TimelineRow): void;
  onOpenRepl?(): void;
}) {
  const row = item.row;
  const text = row?.text ?? '';
  const lines = text.split('\n');
  const preview = (row?.live ? lines.slice(-24) : lines.slice(0, 24)).join(
    '\n',
  );
  return (
    <div
      data-activity-owner={groupId}
      data-activity-detail={item.id}
      {...stylex.props(styles.detail)}
    >
      {row && (
        <>
          <pre {...stylex.props(styles.reasoning)}>{preview}</pre>
          {lines.length > 24 && (
            <details>
              <summary>
                Show all retained{' '}
                {item.kind === 'reasoning' ? 'reasoning' : 'agent messages'}
              </summary>
              <pre {...stylex.props(styles.output)}>{text}</pre>
            </details>
          )}
          {row.truncated && (
            <p {...stylex.props(styles.muted)}>
              Some earlier activity was omitted.
            </p>
          )}
          {row.body && (
            <Button size="sm" variant="ghost" onClick={() => readBody(row)}>
              Read stored details
            </Button>
          )}
        </>
      )}
      {item.host && (
        <>
          <p {...stylex.props(styles.muted)}>Permission scope: {item.host.resource}</p>
          <CodeBlock
            code={JSON.stringify(item.host.arguments, null, 2)}
            label="Operation arguments"
            maxBytes={32 << 10}
          />
          {item.host.result?.value !== undefined && (
            <CodeBlock
              code={JSON.stringify(item.host.result.value, null, 2)}
              label="Operation result"
              maxBytes={32 << 10}
            />
          )}
          {session &&
            item.host.result?.content_references.map((reference) => (
              <ContentRead
                key={reference}
                session={session}
                connected={connected}
                reference={reference}
                label="Host result"
              />
            ))}
          {item.host.result?.failure && (
            <p {...stylex.props(styles.error)}>
              <CircleAlert size={13} /> {item.host.result?.failure}
            </p>
          )}
        </>
      )}
      {item.cell && (
        <ExecutionDetails connected={connected} cell={item.cell} onOpenRepl={onOpenRepl} />
      )}
    </div>
  );
}

export function InlineAgent({
  row,
  agent,
  connected,
  onAgent,
  readBody,
  onOpenRepl,
}: {
  row: AgentActivityRow;
  agent?: TranscriptAgent;
  connected: boolean;
  onAgent?(id: string): void;
  readBody(row: TimelineRow): void;
  onOpenRepl?(): void;
}) {
  const [details, setDetails] = useState(false);
  const id = spawnedSession(row.agentHost);
  const value = row.agentHost.result?.value;
  const recordedName = id && value && typeof value === 'object' && 'name' in value && typeof value.name === 'string' ? value.name : undefined;
  const title = agent?.name || recordedName || agent?.definition.id || id || 'Agent';
  // This is launch evidence, not a second live roster. A child's later turn does
  // not change the outcome of the operation that launched it.
  const launchStatus =
    row.agentHost.state === 'succeeded'
      ? 'Launched'
      : row.agentHost.state === 'dispatched'
        ? connected
          ? 'Launching'
          : 'Launch updates paused'
        : row.agentHost.state === 'failed'
          ? 'Failed to launch'
          : row.agentHost.state === 'cancelled'
            ? 'Launch cancelled'
            : row.agentHost.state === 'uncertain'
              ? 'Launch outcome uncertain'
              : 'Launch status unavailable';
  return (
    <article data-inline-agent={id || row.id} {...stylex.props(styles.agent)}>
      <div data-activity-content {...stylex.props(styles.launchRow)}>
        <button
          type="button"
          disabled={!id || !onAgent}
          onClick={() => id && onAgent?.(id)}
          title={id ? `Open ${title} chat to the right` : undefined}
          {...stylex.props(styles.button, styles.agentButton)}
        >
          <Bot size={14} aria-hidden="true" />
          <span>{launchStatus}</span>{' '}
          <strong {...stylex.props(styles.agentName)}>{title}</strong>
          {id && <ArrowUpRight size={13} aria-hidden="true" />}
        </button>
        <details
          {...stylex.props(styles.launchDetails)}
          onToggle={(event) => setDetails(event.currentTarget.open)}
        >
          <summary {...stylex.props(styles.launchSummary)}>
            Launch details
          </summary>
          {details && (
            <ExecutionDetails connected={connected} cell={row.cell} onOpenRepl={onOpenRepl} />
          )}
        </details>
      </div>
      {row.agentHost.result?.failure && (
        <p {...stylex.props(styles.error)}>{row.agentHost.result?.failure}</p>
      )}
    </article>
  );
}

const styles = stylex.create({
  header: {
    paddingBlock: 4,
    color: surface.secondaryText,
    fontSize: typography.size13,
  },
  button: {
    display: 'flex',
    flexWrap: 'wrap',
    alignItems: 'center',
    gap: 8,
    borderWidth: 0,
    borderRadius: scale.radiusControl,
    backgroundColor: 'transparent',
    color: 'inherit',
    fontFamily: 'inherit',
    fontSize: 'inherit',
    textAlign: 'left',
    cursor: 'pointer',
    padding: '4px 2px',
    minHeight: { default: 28, '@media (pointer: coarse)': 44 },
    maxWidth: '100%',
    outlineOffset: 2,
  },
  agentButton: {
    backgroundColor: { default: 'transparent', ':hover': colors.hover },
  },
  chevron: {
    flexShrink: 0,
    transitionProperty: 'transform',
    transitionDuration: {
      default: '140ms',
      [scale.reducedMotion]: '0ms',
      ':where([data-motion="reduce"]) *': '0ms',
    },
    transitionTimingFunction: 'ease-out',
  },
  rotated: { transform: 'rotate(90deg)' },
  preview: {
    marginLeft: 24,
    whiteSpace: 'pre-wrap',
    overflowWrap: 'anywhere',
    maxHeight: '4.8em',
    overflow: 'hidden',
    fontSize: typography.size12,
    lineHeight: 1.6,
  },
  step: {
    position: 'relative',
    minHeight: 32,
    paddingLeft: 32,
    color: surface.secondaryText,
    fontSize: typography.size12,
  },
  connector: {
    position: 'absolute',
    left: 2,
    top: 0,
    color: surface.quietBorder,
  },
  stepButton: { minHeight: 32, width: '100%', flexWrap: 'nowrap' },
  fixed: { flexShrink: 0, whiteSpace: 'nowrap' },
  duration: { display: { default: 'inline', [scale.phone]: 'none' } },
  subject: {
    flex: 1,
    fontFamily: typography.mono,
    overflow: 'hidden',
    textOverflow: 'ellipsis',
    whiteSpace: 'nowrap',
    minWidth: 0,
    color: colors.foreground,
  },
  muted: {
    color: surface.secondaryText,
    fontSize: typography.size12,
    overflowWrap: 'anywhere',
  },
  detail: {
    marginLeft: 11,
    paddingLeft: 28,
    paddingBottom: 12,
    borderLeft: `1px solid ${surface.quietBorder}`,
    color: surface.secondaryText,
    fontSize: typography.size12,
    minWidth: 0,
  },
  replAction: { alignSelf: 'flex-start' },
  stack: { display: 'flex', flexDirection: 'column', gap: 8, minWidth: 0 },
  reasoning: {
    whiteSpace: 'pre-wrap',
    overflowWrap: 'anywhere',
    fontFamily: typography.sans,
    fontSize: typography.size13,
    lineHeight: 1.65,
    margin: 0,
    maxWidth: '96ch',
    maxHeight: '39.6em',
    overflowY: 'auto',
  },
  output: {
    whiteSpace: 'pre-wrap',
    overflowWrap: 'anywhere',
    fontFamily: typography.mono,
    margin: 0,
    maxHeight: 520,
    overflow: 'auto',
  },
  error: { color: colors.error, overflowWrap: 'anywhere' },
  agent: {
    marginBlock: 4,
    paddingBlock: 2,
    color: surface.secondaryText,
    fontSize: typography.size12,
  },
  agentName: { color: colors.foreground, overflowWrap: 'anywhere' },
  launchRow: {
    display: 'flex',
    flexWrap: 'wrap',
    alignItems: 'center',
    columnGap: 12,
    minWidth: 0,
  },
  launchDetails: {
    minWidth: 0,
    maxWidth: '100%',
    flexBasis: { default: 'auto', ':is([open])': '100%' },
  },
  launchSummary: {
    cursor: 'pointer',
    paddingBlock: 6,
    minHeight: { default: 28, '@media (pointer: coarse)': 44 },
  },
});
