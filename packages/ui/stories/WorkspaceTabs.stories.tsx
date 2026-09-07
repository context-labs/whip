import type { Meta, StoryObj } from '@storybook/react-vite';
import { WorkspaceTabsFixture } from './WorkspaceTabs.fixture';

const meta = { title: 'WHIP/Workspace tabs', component: WorkspaceTabsFixture } satisfies Meta<typeof WorkspaceTabsFixture>;
export default meta;
export const Links: StoryObj<typeof meta> = {};
export const Buttons: StoryObj<typeof meta> = { args: { links: false } };
export const Overflow: StoryObj<typeof meta> = { args: { many: true } };
export const RightToLeft: StoryObj<typeof meta> = { args: { rtl: true } };
