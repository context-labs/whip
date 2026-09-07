import { createRootRouteWithContext, Outlet } from '@tanstack/react-router';
import type { AppRuntime } from '../runtime';
import { Alert, Button } from '@whip/ui';
import { AppShell } from '../shell';

export const Route = createRootRouteWithContext<{ runtime: AppRuntime }>()({
  component: () => (
    <AppShell>
      <Outlet />
    </AppShell>
  ),
  errorComponent: ({ error, reset }) => (
    <Alert tone="error" title="This view could not load">
      <p>{error.message}</p>
      <Button onClick={reset}>Try again</Button>
    </Alert>
  ),
  notFoundComponent: () => (
    <Alert title="Page not found">
      Use the session navigation to continue.
    </Alert>
  ),
});
