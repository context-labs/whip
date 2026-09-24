import type { Meta, StoryObj } from '@storybook/react-vite';
import * as stylex from '@stylexjs/stylex';
import { useMemo, useRef, useState } from 'react';
import { Button, NativeSurfaceProvider, Textarea, useTextareaSuggestions } from '../src';

const options = [
  { value: '$ponytail', label: '/ponytail', description: 'Write the least code that works.' },
  { value: '$frontend-design', label: '/frontend-design', description: 'Intentional visual design inside the existing product.' },
  { value: '$testing', label: '/testing', description: 'Small, repeatable checks for important behavior.' },
];
const manyOptions = Array.from({ length: 32 }, (_, index) => ({ value: `$skill-${index}`, label: `/skill-${index}`, description: 'A bounded host skill suggestion.' }));
function Example({ empty = false, long = false }: { empty?: boolean; long?: boolean }) {
  const input = useRef<HTMLTextAreaElement>(null);
  const [value, setValue] = useState('');
  const [open, setOpen] = useState(false);
  const [sent, setSent] = useState(0);
  const suggestions = useTextareaSuggestions({ input, open, options: empty ? [] : long ? manyOptions : options, queryKey: value,
    label: 'Skills', status: empty ? 'No matching skills.' : '3 skills. Selection inserts a skill reference.',
    onDismiss: () => setOpen(false), onSelect: text => {
      setValue(text + ' '); setOpen(false);
      requestAnimationFrame(() => { input.current?.focus(); input.current?.setSelectionRange(text.length + 1, text.length + 1); });
    },
  });
  return <form {...stylex.props(styles.form)} onSubmit={event => { event.preventDefault(); setSent(sent + 1); }}>
    <Textarea ref={input} {...suggestions.inputProps} aria-label="Message" rows={4} value={value}
      onChange={event => { setValue(event.target.value); setOpen(event.target.value.includes('/')); }}
      onKeyDown={event => { if (!suggestions.onKeyDown(event) && event.key === 'Enter' && !event.shiftKey) { event.preventDefault(); setSent(sent + 1); } }} />
    {suggestions.popup}
    <Button type="submit">Send</Button><output aria-label="Send count">{sent}</output>
    <Button onClick={() => setOpen(true)}>Browse suggestions</Button>
  </form>;
}
function NativeExample() {
  const [released, setReleased] = useState(0);
  const acquire = useMemo(() => () => ({
    // Intentionally resolves even after release: exercise the late native ACK race.
    ready: new Promise<void>(resolve => setTimeout(resolve, 450)),
    release: () => setReleased(count => count + 1),
  }), []);
  return <NativeSurfaceProvider acquire={acquire}><Example /><output aria-label="Native holds released">{released}</output></NativeSurfaceProvider>;
}
const styles = stylex.create({ form: { marginTop: 320, maxWidth: 560, display: 'grid', gap: 8 } });
const meta = { title: 'WHIP/Textarea suggestions', parameters: { layout: 'padded' } } satisfies Meta;
export default meta;
type Story = StoryObj<typeof meta>;
export const Multiline: Story = { render: () => <Example /> };
export const Empty: Story = { render: () => <Example empty /> };
export const LongList: Story = { render: () => <Example long /> };
export const NativeHold: Story = { render: () => <NativeExample /> };
