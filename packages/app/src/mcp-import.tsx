import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import type { WhipClient } from '@whip/sdk';
import type { MCPImportApplyResult, MCPImportCandidatesResult } from '@whip/protocol';
import { Button, Checkbox } from '@whip/ui';
import { Check } from 'lucide-react';
import * as stylex from '@stylexjs/stylex';
import { colors, scale, surface, typography } from '@whip/ui/tokens.stylex';
import { useRuntime } from './context';
import { ErrorNotice } from './error-feedback';
import { layout } from './styles';

// The import screen: the servers other agents configured on a host, one flat
// list, tick what you want, and they become Whip's own (trusted) servers. The
// daemon reads files only; nothing here dials or launches a server.

/** One row of the daemon's answer; the generated contract inlines it. */
export type MCPImportCandidate = NonNullable<MCPImportCandidatesResult['candidates']>[number];

export function candidatesQueryKey(runtimeId: string | undefined, cwd = '') { return ['mcp-import-candidates', runtimeId, cwd] as const; }

/** Older daemons have neither operation; the Welcome offer stays hidden and Settings explains. */
export function importSupported(client: WhipClient) {
  return typeof client.supports === 'function' && client.supports('rpc', 'mcp.import.candidates') && client.supports('rpc', 'mcp.import.apply');
}

export function useMCPImportCandidates(client: WhipClient, { enabled, cwd = '' }: { enabled: boolean; cwd?: string }) {
  const runtimeId = client.getSnapshot().info?.runtime_id;
  const supported = importSupported(client);
  const query = useQuery({
    queryKey: candidatesQueryKey(runtimeId, cwd),
    queryFn: ({ signal }) => client.mcpImport.candidates(cwd ? { cwd } : {}, { signal }),
    enabled: enabled && supported,
    staleTime: 30_000,
  });
  return { query, supported, runtimeId };
}

/** A host is offered the screen once: when it has something importable and has not answered yet. */
export function shouldOffer(data: MCPImportCandidatesResult | undefined) {
  return !!data && !data.offered && (data.candidates ?? []).some(candidate => candidate.state === 'importable');
}

/** Everything that is not already in Whip first, A→Z; the already-native servers last, A→Z. */
export function sortCandidates(candidates: readonly MCPImportCandidate[]) {
  return [...candidates].sort((a, b) => Number(a.state === 'native') - Number(b.state === 'native') || a.name.localeCompare(b.name));
}

const sourceNames: Record<string, string> = { codex: 'Codex', claude: 'Claude', opencode: 'OpenCode', project: "the project's .mcp.json" };
const sourceOrder = ['codex', 'claude', 'opencode', 'project'];

function describeSources(candidates: readonly MCPImportCandidate[]) {
  const present = sourceOrder.filter(source => candidates.some(candidate => candidate.source === source)).map(source => sourceNames[source]);
  if (present.length <= 1) return present[0] ?? 'other agents';
  return `${present.slice(0, -1).join(', ')}, and ${present[present.length - 1]}`;
}

/** The short reason on the right of a row; empty for a plain importable server. */
export function caveat(candidate: MCPImportCandidate, included: boolean) {
  switch (candidate.state) {
    case 'native': return 'Already in Whip';
    case 'disabled': return `Off in ${sourceNames[candidate.source] ?? candidate.source}`;
    case 'unsupported': return candidate.note || 'Not supported yet';
    case 'excluded': return included ? '' : 'Excluded by your rules';
    default: return '';
  }
}

export function MCPImportScreen({ client, hostName, cwd = '', onDone }: {
  client: WhipClient; hostName: string; cwd?: string; onDone?: (result: MCPImportApplyResult | undefined) => void;
}) {
  const runtime = useRuntime();
  const { query, runtimeId, supported } = useMCPImportCandidates(client, { enabled: true, cwd });
  // Ticks the person changed; everything else follows the row's default.
  const [overrides, setOverrides] = useState<Record<string, boolean>>({});
  const [included, setIncluded] = useState<Record<string, true>>({});
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>();
  const data = query.data;
  const rows = sortCandidates(data?.candidates ?? []);
  const selectable = (candidate: MCPImportCandidate) =>
    candidate.state === 'importable' || candidate.state === 'disabled' || (candidate.state === 'excluded' && !!included[candidate.name]);
  const checked = (candidate: MCPImportCandidate) => selectable(candidate) && (overrides[candidate.name] ?? candidate.state === 'importable');
  const chosen = rows.filter(checked).map(candidate => candidate.name);
  async function apply(names: string[]) {
    setBusy(true); setError(undefined);
    try {
      const result = await client.mcpImport.apply(cwd ? { cwd, names } : { names });
      runtime.queries.setQueryData(candidatesQueryKey(runtimeId, cwd), (old: MCPImportCandidatesResult | undefined) => old && { ...old, offered: true });
      void runtime.queries.invalidateQueries({ queryKey: ['mcp-import-candidates', runtimeId] });
      onDone?.(result);
    } catch (value) { setError(value); } finally { setBusy(false); }
  }
  if (!supported) return <p role="status" {...stylex.props(layout.muted)}>{hostName} runs an older daemon without MCP import. Update whipcode there, or run <code>whip mcp import</code> on that machine.</p>;
  if (query.isPending) return <p role="status" {...stylex.props(layout.muted)}>Looking for MCP servers on {hostName}…</p>;
  if (query.error) return <ErrorNotice type="resource" owner={`${runtimeId}:mcp-import`} title="Could not read the other agents' configuration" error={query.error} />;
  if (!data) return null;
  const found = rows.filter(row => row.state !== 'native').length;
  return <div aria-label="Import MCP servers" {...stylex.props(styles.screen)}>
    {rows.length === 0
      ? <p {...stylex.props(styles.intro)}>No MCP servers were found in Codex, Claude, or OpenCode on {hostName}.</p>
      : <p {...stylex.props(styles.intro)}>Whip found {found} {found === 1 ? 'server' : 'servers'} in {describeSources(rows)} on {hostName}. Pick the ones to add; they run as Whip's own servers, without per-call approval.</p>}
    {rows.length > 0 && <ul role="list" aria-label="Discovered MCP servers" {...stylex.props(styles.list)}>
      {rows.map(candidate => {
        const on = checked(candidate);
        const reason = caveat(candidate, !!included[candidate.name]);
        return <li key={candidate.name} {...stylex.props(styles.row)}>
          <span {...stylex.props(styles.slot)}>{candidate.state === 'native'
            ? <Check size={14} aria-label={`${candidate.name} is already in Whip`} {...stylex.props(styles.nativeMark)} />
            : <Checkbox aria-label={`Import ${candidate.name}`} checked={on} disabled={busy || !selectable(candidate)}
              onCheckedChange={value => setOverrides(previous => ({ ...previous, [candidate.name]: !!value }))} />}</span>
          <span aria-hidden {...stylex.props(styles.tile)}>{candidate.name.slice(0, 1).toUpperCase()}</span>
          <span {...stylex.props(styles.name, candidate.state === 'native' && styles.quiet, candidate.state === 'unsupported' && styles.quiet)}>{candidate.name}</span>
          <span {...stylex.props(styles.caveat)}>
            {reason}
            {candidate.state === 'excluded' && !included[candidate.name] && <>
              {' · '}<Button variant="ghost" size="sm" xstyle={styles.inlineAction} disabled={busy}
                onClick={() => setIncluded(previous => ({ ...previous, [candidate.name]: true }))}>Include</Button>
            </>}
          </span>
        </li>;
      })}
    </ul>}
    {data.errors && Object.entries(data.errors).map(([path, message]) =>
      <p key={path} role="status" {...stylex.props(layout.muted)}>Couldn't read {path}: {message}</p>)}
    <ErrorNotice type="action" owner={`${runtimeId}:mcp-import`} title="Could not import" error={error} />
    <div {...stylex.props(styles.actions)}>
      <span {...stylex.props(styles.summary)}>
        {rows.length > 0 && <>{chosen.length} selected · saved to Whip's configuration on {hostName} </>}
        {data.config_path && <code {...stylex.props(styles.path)}>{data.config_path}</code>}
      </span>
      <span {...stylex.props(layout.grow)} />
      {!data.offered && <Button variant="ghost" disabled={busy} onClick={() => void apply([])}>Skip for now</Button>}
      {rows.length > 0 && <Button variant="primary" loading={busy} disabled={chosen.length === 0} onClick={() => void apply(chosen)}>
        Import {chosen.length} {chosen.length === 1 ? 'server' : 'servers'}
      </Button>}
    </div>
  </div>;
}

const styles = stylex.create({
  screen: { display: 'flex', flexDirection: 'column', gap: scale.space3, minWidth: 0 },
  intro: { margin: 0, color: surface.secondaryText, lineHeight: 1.5 },
  list: { listStyle: 'none', margin: 0, padding: 0, backgroundColor: colors.panel, borderRadius: scale.radiusPanel, overflow: 'hidden' },
  row: {
    display: 'flex', alignItems: 'center', gap: 12, minHeight: 34, paddingInline: 14,
    borderBottomWidth: 1, borderBottomStyle: 'solid', borderBottomColor: surface.quietBorder,
    ':last-child': { borderBottomWidth: 0 },
  },
  slot: { display: 'inline-flex', width: 16, justifyContent: 'center', flexShrink: 0 },
  nativeMark: { color: surface.secondaryText },
  tile: {
    display: 'inline-flex', alignItems: 'center', justifyContent: 'center', width: 22, height: 22, flexShrink: 0,
    borderRadius: scale.radiusSmall, backgroundColor: colors.element, color: surface.secondaryText,
    fontSize: typography.size11, fontWeight: 600, lineHeight: 1,
  },
  name: { flex: 1, minWidth: 0, fontWeight: 500, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' },
  quiet: { color: surface.secondaryText, fontWeight: 400 },
  caveat: { display: 'inline-flex', alignItems: 'center', flexShrink: 0, fontSize: typography.size12, color: surface.secondaryText, whiteSpace: 'nowrap' },
  inlineAction: { paddingInline: 4, minHeight: 0, height: 'auto', fontSize: typography.size12 },
  actions: { display: 'flex', alignItems: 'center', flexWrap: 'wrap', gap: scale.space2, minWidth: 0 },
  summary: { fontSize: typography.size12, color: surface.secondaryText, minWidth: 0 },
  path: { fontFamily: typography.mono, fontSize: typography.size11 },
});
