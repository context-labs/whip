import { createFileRoute } from '@tanstack/react-router';
import { validateSessionSearch } from '../session-tabs';
import { ConversationRoute } from '../conversation';
import { Alert, Button } from '@whip/ui';
export const Route = createFileRoute('/h/$runtimeId/s/$rootId')({
  validateSearch: validateSessionSearch,
  component: function SessionPage() { const { rootId, runtimeId } = Route.useParams(); return <ConversationRoute rootId={rootId} runtimeId={runtimeId} />; },
  errorComponent: ({ error, reset }) => <Alert tone="error" title="This session could not load"><p>{error.message}</p><Button onClick={reset}>Try again</Button></Alert>,
});
