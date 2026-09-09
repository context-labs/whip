import { createFileRoute } from '@tanstack/react-router';
import { Settings } from '../settings';
import { validateSettingsSearch } from '../settings/navigation';
export const Route = createFileRoute('/settings')({
  validateSearch: validateSettingsSearch,
  component: () => <Settings {...Route.useSearch()} />,
});
