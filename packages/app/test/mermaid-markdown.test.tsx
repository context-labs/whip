import {isValidElement, type ComponentPropsWithoutRef} from 'react';
import {act, fireEvent, render, screen} from '@testing-library/react';
import {expect, it, vi} from 'vitest';
import {parseMarkdown} from '@tanstack/markdown/parser';
import {CodeBlock, UIProvider, ThemeProvider} from '@whip/ui';
import {Prose} from '../src/timeline';
import {DiagramChoices, MarkdownCodeBlock} from '../src/markdown-code-block';
import {MarkdownBlock, markdownRows, type MarkdownRow} from '../src/streaming-markdown';
import {responseCopies} from '../src/chat-activity-rows';

// The real worker/image boundary has browser coverage; here pin app routing.
vi.mock('@whip/ui', async importOriginal => ({
  ...await importOriginal<typeof import('@whip/ui')>(),
  MermaidBlock: ({code, live, truncated, view, onViewChange}: {code: string; live?: boolean; truncated?: boolean; view?: string; onViewChange?(value: string): void}) =>
    <div data-testid="diagram" data-live={!!live} data-truncated={!!truncated} data-view={view ?? 'diagram'}>
      <pre>{code}</pre><button onClick={() => onViewChange?.('source')}>Mock source</button>
    </div>,
}));
const source = 'flowchart TD\n  Agent[Agent] --> Tool[Tool]';
const markdown = 'Before\n\n```mermaid\n' + source + '\n```\n\nAfter';
const components = {pre: ({children}: ComponentPropsWithoutRef<'pre'>) => {
  if (isValidElement<{children?: unknown; className?: string}>(children) && typeof children.props.children === 'string')
    return <MarkdownCodeBlock code={children.props.children} language={children.props.className?.replace(/^language-/, '')} />;
  return <pre>{children}</pre>;
}};
const row = (live = false, truncated = false): MarkdownRow => ({id: 'a:block:0', ownerId: 'a', blockIndex: 0,
  role: 'assistant', text: markdown, live, truncated, block: parseMarkdown('```mermaid\n' + source + '\n```').children[0]!});

it('routes static Markdown fences with message readiness, not parser fence completion', () => {
  const result = render(<Prose text={markdown} live />);
  expect(screen.getByTestId('diagram').dataset.live).toBe('true');
  expect(screen.getByTestId('diagram').textContent).toContain(source);
  result.rerender(<Prose text={markdown} />);
  expect(screen.getByTestId('diagram').dataset.live).toBe('false');
  result.rerender(<Prose text={markdown} truncated />);
  expect(screen.getByTestId('diagram').dataset.truncated).toBe('true');
});

it('routes the virtualized assistant path and notices truncation changes on a stable AST', () => {
  const initial = row(true);
  const result = render(<MarkdownBlock row={initial} components={components} />);
  expect(screen.getByTestId('diagram').dataset.live).toBe('true');
  result.rerender(<MarkdownBlock row={{...initial, live: false, truncated: true}} components={components} />);
  expect(screen.getByTestId('diagram').dataset.live).toBe('false');
  expect(screen.getByTestId('diagram').dataset.truncated).toBe('true');
});

it('does not serve stale readiness from the bounded Markdown row cache', () => {
  const cache = new Map();
  const initial = [{id: 'a', role: 'assistant', text: markdown, truncated: true}];
  const first = markdownRows(initial, cache) as MarkdownRow[];
  const second = markdownRows([{...initial[0]!, truncated: false}], cache) as MarkdownRow[];
  expect(first[1]!.truncated).toBe(true);
  expect(second[1]!.truncated).toBe(false);
  expect(second[1]!.block).toBe(first[1]!.block);
});

it('retains explicit views through virtual remounts with a 128-choice bound', () => {
  const choices = new Map<string, 'diagram' | 'source'>();
  for (let i = 0; i < 128; i++) choices.set(`old-${i}`, 'source');
  const content = <DiagramChoices.Provider value={choices}><MarkdownBlock row={row()} components={components} /></DiagramChoices.Provider>;
  const result = render(content);
  fireEvent.click(screen.getByRole('button', {name: 'Mock source'}));
  expect(choices.size).toBe(128);
  expect(choices.has('old-0')).toBe(false);
  result.unmount();
  render(content);
  expect(screen.getByTestId('diagram').dataset.view).toBe('source');
});

it('retains static message choices on remount without crossing message owners', () => {
  const choices = new Map<string, 'diagram' | 'source'>();
  const content = (ownerId: string) => <DiagramChoices.Provider value={choices}><Prose text={markdown} ownerId={ownerId} /></DiagramChoices.Provider>;
  const result = render(content('user-1'));
  fireEvent.click(screen.getByRole('button', {name: 'Mock source'}));
  result.unmount();
  const remount = render(content('user-1'));
  expect(screen.getByTestId('diagram').dataset.view).toBe('source');
  remount.rerender(content('user-2'));
  expect(screen.getByTestId('diagram').dataset.view).toBe('diagram');
});

it('keeps identical static fences independent through remounts', () => {
  const choices = new Map<string, 'diagram' | 'source'>();
  const content = <DiagramChoices.Provider value={choices}><Prose text={markdown + '\n\n' + markdown} ownerId="user" /></DiagramChoices.Provider>;
  const result = render(content);
  fireEvent.click(screen.getAllByRole('button', {name: 'Mock source'})[0]!);
  result.unmount();
  render(content);
  expect(screen.getAllByTestId('diagram').map(node => node.dataset.view)).toEqual(['source', 'diagram']);
});

it('bounds retained identity bytes as well as choice counts', () => {
  const choices = new Map<string, 'diagram' | 'source'>();
  const content = (ownerId: string) => <DiagramChoices.Provider value={choices}><Prose text={markdown} ownerId={ownerId + 'x'.repeat(30000)} /></DiagramChoices.Provider>;
  const result = render(content('0'));
  for (let i = 0; i < 20; i++) {
    result.rerender(content(String(i)));
    fireEvent.click(screen.getByRole('button', {name: 'Mock source'}));
  }
  expect(choices.size).toBeLessThan(20);
  expect([...choices.keys()].reduce((sum, key) => sum + key.length * 2, 0)).toBeLessThanOrEqual(512 * 1024);
});

it('does not replace selected streaming source during settlement', () => {
  const initial = row(true);
  const result = render(<MarkdownBlock row={initial} components={components} />);
  const selection = document.getSelection()!;
  const range = document.createRange();
  range.selectNodeContents(screen.getByTestId('diagram').querySelector('pre')!);
  selection.removeAllRanges(); selection.addRange(range);
  result.rerender(<MarkdownBlock row={{...initial, live: false}} components={components} />);
  expect(screen.getByTestId('diagram').dataset.live).toBe('true');
  act(() => {selection.removeAllRanges(); document.dispatchEvent(new Event('selectionchange'));});
  expect(screen.getByTestId('diagram').dataset.live).toBe('false');
});

it('keeps ordinary Markdown and generic tool/REPL CodeBlocks as source', () => {
  render(<ThemeProvider><UIProvider><Prose text={'```typescript\nconst diagram = true;\n```'} /><CodeBlock code={source} language="mermaid" /></UIProvider></ThemeProvider>);
  expect(screen.queryByTestId('diagram')).toBeNull();
  expect(screen.getAllByRole('button', {name: 'Copy code'})).toHaveLength(2);
});

it('keeps original response copy text including the Mermaid fence', () => {
  expect([...responseCopies([{id: 'a', role: 'assistant', text: markdown}], false).values()])
    .toEqual([{text: markdown, label: 'Copy response'}]);
});
