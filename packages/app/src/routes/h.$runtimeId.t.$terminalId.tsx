import { createFileRoute } from '@tanstack/react-router';
import { EmptyWorkspace } from '../empty-workspace';
import { useRuntime, useSessionTabs } from '../context';
/** Terminal tabs render inside the workspace; this route body only shows when no open tab matches the URL. */
export const Route = createFileRoute('/h/$runtimeId/t/$terminalId')({ component: function TerminalPage() {
  const { runtimeId, terminalId } = Route.useParams();
  const runtime = useRuntime();
  useSessionTabs();
  if (runtime.tabs.workspace().tabs.some(tab => tab.kind === 'terminal' && tab.runtimeId === runtimeId && tab.terminalId === terminalId)) return null;
  return <EmptyWorkspace missing subject="terminal" />;
} });
