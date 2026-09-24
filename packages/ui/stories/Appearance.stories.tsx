import type {Meta, StoryObj} from '@storybook/react-vite';
import {useState} from 'react';
import {Button, CodeBlock, Dialog, NumberField, Slider, Stack, Switch, ThemePicker, useTheme} from '../src';

function DisplayControls() {
  const {display, setDisplay, resetAppearance, notice} = useTheme();
  const [density, setDensity] = useState(0);
  const [open, setOpen] = useState(false);
  const values = ['Compact', 'Comfortable', 'Detailed'];
  return <Stack>
    <ThemePicker presentation="popover"/>
    <Slider label="Tool call density" value={density} onValueChange={setDensity} min={0} max={2} getValueText={value => values[value]!}/>
    <span>{values[density]}</span>
    <NumberField label="UI font size" format={{maximumFractionDigits: 0}} value={display.uiSize} min={12} max={20} onValueChange={value => {if (value !== null) setDisplay({uiSize: value});}}/>
    <NumberField label="Code font size" format={{maximumFractionDigits: 0}} value={display.codeSize} min={10} max={24} onValueChange={value => {if (value !== null) setDisplay({codeSize: value});}}/>
    <Switch label="Wrap code" checked={display.wrapCode} onCheckedChange={wrapCode => setDisplay({wrapCode})}/>
    <Button onClick={() => setDisplay({uiFont: 'system', codeFont: 'system', contrast: 'more', motion: 'reduce'})}>Use accessible system presentation</Button>
    <Button onClick={resetAppearance}>Reset appearance</Button>
    <Button onClick={() => setOpen(true)}>Open appearance portal</Button>
    <CodeBlock label="Appearance code" language="starlark" code={'def inspect(name):\n    return agents.get(name="an unusually long but bounded sample name for wrapping in a narrow reading column", limit=12)\n'}/>
    <Dialog open={open} onOpenChange={setOpen} title="Appearance portal"><Button>Inherited type</Button><CodeBlock code="return True"/></Dialog>
    {notice && <p role="status">{notice}</p>}
  </Stack>;
}
const meta = {title: 'WHIP/Appearance', parameters: {layout: 'padded'}} satisfies Meta;
export default meta;
type Story = StoryObj<typeof meta>;
export const DisplayPreferences: Story = {render: () => <DisplayControls/>};
