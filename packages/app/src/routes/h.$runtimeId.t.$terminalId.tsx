import { createFileRoute } from '@tanstack/react-router';
import { EmptyWorkspace } from '../empty-workspace';
/** Terminal tabs render inside the workspace; this route body only shows when no open tab matches the URL. */
export const Route = createFileRoute('/h/$runtimeId/t/$terminalId')({ component: () => <EmptyWorkspace missing subject="terminal" /> });
