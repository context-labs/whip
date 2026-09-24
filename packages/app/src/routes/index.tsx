import { createFileRoute } from '@tanstack/react-router';
import { newSessionSearch } from '../sidebar-state';
import { EmptyWorkspace } from '../empty-workspace';
export const Route = createFileRoute('/')({ validateSearch: newSessionSearch, component: EmptyWorkspace });
