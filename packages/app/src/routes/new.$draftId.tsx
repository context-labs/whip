import { createFileRoute } from '@tanstack/react-router';
import { EmptyWorkspace } from '../empty-workspace';
import { useRuntime, useSessionTabs } from '../context';
/** Draft tabs render inside the workspace; this body only shows when no open tab holds the draft. */
export const Route = createFileRoute('/new/$draftId')({ component: function DraftPage() {
  const { draftId } = Route.useParams();
  const runtime = useRuntime();
  useSessionTabs();
  if (runtime.tabs.workspace().tabs.some(tab => tab.id === draftId)) return null;
  return <EmptyWorkspace missing />;
} });
