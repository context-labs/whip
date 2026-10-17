import type { Meta, StoryObj } from '@storybook/react-vite';
import { ScrollbarsExample } from './Scrollbars.fixture';

const meta = { title: 'WHIP/Scrollbars', component: ScrollbarsExample, parameters: { layout: 'fullscreen' } } satisfies Meta<typeof ScrollbarsExample>;
export default meta;
export const Native: StoryObj<typeof meta> = {};
