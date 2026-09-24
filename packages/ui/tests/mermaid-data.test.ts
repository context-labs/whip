import {test} from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import {parseMermaid, renderMermaidSVG} from 'beautiful-mermaid';
import {MermaidRenderError, mermaidLimits, prepareMermaidSVG, validateMermaid, validateMermaidPalette} from '../src/mermaid-data.ts';

const palette = {background: '#ffffff', foreground: '#202020', line: '#555555', accent: '#0055aa', muted: '#555555', surface: '#eeeeee', border: '#555555', font: 'inter' as const, fontSize: 13};
const fixtures = {
  flowchart: 'flowchart TD\n Root[Root agent] -->|inspect| Worker[Tool worker]\n Worker --> Done((Complete))',
  state: 'stateDiagram-v2\n state "Working hard" as Working\n [*] --> Working\n Working --> Done : finish\n Done --> [*]',
  sequence: 'sequenceDiagram\n participant A as Alice\n participant B as Bob\n loop Every item\n A->>B: Inspect\n B-->>A: Result\n end\n Note over A,B: Complete',
  class: 'classDiagram\n class Animal {\n +String name\n +run() void\n }\n Animal <|-- Dog',
  er: 'erDiagram\n CUSTOMER ||--o{ ORDER : places\n CUSTOMER {\n string name PK\n }',
  xychart: 'xychart-beta\n title "Sales"\n x-axis [Jan, Feb, Mar]\n y-axis "Revenue" 0 --> 100\n bar [10, 20, 80]',
};
const font = readFileSync(new URL('../../../node_modules/@fontsource-variable/inter/files/inter-latin-wght-normal.woff2', import.meta.url)).toString('base64');
const css = `@font-face{font-family:'Whip Diagram Inter';src:url(data:font/woff2;base64,${font}) format('woff2');font-weight:100 900;}`;

for (const [type, source] of Object.entries(fixtures)) test(`reviewed ${type} fixture renders an inert, bounded image`, () => {
  assert.deepEqual(validateMermaid(source), {ok: true, type});
  const svg = renderMermaidSVG(source, {bg: palette.background, fg: palette.foreground, accent: palette.accent, font: 'Whip Diagram Inter'});
  assert.match(svg, /@import/);
  const image = prepareMermaidSVG(svg, palette, css);
  assert.ok(image.width > 0 && image.height > 0);
  assert.ok(image.svg.length < mermaidLimits.outputBytes);
  assert.doesNotMatch(image.svg, /@import|https:\/\/fonts|foreignObject|<script|\son\w+=|\b(?:href|src)=/i);
  assert.match(image.svg, /data:font\/woff2;base64/);
  assert.match(image.svg, /--_text-faint:var\(--muted\)/);
  assert.match(image.svg, /--_inner-stroke:var\(--border\)/);
  assert.match(image.svg, /--_group-hdr:var\(--surface\)/);
  assert.match(image.svg, /--_key-badge:var\(--surface\)/);
  const system = prepareMermaidSVG(svg, {...palette, font: 'system', fontSize: 20}, css);
  assert.doesNotMatch(system.svg, /@font-face|JetBrains|Inter/);
  assert.ok(Math.abs(system.width / image.width - 20 / 13) < 1e-12);
});

test('accepted flow arrows preserve endpoints, marker semantics, styles and labels', () => {
  for (const arrow of ['-->', '---', '-.->', '-.-', '==>', '===', '<-->', '<-.->', '<==>']) {
    const source = `flowchart LR\n A[Alpha] ${arrow}|result| B{Beta}`;
    assert.equal(validateMermaid(source).ok, true, arrow);
    const graph = parseMermaid(source);
    assert.equal(graph.nodes.size, 2);
    assert.equal(graph.edges.length, 1);
    assert.equal(graph.edges[0]!.source, 'A');
    assert.equal(graph.edges[0]!.target, 'B');
    assert.equal(graph.edges[0]!.label, 'result');
    assert.equal(graph.edges[0]!.hasArrowStart, arrow.startsWith('<'));
    assert.equal(graph.edges[0]!.hasArrowEnd, arrow.endsWith('>'));
  }
});
test('state directions and descriptions are not silently discarded', () => {
  for (const direction of ['LR', 'RL', 'TB', 'BT', 'TD']) {
    const source = `stateDiagram-v2\n direction ${direction}\n A : Ready\n A --> B`;
    assert.equal(validateMermaid(source).ok, true);
    assert.equal(parseMermaid(source).direction, direction);
    assert.equal(parseMermaid(source).nodes.get('A')?.label, 'Ready');
  }
  assert.equal(validateMermaid('stateDiagram-v2\n A --> B\n A : Ready').ok, false);
});
test('upstream permissive parsing is fenced off instead of presenting partial diagrams', () => {
  const unsupported = [
    'flowchart TD\n A --> B\n this is silently ignored', 'flowchart TD\n A --> B garbage', 'flowchart TD\n A --> B\n A[Late label]', 'flowchart TD\n A{First} --> B\n A[Changed]',
    'flowchart TD\n A --> B; B --> C', 'flowchart TD\n subgraph Group\n subgraph Nested\n A --> B\n end\n end',
    'flowchart TD\n A@{ shape: cloud }', 'flowchart TD\n A & B --> C', 'flowchart TD\n A <-- B',
    'flowchart TD\n A:::hot --> B', 'flowchart TD\n end',
    'sequenceDiagram\n A->>B: Hello\n activate B', 'sequenceDiagram\n A-xB: Destroy',
    'sequenceDiagram\n autonumber\n A->>B: Hello', 'sequenceDiagram\n A->>+B: Hello',
    'sequenceDiagram\n A->>B: Hello\n participant B as Bob', 'sequenceDiagram\n Note over B: Hi\n participant B as Bob',
    'sequenceDiagram\n A->>B: Hello\n unknown statement', 'sequenceDiagram\n rect rgb(1,2,3)\n A->>B: Hello\n end',
    'classDiagram\n class A\n namespace X {\n class B\n }', 'classDiagram\n A -- B',
    'classDiagram\n class A\n direction LR', 'erDiagram\n ENTITY',
    'erDiagram\n A ||--o{ B : owns\n direction LR', 'erDiagram\n A {\n string name NOT_A_KEY\n }',
    'xychart-beta\n bar [1, 2junk]', 'xychart-beta\n bar [1,2] trailing', 'xychart-beta\n bar [1,2]\n line [3,4]',
    'xychart-beta\n x-axis [a,b]\n bar [1,2,3]', 'xychart-beta\n y-axis 10 --> 1\n bar [1]',
    'xychart-beta\n x-axis ["quoted"]\n bar [1]', 'flowchart TD\n A[Hello] --> B\n accTitle: Hello',
  ];
  for (const source of unsupported) assert.equal(validateMermaid(source).ok, false, source);
});
test('ordinary data labels and class names survive real-library rendering', () => {
  for (const [source, label] of [
    ['flowchart TD\n A[data: string] --> B[Result]', 'data: string'],
    ['sequenceDiagram\n A->>B: data: string', 'data: string'],
    ['stateDiagram-v2\n data: Ready\n data --> Done', 'Ready'],
    ['classDiagram\n class data\n data: +String value', 'data'],
  ]) {
    assert.equal(validateMermaid(source!).ok, true, source);
    assert.ok(prepareMermaidSVG(renderMermaidSVG(source!), palette, css).svg.includes(`>${label}</text>`), source);
  }
});
test('data URLs still stay source-only, including omitted media types and mixed case', () => {
  for (const url of ['data:text/plain,hello', 'data:text/html;base64,PHNjcmlwdD4=', 'data:,hello', 'data:;base64,aGVsbG8=', 'DATA:image/svg+xml,unsafe', 'data: text/plain,hello', 'data:text/plain ,hello']) {
    const validation = validateMermaid(`flowchart TD\n A[${url}] --> B`);
    assert.equal(validation.ok, false, url);
    if (!validation.ok) assert.equal(validation.reason, 'unsafe-source', url);
  }
});
test('unsafe instructions, malformed syntax, empty input and budgets have distinct outcomes', () => {
  for (const source of ['%%{init: {}}%%\nflowchart TD\n A --> B', '---\ntitle: Hi\n---\nflowchart TD\n A --> B', 'flowchart TD\n A[<img src=x>]', 'flowchart TD\n A[&#10;]']) {
    assert.equal((validateMermaid(source) as {reason: string}).reason, 'unsafe-source');
  }
  assert.equal((validateMermaid('pie\n "A": 1') as {reason: string}).reason, 'unsupported-type');
  assert.equal((validateMermaid('') as {reason: string}).reason, 'empty');
  for (const source of ['flowchart TD', 'classDiagram\n class A {\n +String name', 'sequenceDiagram\n loop Again\n A->>B: Hi']) assert.equal((validateMermaid(source) as {reason: string}).reason, 'invalid-syntax');
  assert.equal((validateMermaid('flowchart TD\n A[' + '界'.repeat(12000) + ']') as {reason: string}).reason, 'too-large');
  assert.equal((validateMermaid('flowchart TD\n' + ' A --> B\n'.repeat(251)) as {reason: string}).reason, 'too-large');
  assert.equal((validateMermaid('flowchart TD\n A[' + 'x'.repeat(2049) + ']') as {reason: string}).reason, 'too-large');
});
test('image guard rejects active/remote resources, oversized output and invalid dimensions', () => {
  const raw = renderMermaidSVG(fixtures.flowchart);
  for (const extra of ['<script>evil()</script>', '<image href="https://bad.test/x"/>', '<g onclick="x"/>', '<style>@import url(x);</style>', '<style>.x{fill:url(https://bad.test/x)}</style>', '<foreignObject/>']) assert.throws(() => prepareMermaidSVG(raw.replace('</svg>', extra + '</svg>'), palette, css), MermaidRenderError);
  for (const modified of [raw.replace(/width="[^"]+"/, 'width="0"'), raw.replace(/height="[^"]+"/, 'height="999999"'), 'x'.repeat(mermaidLimits.outputBytes + 1)]) assert.throws(() => prepareMermaidSVG(modified, palette, css), {reason: 'output-limit'});
  assert.throws(() => validateMermaidPalette({...palette, accent: 'red;--bad:url(x)'}));
});

test('one-level flowchart groups preserve labels and membership', () => {
  const source = 'flowchart TD\n subgraph Runtime[Agent runtime]\n A[Agent] --> B[Tool]\n end\n B --> C[Mailbox]';
  assert.equal(validateMermaid(source).ok, true);
  const graph = parseMermaid(source);
  assert.equal(graph.subgraphs[0]?.label, 'Agent runtime');
  assert.deepEqual(graph.subgraphs[0]?.nodeIds, ['A', 'B']);
  assert.match(prepareMermaidSVG(renderMermaidSVG(source), palette, css).svg, /Agent runtime/);
  assert.equal(validateMermaid('flowchart TD\n subgraph Group\n A --> B').ok, false);
  assert.equal(validateMermaid(source + '\n C --> Runtime').ok, false);
});

test('upstream reserved prefixes and whitespace cannot bypass full-consumption policy', () => {
  for (const actor of ['loopActor', 'alternate', 'partner', 'option', 'criticalAgent', 'breakpoint', 'rectangle', 'elsewhere', 'android']) {
    assert.equal(validateMermaid(`sequenceDiagram\n ${actor}->>B: Important\n B->>C: Result`).ok, false, actor);
  }
  assert.equal(validateMermaid('sequenceDiagram\n Note left of A,B: over time').ok, false);
  assert.equal(validateMermaid('xychart-beta\n x-axis [A,B]\n x-axis\t[C,D]\n bar [1,2]').ok, false);
  assert.equal(validateMermaid('sequenceDiagram\n alt\tReady\n A->>B: Hi\n else No\n B->>A: Done\n end').ok, true);
});

test('sequence notes require an earlier message because upstream drops afterIndex=-1', () => {
  assert.equal(validateMermaid('sequenceDiagram\n participant A\n Note over A: Important\n A->>B: Sent').ok, false);
  assert.equal(validateMermaid('sequenceDiagram\n Note over A: Important').ok, false);
  const source = 'sequenceDiagram\n A->>B: Sent\n Note over A: Important';
  assert.equal(validateMermaid(source).ok, true);
  assert.match(renderMermaidSVG(source), />Important</);
});

test('ER tooltip-only comments stay in source instead of disappearing in an image', () => {
  assert.equal(validateMermaid('erDiagram\n A {\n string name PK "Required value"\n }').ok, false);
});
