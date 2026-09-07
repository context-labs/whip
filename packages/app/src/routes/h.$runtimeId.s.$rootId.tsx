import { createFileRoute } from '@tanstack/react-router';
import { isInspectorSection, type InspectorSection } from '../navigation';
import { ConversationRoute } from '../conversation';
import { Alert, Button } from '@whip/ui';
export const Route = createFileRoute('/h/$runtimeId/s/$rootId')({
  validateSearch: (search: Record<string, unknown>): { agent?: string; panel?: InspectorSection } => ({ ...(typeof search.agent === 'string' && search.agent ? { agent: search.agent } : {}), ...(isInspectorSection(search.panel) ? { panel: search.panel } : {}) }),
  component: function SessionPage() { const { rootId, runtimeId } = Route.useParams(); const { agent, panel } = Route.useSearch(); return <ConversationRoute rootId={rootId} runtimeId={runtimeId} agentId={agent ?? rootId} panel={panel} />; },
  errorComponent: ({ error, reset }) => <Alert tone="error" title="This session could not load"><p>{error.message}</p><Button onClick={reset}>Try again</Button></Alert>,
});
