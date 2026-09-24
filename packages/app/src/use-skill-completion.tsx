import { useEffect, useMemo, useRef, useState, type RefObject } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import type { WhipClient } from '@whip/sdk';
import type { HostSkillCompletionParams } from '@whip/protocol';
import { Button, useTextareaSuggestions } from '@whip/ui';
import { ErrorNotice } from './error-feedback';
import { insertSkill, skillTrigger, type SkillTrigger } from './skill-completion';

type Scope = { rootId: string; agentId: string } | { cwd: string; definition: string; permissionMode: HostSkillCompletionParams['permission_mode'] };
interface Request { scope: string; draft: string; start: number; end: number; trigger: SkillTrigger }

/** Ephemeral discovery only. Each composer retains its existing draft/selection owner. */
export function useSkillCompletion({ client, owner, scope: context, input, draft, change, connected, blocked = false, rememberSelection }: {
  client: WhipClient; owner: string; scope: Scope; input: RefObject<HTMLTextAreaElement | null>;
  draft: string; change(text: string): void; connected: boolean; blocked?: boolean;
  rememberSelection?(selection: { start: number; end: number }): void;
}) {
  // Empty New Chat cwd is a distinct host-global scope, never a fallback folder.
  const scope = 'rootId' in context || context.cwd.trim() ? context
    : { scope: 'global' as const, definition: context.definition, permissionMode: context.permissionMode };
  const global = 'scope' in scope;
  const runtimeId = client.getSnapshot().info?.runtime_id;
  // A replaced client must not reuse reads from the previous connection lifetime.
  const clientKey = useMemo(() => crypto.randomUUID(), [client]);
  const scopeKey = JSON.stringify([runtimeId, clientKey, owner, scope]);
  const latest = useRef({ scopeKey, client }); latest.current = { scopeKey, client };
  const [request, setRequest] = useState<Request | null>(null);
  useEffect(() => { setRequest(null); }, [scopeKey, blocked, connected]);
  const dismissed = useRef('');
  const composing = useRef(false);
  const suppressRepeat = useRef(false);
  const current = !blocked && request?.scope === scopeKey && request.draft === draft ? request : null;
  const open = !!current;
  const queryText = current?.trigger.prefix ?? '';
  const queryIdentity = JSON.stringify([scopeKey, queryText]);
  const capabilities = client.getSnapshot().info?.negotiated_capabilities;
  const supported = capabilities?.includes('rootId' in scope ? 'workspace_completion' : 'host_skill_completion') ?? false;
  const catalogSupported = capabilities?.includes('skill_catalog_completion') ?? false;
  const globalSupported = capabilities?.includes('host_global_skill_completion') ?? false;
  const ready = !blocked && connected && supported && (!global || globalSupported);
  const [focused, setFocused] = useState(false);
  const [warmed, setWarmed] = useState('');
  const [incomplete, setIncomplete] = useState<{ scope: string; warnings: string[] } | null>(null);
  useEffect(() => { setIncomplete(null); }, [scopeKey]);
  const fallback = !catalogSupported || incomplete?.scope === scopeKey;
  // Keep the observer after blur/Escape: zero inactive retention must not discard
  // an in-scope catalog on dismissal. Only the focused composer starts warming.
  useEffect(() => {
    if (ready && document.activeElement === input.current) setWarmed(scopeKey);
  }, [ready, focused, scopeKey, input]);
  const [debounced, setDebounced] = useState('');
  useEffect(() => {
    if (!open || !fallback) { setDebounced(''); return; }
    const timer = setTimeout(() => setDebounced(queryIdentity), 120);
    return () => clearTimeout(timer);
  }, [open, fallback, queryIdentity]);
  const enabled = ready && (fallback ? open && debounced === queryIdentity : warmed === scopeKey);
  const prefix = fallback ? queryText : '';
  const limit = fallback ? 32 : 1024;
  const queries = useQueryClient();
  const queryKey = ['slash-skills', runtimeId, owner, clientKey, scope, prefix, limit];
  const readIdentity = JSON.stringify(queryKey);
  useEffect(() => {
    if (!enabled) return;
    return () => { void queries.cancelQueries({ queryKey, exact: true }); };
  }, [queries, enabled, readIdentity]);
  const result = useQuery({
    queryKey,
    queryFn: ({ signal }) => 'rootId' in scope
      ? client.call('workspace.complete', { root_id: scope.rootId, agent_id: scope.agentId, kind: 'skill', prefix, limit }, { signal })
      : client.call('host.skills.complete', { ...('scope' in scope ? { scope: scope.scope } : { cwd: scope.cwd }), definition: scope.definition, permission_mode: scope.permissionMode, prefix, limit }, { signal }),
    enabled, gcTime: 0, retry: false, refetchOnWindowFocus: false,
  });
  useEffect(() => {
    if (!fallback && result.data?.truncated) setIncomplete({ scope: scopeKey, warnings: result.data.warnings ?? [] });
  }, [fallback, result.data, scopeKey]);
  // Only focus/open boundaries refresh stale catalogs. Typing never toggles
  // enabled or the catalog key, even after its ten-second freshness expires.
  const boundary = useRef({ open: false, focused: false });
  useEffect(() => {
    const entered = (open && !boundary.current.open) || (focused && !boundary.current.focused);
    boundary.current = { open, focused };
    if (entered && !fallback && enabled && result.data && result.isStale && !result.isFetching) void result.refetch();
  }, [open, focused]);
  const data = ready && (!fallback || enabled) && !(!fallback && result.data?.truncated) ? result.data : undefined;
  const matches = data?.candidates?.filter(candidate => candidate.text.slice(1).startsWith(queryText)) ?? [];
  const candidates = matches.slice(0, 32);
  const cold = open && ready && !data && !result.error;
  const [loading, setLoading] = useState('');
  useEffect(() => {
    setLoading('');
    if (!cold) return;
    const timer = setTimeout(() => setLoading(readIdentity), 150);
    return () => clearTimeout(timer);
  }, [cold, readIdentity]);
  function signature(value: string, start: number, end: number) { return JSON.stringify([scopeKey, value, start, end]); }
  function observe(edited = false) {
    const element = input.current;
    if (!element || document.activeElement !== element || blocked || composing.current) return;
    const { value, selectionStart: start, selectionEnd: end } = element;
    if (edited) dismissed.current = '';
    const trigger = skillTrigger(value, start, end);
    if (!trigger || dismissed.current === signature(value, start, end)) { setRequest(null); return; }
    setRequest({ scope: scopeKey, draft: value, start, end, trigger });
  }
  function dismiss() {
    const element = input.current;
    if (element) dismissed.current = signature(element.value, element.selectionStart, element.selectionEnd);
    setRequest(null);
  }
  function select(reference: string) {
    const element = input.current;
    if (!current || !element || latest.current.scopeKey !== current.scope || latest.current.client !== client ||
      element.value !== current.draft || element.selectionStart !== current.start || element.selectionEnd !== current.end ||
      !candidates.some(candidate => candidate.text === reference)) return;
    const insertion = insertSkill(element.value, current.trigger, reference);
    if (!insertion) return;
    dismiss(); suppressRepeat.current = true; change(insertion.text);
    requestAnimationFrame(() => {
      if (latest.current.scopeKey !== current.scope || input.current !== element || element.value !== insertion.text || document.activeElement !== element) return;
      element.setSelectionRange(insertion.caret, insertion.caret);
      rememberSelection?.({ start: insertion.caret, end: insertion.caret });
    });
  }
  const status = !connected ? 'Reconnect to search skills.'
    : !supported ? 'Skill suggestions require a newer host.'
    : global && !globalSupported ? 'Update this host or choose a project folder to browse skills.'
    : cold ? (loading === readIdentity ? 'Loading skills…' : '')
    : result.error ? (data ? 'Showing saved skills. Refresh failed.' : 'Could not load skills.')
    : candidates.length ? undefined
    : queryText ? 'No matching skills.' : global ? 'No global skills available.' : 'No skills available.';
  const suggestions = useTextareaSuggestions({
    input, open, queryKey: queryIdentity, label: 'Skills', status,
    options: candidates.map(candidate => ({ value: candidate.text, label: '/' + candidate.text.slice(1), description: candidate.description })),
    detail: [...(incomplete?.scope === scopeKey ? incomplete.warnings : []), ...(data?.warnings ?? [])].filter(Boolean).join(' '),
    feedback: ready && result.error ? <ErrorNotice type="resource" owner={`${owner}:slash-skills`} title={data ? 'Could not refresh skills' : 'Could not load skills'} error={result.error}
      action={<Button variant="ghost" onClick={() => { input.current?.focus({ preventScroll: true }); void result.refetch(); }}>Retry</Button>} /> : undefined,
    onSelect: select, onDismiss: dismiss,
  });
  return {
    popup: suggestions.popup,
    dismiss,
    onChange: () => observe(true),
    onSelect: () => observe(),
    inputProps: {
      ...suggestions.inputProps,
      onFocus: () => setFocused(true),
      onBlur: (event: React.FocusEvent<HTMLTextAreaElement>) => { setFocused(false); suggestions.inputProps.onBlur(event); },
      onCompositionStart: () => { composing.current = true; suggestions.inputProps.onCompositionStart(); dismiss(); },
      onCompositionEnd: () => { composing.current = false; suggestions.inputProps.onCompositionEnd(); observe(true); },
    },
    onKeyDown: (event: React.KeyboardEvent<HTMLTextAreaElement>) => {
      if (event.key === 'Enter' && event.repeat && suppressRepeat.current) { event.preventDefault(); return true; }
      if (!event.repeat) suppressRepeat.current = false;
      return suggestions.onKeyDown(event);
    },
  };
}
