import { useEffect, useRef, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import type { Client, MCPImportResult, MCPImportCandidatesResult, MCPConfiguration } from '@whip/sdk';
import { Button, Checkbox } from '@whip/ui';
import { Check } from 'lucide-react';
import * as stylex from '@stylexjs/stylex';
import { colors, scale, surface, typography } from '@whip/ui/tokens.stylex';
import { useRuntime } from './context';
import { ErrorNotice } from './error-feedback';
import { MCPBrandIcon, useBrandIcons, useBrandMarks } from './mcp-brand';
import { layout } from './styles';

// The import screen: the servers other agents configured on a host, one flat
// list, tick what you want, and they become Whip's own (trusted) servers. The
// discovery reads files only. After persistence, Settings can refresh its
// current session, which may dial or launch the newly configured servers.

/** One row of the daemon's answer; the generated contract inlines it. */
export type MCPImportCandidate = NonNullable<MCPImportCandidatesResult['candidates']>[number];

export function candidatesQueryKey(runtimeId: string, sessionID = '') { return ['mcp-import-candidates', runtimeId, sessionID] as const; }

export function importSupported(_client: Client) { return true; }
export function useMCPImportCandidates(client: Client, { enabled, sessionID = null }: { enabled: boolean; sessionID?: string | null }) {
  const runtimeId = client.runtimeID;
  const query = useQuery({ queryKey: candidatesQueryKey(runtimeId, sessionID ?? ''), queryFn: ({ signal }) => client.mcpImportCandidates(sessionID, { signal }), enabled, staleTime: 30_000 });
  return { query, supported: true, runtimeId };
}
export function shouldOffer(data: MCPImportCandidatesResult | undefined, configuration: MCPConfiguration | undefined) {
  return !!data && !!configuration && !configuration.imports.offered && (data.candidates ?? []).some(candidate => candidate.state === 'importable');
}

/** Everything that is not already in Whip first, A→Z; the already-native servers last, A→Z. */
export function sortCandidates(candidates: readonly MCPImportCandidate[]) {
  return [...candidates].sort((a, b) => Number(a.state === 'native') - Number(b.state === 'native') || a.name.localeCompare(b.name));
}

/** Import sources in the order the intro names them. */
const sources = [['codex', 'Codex'], ['claude', 'Claude'], ['opencode', 'OpenCode'], ['project', "the project's .mcp.json"]] as const;
const sourceName = (source: string) => sources.find(([id]) => id === source)?.[1] ?? source;
const listFormat = new Intl.ListFormat('en', { type: 'conjunction' });

function describeSources(candidates: readonly MCPImportCandidate[]) {
  const present = sources.filter(([id]) => candidates.some(candidate => candidate.source === id)).map(([, name]) => name);
  return present.length ? listFormat.format(present) : 'other agents';
}

/** The short reason on the right of a row; empty for a plain importable server. */
export function caveat(candidate: MCPImportCandidate, included: boolean) {
  switch (candidate.state) {
    case 'native': return 'Already in Whip';
    case 'disabled': return `Off in ${sourceName(candidate.source)}`;
    case 'unsupported': return candidate.note || 'Not supported yet';
    case 'excluded': return included ? '' : 'Excluded by your rules';
    default: return '';
  }
}

export interface MCPImportRefresh { notice: string; error?: unknown }

export function MCPImportScreen({ client, hostName, sessionID = null, onDone, enabled = true }: {
  client: Client; hostName: string; sessionID?: string | null; enabled?: boolean;
  onDone?: (result: MCPImportResult, refresh?: MCPImportRefresh) => void;
}) {
  const runtime = useRuntime();
  const { query, runtimeId } = useMCPImportCandidates(client, { enabled, sessionID });
  // Ticks the person changed; everything else follows the row's default. An
  // excluded server enters here unticked when its Include button is pressed.
  const [overrides, setOverrides] = useState<Record<string, boolean>>({});
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>();
  const request = useRef<AbortController | null>(null);
  useEffect(() => { request.current = null; setBusy(false); return () => request.current?.abort(); }, [client]);
  const data = query.data;
  const rows = sortCandidates(data?.candidates ?? []);
  // Bundled marks first; the daemon is asked only for domains the bundle lacks, once the bundle has loaded.
  const marks = useBrandMarks();
  const icons = useBrandIcons(client, enabled && marks ? rows.map(row => row.brand_key).filter((key): key is string => !!key && !marks[key]) : []);
  const mark = (candidate: MCPImportCandidate) => candidate.brand_key ? marks?.[candidate.brand_key] ?? icons?.[candidate.brand_key] : undefined;
  const included = (candidate: MCPImportCandidate) => candidate.name in overrides;
  const selectable = (candidate: MCPImportCandidate) =>
    candidate.state === 'importable' || candidate.state === 'disabled' || (candidate.state === 'excluded' && included(candidate));
  const checked = (candidate: MCPImportCandidate) => selectable(candidate) && (overrides[candidate.name] ?? candidate.state === 'importable');
  const chosen = rows.filter(checked).map(candidate => candidate.name);
  const tick = (name: string, value: boolean) => setOverrides(previous => ({ ...previous, [name]: value }));
  async function apply(names: string[]) {
    if (!enabled || request.current || !data) return;
    const controller = new AbortController(); request.current = controller; setBusy(true); setError(undefined);
    const fingerprints = Object.fromEntries(rows.filter(row => names.includes(row.name)).map(row => [row.name, row.fingerprint]));
    try {
      const result = await client.importMCP({ session_id: sessionID, revision: data.revision, fingerprints }, { signal: controller.signal });
      if (controller.signal.aborted) return;
      runtime.queries.setQueryData(['mcp-configuration', runtimeId], result.configuration);
      await runtime.queries.invalidateQueries({ queryKey: ['mcp-import-candidates', runtimeId], refetchType: 'none' });
      onDone?.(result, { notice: 'Declarations saved. Existing session connections are unchanged; refresh a session explicitly to discover them.' });
    } catch (error) { if (!controller.signal.aborted) { setError(error); await query.refetch(); } }
    finally { if (request.current === controller) request.current = null; if (!controller.signal.aborted) setBusy(false); }
  }
  if (query.isPending) return <p role="status" {...stylex.props(layout.muted)}>Looking for MCP servers on {hostName}…</p>;
  if (query.error) return <ErrorNotice type="resource" owner={`${runtimeId}:mcp-import`} title="Could not read the other agents' configuration" error={query.error} />;
  if (!data) return null;
  const found = rows.filter(row => row.state !== 'native').length;
  return <div aria-label="Import MCP servers" {...stylex.props(layout.column)}>
    {rows.length === 0
      ? <p {...stylex.props(styles.intro)}>No MCP servers were found in Codex, Claude, or OpenCode on {hostName}.</p>
      : <p {...stylex.props(styles.intro)}>Whip found {found} {found === 1 ? 'server' : 'servers'} in {describeSources(rows)} on {hostName}. Pick the ones to add; importing trusts these declarations, while calls still require permission. Connecting is a separate session action.</p>}
    {rows.length > 0 && <ul role="list" aria-label="Discovered MCP servers" {...stylex.props(styles.list)}>
      {rows.map(candidate => <li key={candidate.name} {...stylex.props(styles.row)}>
        <span {...stylex.props(styles.slot)}>{candidate.state === 'native'
          ? <Check size={14} aria-label={`${candidate.name} is already in Whip`} {...stylex.props(styles.nativeMark)} />
          : <Checkbox aria-label={`Import ${candidate.name}`} checked={checked(candidate)} disabled={!enabled || busy || !selectable(candidate)}
            onCheckedChange={value => tick(candidate.name, !!value)} />}</span>
        <MCPBrandIcon name={candidate.name} src={mark(candidate)} quiet={candidate.state === 'native' || candidate.state === 'unsupported'} />
        <span {...stylex.props(styles.name, (candidate.state === 'native' || candidate.state === 'unsupported') && styles.quiet)}>{candidate.name}</span>
        <span {...stylex.props(styles.caveat)}>
          {caveat(candidate, included(candidate))}
          {candidate.state === 'excluded' && !included(candidate) && <>
            {' · '}<Button variant="ghost" size="sm" xstyle={styles.inlineAction} disabled={!enabled || busy} onClick={() => tick(candidate.name, false)}>Include</Button>
          </>}
        </span>
      </li>)}
    </ul>}
    {data.source_errors && Object.entries(data.source_errors).map(([path, message]) =>
      <p key={path} role="status" {...stylex.props(layout.muted)}>Couldn't read {path}: {message}</p>)}
    <ErrorNotice type="action" owner={`${runtimeId}:mcp-import`} title="Could not import" error={error} />
    <div {...stylex.props(layout.row, layout.wrap)}>
      <span {...stylex.props(layout.muted)}>
        {rows.length > 0 && <>{chosen.length} selected · saved to Whip's configuration on {hostName} </>}
      </span>
      <span {...stylex.props(layout.grow)} />
      {<Button variant="ghost" disabled={!enabled || busy} onClick={() => void apply([])}>Skip for now</Button>}
      {rows.length > 0 && <Button variant="primary" loading={busy} disabled={!enabled || busy || chosen.length === 0} onClick={() => void apply(chosen)}>
        Import {chosen.length} {chosen.length === 1 ? 'server' : 'servers'}
      </Button>}
    </div>
  </div>;
}

const styles = stylex.create({
  intro: { margin: 0, color: surface.secondaryText, lineHeight: 1.5 },
  list: { listStyle: 'none', margin: 0, padding: 0, backgroundColor: colors.panel, borderRadius: scale.radiusPanel, overflow: 'hidden' },
  row: {
    display: 'flex', alignItems: 'center', gap: 12, minHeight: 34, paddingInline: 14,
    borderBottomWidth: { default: 1, ':last-child': 0 }, borderBottomStyle: 'solid', borderBottomColor: surface.quietBorder,
  },
  slot: { display: 'inline-flex', width: 16, justifyContent: 'center', flexShrink: 0 },
  nativeMark: { color: surface.secondaryText },
  name: { flex: 1, minWidth: 0, fontWeight: 500, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' },
  quiet: { color: surface.secondaryText, fontWeight: 400 },
  caveat: { display: 'inline-flex', alignItems: 'center', flexShrink: 0, fontSize: typography.size12, color: surface.secondaryText, whiteSpace: 'nowrap' },
  inlineAction: { paddingInline: 4, minHeight: 0, height: 'auto', fontSize: typography.size12 },
  path: { fontFamily: typography.mono, fontSize: typography.size11 },
});
