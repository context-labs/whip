import { createFileRoute } from '@tanstack/react-router';
import { isInspectorSection, type InspectorSection } from '../navigation';
import { ConversationRoute } from '../conversation';
export const Route = createFileRoute('/h/$runtimeId/s/$rootId')({
  validateSearch: (search: Record<string, unknown>): { agent?: string; panel?: InspectorSection } => ({ ...(typeof search.agent === 'string' && search.agent ? { agent: search.agent } : {}), ...(isInspectorSection(search.panel) ? { panel: search.panel } : {}) }),
  component: function SessionPage() { const { rootId, runtimeId } = Route.useParams(); const { agent, panel } = Route.useSearch(); return <ConversationRoute rootId={rootId} runtimeId={runtimeId} agentId={agent ?? rootId} panel={panel} />; },
});
