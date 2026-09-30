import type { Meta, StoryObj } from '@storybook/react-vite';
import * as stylex from '@stylexjs/stylex';
import { ActivityIndicator, Stack } from '../src';

const styles = stylex.create({ row: { flexDirection: 'row', alignItems: 'center' } });

function Example() {
  return <Stack>
    <Stack xstyle={styles.row}><ActivityIndicator active /><span>Reading files</span></Stack>
    <Stack xstyle={styles.row}><ActivityIndicator active reduceMotion /><span>Reduced motion</span></Stack>
    <Stack xstyle={styles.row}><ActivityIndicator active={false} /><span>Updates paused</span></Stack>
  </Stack>;
}
const meta = { title: 'WHIP/Activity', parameters: { layout: 'padded' } } satisfies Meta;
export default meta;
type Story = StoryObj<typeof meta>;
export const LiveStatus: Story = { render: () => <Example /> };
