import { useEffect, useRef, useState, useSyncExternalStore } from 'react';
import { useLocation, useNavigate } from '@tanstack/react-router';
import { Button } from '@whip/ui';
import * as stylex from '@stylexjs/stylex';
import { useAppState, useRuntime, useSessionTabs } from './context';
import { ErrorNotice } from './error-feedback';
import { tabDestination } from './session-tab-routing';
import { selectedSessionTab } from './session-tabs';
import { layout } from './styles';
import type { WelcomeSubmission } from './welcome-submission';

/** Unresolved first messages remain reachable even after closed-history eviction. */
export function WelcomeRecovery({ currentId }: { currentId?: string }) {
  const runtime = useRuntime();
  const navigate = useNavigate();
  const location = useLocation({ select: location => location });
  const scope = `${currentId ?? 'home'}:${location.href}:${location.state.__TSR_key}`;
  const currentScope = useRef(scope);
  currentScope.current = scope;
  const request = useRef<symbol | undefined>(undefined);
  const [pending, setPending] = useState<{ scope: string; id: string }>();
  const [failure, setFailure] = useState<{ scope: string; id: string; error: unknown }>();
  const [, retryRead] = useState(0);
  useEffect(() => () => { request.current = undefined; }, []);
  useSyncExternalStore(runtime.welcome.subscribe, runtime.welcome.getSnapshot);
  useSessionTabs();
  const { hosts } = useAppState();
  let items: readonly WelcomeSubmission[] = [];
  let drafts: readonly string[] = [];
  try {
    items = runtime.welcome.list().filter(item => item.draftId !== currentId);
    drafts = runtime.orphanWelcomeDrafts().filter(id => id !== currentId && !items.some(item => item.draftId === id));
  } catch (error) {
    return <ErrorNotice type="resource" owner="first-message-recovery" title="Could not load saved first messages" error={error}
      action={<Button variant="ghost" onClick={() => retryRead(value => value + 1)}>Retry</Button>} />;
  }
  const activeFailure = failure?.scope === scope ? failure : undefined;
  const busy = pending?.scope === scope;
  if (!items.length && !drafts.length && !activeFailure && !busy) return null;
  async function recover(id: string) {
    if (busy) return;
    const token = Symbol();
    request.current = token;
    const selected = selectedSessionTab(runtime.tabs.workspace())?.id;
    let activatedId: string | undefined;
    const current = () => request.current === token && currentScope.current === scope
      && selectedSessionTab(runtime.tabs.workspace())?.id === (activatedId ?? selected);
    setPending({ scope, id }); setFailure(undefined);
    try {
      let tab = runtime.recoverWelcome(id);
      if (runtime.welcome.get(id)?.state === 'accepted') {
        await runtime.welcome.completeAccepted(id);
        if (!current()) return;
        const workspace = runtime.tabs.workspace();
        tab = workspace.tabs.find(item => item.id === id) ?? workspace.closed.find(item => item.tab.id === id)?.tab ?? tab;
      }
      if (!current()) return;
      if (!runtime.tabs.workspace().tabs.some(open => open.id === tab.id)) runtime.tabs.reopenView(tab.id);
      runtime.tabs.activate(tab.id);
      activatedId = tab.id;
      await navigate(tabDestination(tab));
    } catch (error) {
      if (current()) setFailure({ scope, id, error });
    } finally {
      if (request.current === token) { request.current = undefined; setPending(undefined); }
    }
  }
  const feedback = (id: string) => activeFailure?.id === id && <ErrorNotice type="action" owner={`recover:${id}`}
    title="Could not reopen this saved message" error={activeFailure.error} />;
  return <section aria-label="First-message recovery" {...stylex.props(layout.column)}>
    <p>Unfinished first messages are saved. Reopen one to check its original request; nothing is sent automatically.</p>
    {items.map(item => <div key={item.draftId}>
      <Button variant="ghost" disabled={busy} onClick={() => void recover(item.draftId)}>Recover first message · {hosts.find(host => host.runtimeId === item.create.runtimeId)?.name ?? item.create.runtimeId} · {item.params.cwd}</Button>
      {feedback(item.draftId)}
    </div>)}
    {drafts.map(id => <div key={id}>
      <Button variant="ghost" disabled={busy} onClick={() => void recover(id)}>Recover saved draft · {id}</Button>
      {feedback(id)}
    </div>)}
    {activeFailure && !items.some(item => item.draftId === activeFailure.id) && !drafts.includes(activeFailure.id) && <div>
      {feedback(activeFailure.id)}
      <Button variant="ghost" disabled={busy} onClick={() => void recover(activeFailure.id)}>Retry reopening saved message</Button>
    </div>}
    {busy && <p role="status">Reopening saved message…</p>}
  </section>;
}
