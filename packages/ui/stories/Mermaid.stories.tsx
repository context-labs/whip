import type {Meta, StoryObj} from '@storybook/react-vite';
import {useState} from 'react';
import {Button, MermaidBlock, Stack, useTheme} from '../src';

const meta = {title: 'WHIP/Mermaid', component: MermaidBlock} satisfies Meta<typeof MermaidBlock>;
export default meta;
type Story = StoryObj<typeof meta>;
const flow = 'flowchart TD\n  Root[Root agent] --> Worker[Tool worker]\n  Worker --> Mailbox[Mailbox]\n  Mailbox --> Root';
export const Flowchart: Story = {args: {code: flow}};
export const Sequence: Story = {args: {code: 'sequenceDiagram\n  participant User\n  participant Root\n  participant Worker\n  User->>Root: Inspect workspace\n  Root->>Worker: Read files\n  Worker-->>Root: Evidence\n  Root-->>User: Findings'}};
export const State: Story = {args: {code: 'stateDiagram-v2\n  [*] --> Idle\n  Idle --> Running\n  Running --> Waiting\n  Waiting --> Running\n  Running --> Complete\n  Complete --> [*]'}};
export const Class: Story = {args: {code: 'classDiagram\n  class Agent {\n    +String name\n    +run()\n  }\n  class Mailbox {\n    +send()\n  }\n  Agent --> Mailbox'}};
export const EntityRelationship: Story = {args: {code: 'erDiagram\n  ROOT ||--o{ AGENT : owns\n  AGENT ||--o{ MESSAGE : sends'}};
export const XYChart: Story = {args: {code: 'xychart-beta\n  x-axis [Read, Render, Report]\n  y-axis "Tasks" 0 --> 10\n  bar [8, 5, 3]'}};
export const Unsupported: Story = {args: {code: 'pie title Tool calls\n  "Read" : 8\n  "Write" : 2'}};
export const Invalid: Story = {args: {code: 'flowchart TD\n  A[An unfinished node'}};
export const Truncated: Story = {args: {code: flow, truncated: true}};
export const Wide: Story = {args: {code: 'flowchart LR\n  Root[Root agent] --> Inspect[Inspect workspace] --> Delegate[Delegate research] --> Review[Review evidence] --> Test[Run checks] --> Report[Report to user]'}};
export const NonLatin: Story = {args: {code: 'flowchart TD\n  A[読み取り] --> B[分析]\n  B --> C[Résultat · Ελληνικά]'}};
function LiveExample() {
  const [live, setLive] = useState(true);
  return <Stack><Button onClick={() => setLive(value => !value)}>{live ? 'Finish response' : 'Resume response'}</Button><MermaidBlock code={flow} live={live}/></Stack>;
}
export const Live: Story = {args: {code: flow}, render: () => <LiveExample/>};
function AppearanceExample() {
  const {theme, setTheme, display, setDisplay} = useTheme();
  return <Stack>
    <Button onClick={() => setTheme(theme === 'dark' ? 'light' : 'dark')}>Switch theme</Button>
    <Button onClick={() => setDisplay({uiFont: display.uiFont === 'inter' ? 'system' : 'inter'})}>Switch diagram font</Button>
    <Button onClick={() => setDisplay({uiSize: display.uiSize === 13 ? 20 : 13})}>Switch diagram size</Button>
    <MermaidBlock code={flow}/>
  </Stack>;
}
export const Appearance: Story = {args: {code: flow}, render: () => <AppearanceExample/>};
