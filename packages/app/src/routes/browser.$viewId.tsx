import { createFileRoute } from '@tanstack/react-router';
import { EmptyWorkspace } from '../empty-workspace';
import { useRuntime, useSessionTabs } from '../context';
export const Route = createFileRoute('/browser/$viewId')({ component: function BrowserPage() {
  const { viewId } = Route.useParams();
  const runtime = useRuntime(); useSessionTabs();
  if (runtime.tabs.workspace().tabs.some(tab => tab.kind === 'browser' && tab.id === viewId)) return null;
  return <EmptyWorkspace missing subject="browser"/>;
} });
