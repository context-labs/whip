import { createFileRoute } from '@tanstack/react-router';
import { newSessionSearch } from '../sidebar-state';
import { Welcome } from '../welcome';
export const Route = createFileRoute('/')({ validateSearch: newSessionSearch, component: Welcome });
