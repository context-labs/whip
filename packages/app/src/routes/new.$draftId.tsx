import { createFileRoute } from '@tanstack/react-router';
import { EmptyWorkspace } from '../empty-workspace';
export const Route = createFileRoute('/new/$draftId')({ component: () => <EmptyWorkspace missing /> });
