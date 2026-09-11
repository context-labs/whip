import { createFileRoute } from '@tanstack/react-router';
import { validateSessionSearch } from '../session-tabs';
import { ConversationRoute } from '../conversation';
import { ErrorNotice } from '../error-feedback';
import { Button } from '@whip/ui';
export const Route = createFileRoute('/h/$runtimeId/s/$rootId')({
  validateSearch: validateSessionSearch,
  component: function SessionPage() { const { rootId, runtimeId } = Route.useParams(); return <ConversationRoute rootId={rootId} runtimeId={runtimeId} />; },
  errorComponent: ({ error, reset }) => <ErrorNotice type="session" owner="session-route" error={error} action={<Button onClick={reset}>Try again</Button>} />,
});
