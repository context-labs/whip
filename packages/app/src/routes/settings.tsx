import { createFileRoute } from '@tanstack/react-router';
import { Settings } from '../settings';
export const Route = createFileRoute('/settings')({
  validateSearch: (search: Record<string, unknown>): { section?: string } => ({ ...(typeof search.section === 'string' && ['appearance', 'providers', 'runtime', 'device', 'recovery'].includes(search.section) ? { section: search.section } : {}) }),
  component: () => <Settings section={Route.useSearch().section} />,
});
