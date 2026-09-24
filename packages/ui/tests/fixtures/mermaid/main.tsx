import {StrictMode, useEffect, useState} from 'react';
import {createRoot} from 'react-dom/client';
import {Button, CodeBlock, MermaidBlock, ThemeProvider, UIProvider, useTheme} from '@whip/ui';
import '@whip/ui/reset.css';
import '@whip/ui/fonts.css';

const params = new URLSearchParams(location.search);
const code = 'flowchart TD\n  Root[Root agent] --> Worker[Tool worker]\n  Worker --> Mailbox[Mailbox]\n  Mailbox --> Root';
declare global {interface Window {settleMermaid(): void; setMermaidSource(code: string): void; copiedMermaid?: string}}
function Fixture() {
  const {theme, setTheme, previewTheme, cancelPreview, setDisplay} = useTheme();
  const [live, setLive] = useState(params.has('live'));
  const [mounted, setMounted] = useState(!params.has('ordinary'));
  const [source, setSource] = useState(params.has('unsupported') ? 'pie title Tools\n  "Read" : 8'
    : params.has('invalid') ? 'flowchart TD\n  A[Unfinished node'
    : params.has('unsafe') ? 'flowchart TD\n  A --> B\n  click A "https://example.com"'
    : params.has('large') ? 'flowchart TD\n' + 'a'.repeat(33000)
    : params.has('sequence') ? 'sequenceDiagram\n  participant User\n  participant Root\n  User->>Root: Inspect workspace\n  Root-->>User: Findings'
    : params.has('state') ? 'stateDiagram-v2\n  [*] --> Idle\n  Idle --> Running\n  Running --> Complete\n  Complete --> [*]'
    : params.has('class') ? 'classDiagram\n  class Agent {\n    +String name\n    +run()\n  }\n  Agent --> Mailbox'
    : params.has('er') ? 'erDiagram\n  ROOT ||--o{ AGENT : owns\n  AGENT ||--o{ MESSAGE : sends'
    : params.has('xychart') ? 'xychart-beta\n  x-axis [Read, Render, Report]\n  y-axis "Tasks" 0 --> 10\n  bar [8, 5, 3]'
    : params.has('nonlatin') ? 'flowchart TD\n  A[読み取り] --> B[分析]\n  B --> C[Résultat · Ελληνικά]'
    : params.has('wide') ? 'flowchart LR\n  Root[Root agent] --> Inspect[Inspect workspace] --> Delegate[Delegate research] --> Review[Review evidence] --> Test[Run checks] --> Report[Report to user]'
    : code);
  useEffect(() => {window.settleMermaid = () => setLive(false); window.setMermaidSource = setSource;}, []);
  return <main><h1>Conversation diagrams</h1>
    <Button onClick={() => setMounted(value => !value)}>{mounted ? 'Unmount diagram' : 'Mount diagram'}</Button>
    <Button onClick={() => setTheme(theme === 'dark' ? 'light' : 'dark')}>Switch theme</Button>
    <Button onClick={() => previewTheme('claude-code')}>Preview theme</Button>
    <Button onClick={cancelPreview}>Cancel preview</Button>
    <Button onClick={() => setDisplay({uiFont: 'system'})}>System diagram font</Button>
    <Button onClick={() => setDisplay({uiFont: 'system', uiSize: 20})}>Larger system font</Button>
    <Button onClick={() => setDisplay({contrast: 'more', motion: 'reduce'})}>Increase contrast</Button>
    {mounted && <MermaidBlock code={source} live={live} truncated={params.has('truncated')} renderText={params.has('decorate') ? text => <mark>{text}</mark> : undefined}/>}
    {params.has('ordinary') && <CodeBlock code="Source remains plain text." label="Ordinary code"/>}
  </main>;
}
createRoot(document.getElementById('root')!).render(<StrictMode><ThemeProvider initialTheme={params.get('theme') ?? 'dark'}><UIProvider copy={async text => {window.copiedMermaid = text;}}><Fixture/></UIProvider></ThemeProvider></StrictMode>);
