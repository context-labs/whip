import type { Meta, StoryObj } from '@storybook/react-vite';
import { SiteHeader } from '../navigation/SiteHeader';

const meta = { title: 'Docs/Header', component: SiteHeader, parameters: { layout: 'fullscreen' } } satisfies Meta<typeof SiteHeader>;
export default meta;
type Story = StoryObj<typeof meta>;
export const Dark: Story = { globals: { theme: 'dark' } };
export const Light: Story = { globals: { theme: 'light' } };
