import { useSyncExternalStore } from 'react';
import { useNavigate } from '@tanstack/react-router';
import { Alert, Button } from '@whip/ui';
import * as stylex from '@stylexjs/stylex';
import { useAppState, useRuntime, useSessionTabs } from './context';
import { tabDestination } from './session-tab-routing';
import { errorMessage } from './platform';
import { layout } from './styles';
import type { WelcomeSubmission } from './welcome-submission';

/** Unresolved first messages remain reachable even after closed-history eviction. */
export function WelcomeRecovery({ currentId }: { currentId?: string }) {
  const runtime = useRuntime();
  const navigate = useNavigate();
  useSyncExternalStore(runtime.welcome.subscribe, runtime.welcome.getSnapshot);
  useSessionTabs();
  const { hosts } = useAppState();
  let items: readonly WelcomeSubmission[] = [];
  let drafts: readonly string[] = [];
  try {
    items = runtime.welcome.list().filter(item => item.draftId !== currentId);
    drafts = runtime.orphanWelcomeDrafts().filter(id => id !== currentId && !items.some(item => item.draftId === id));
  }
  catch (error) { return <Alert tone="error">{errorMessage(error)}</Alert>; }
  if (!items.length && !drafts.length) return null;
  async function recover(id: string) {
    try {
      let tab = runtime.recoverWelcome(id);
      if (runtime.welcome.get(id)?.state === 'accepted') {
        await runtime.welcome.completeAccepted(id);
        const workspace = runtime.tabs.workspace();
        tab = workspace.tabs.find(item => item.id === id) ?? workspace.closed.find(item => item.tab.id === id)?.tab ?? tab;
      }
      if (!runtime.tabs.workspace().tabs.some(open => open.id === tab.id)) runtime.tabs.reopenView(tab.id);
      runtime.tabs.activate(tab.id);
      void navigate(tabDestination(tab)).catch(error => runtime.report(error));
    } catch (error) { runtime.report(error); }
  }
  return <section aria-label="First-message recovery" {...stylex.props(layout.column)}>
    <p>Unfinished first messages are saved. Reopen one to check its original request; nothing is sent automatically.</p>
    {items.map(item => <Button key={item.draftId} variant="ghost" onClick={() => recover(item.draftId)}>Recover first message · {hosts.find(host => host.runtimeId === item.create.runtimeId)?.name ?? item.create.runtimeId} · {item.params.cwd}</Button>)}
    {drafts.map(id => <Button key={id} variant="ghost" onClick={() => recover(id)}>Recover saved draft · {id}</Button>)}
  </section>;
}
