import { createRootRouteWithContext, Outlet } from '@tanstack/react-router';
import type { AppRuntime } from '../runtime';
import { Alert, Button } from '@whip/ui';
import { ErrorNotice } from '../error-feedback';
import { AppShell } from '../shell';

export const Route = createRootRouteWithContext<{ runtime: AppRuntime }>()({
  component: () => (
    <AppShell>
      <Outlet />
    </AppShell>
  ),
  errorComponent: ({ error, reset }) => (
    <ErrorNotice type="application" owner="application" title="This view could not load" error={error}
      action={<Button onClick={reset}>Try again</Button>} />
  ),
  notFoundComponent: () => (
    <Alert title="Page not found">
      Use the session navigation to continue.
    </Alert>
  ),
});
