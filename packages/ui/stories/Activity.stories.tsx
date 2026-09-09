import type { Meta, StoryObj } from '@storybook/react-vite';
import * as stylex from '@stylexjs/stylex';
import { ActivityIndicator, Button, Stack, useTheme } from '../src';

const styles = stylex.create({ row: { flexDirection: 'row', alignItems: 'center' } });

function Example() {
  const { display, setDisplay } = useTheme();
  return <Stack>
    <Stack xstyle={styles.row}><ActivityIndicator active reduceMotion={display.motion === 'reduce'} /><span>Reading files</span></Stack>
    <Button variant="ghost" onClick={() => setDisplay({ motion: display.motion === 'reduce' ? 'system' : 'reduce' })}>
      {display.motion === 'reduce' ? 'Use system motion' : 'Pause activity animation'}
    </Button>
    <Stack xstyle={styles.row}><ActivityIndicator active={false} /><span>Updates paused</span></Stack>
  </Stack>;
}
const meta = { title: 'WHIP/Activity', parameters: { layout: 'padded' } } satisfies Meta;
export default meta;
type Story = StoryObj<typeof meta>;
export const LiveStatus: Story = { render: () => <Example /> };
