import type { Meta, StoryObj } from '@storybook/react-vite';
import { WorkspaceLayoutFixture } from './WorkspaceLayout.fixture';

const meta = { title: 'Workspace/Layout', component: WorkspaceLayoutFixture, parameters: { layout: 'fullscreen' } } satisfies Meta<typeof WorkspaceLayoutFixture>;
export default meta;
type Story = StoryObj<typeof meta>;
export const NestedPanes: Story = {};
