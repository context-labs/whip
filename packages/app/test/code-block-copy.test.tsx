import {act, fireEvent, render, screen, waitFor, within} from '@testing-library/react';
import {afterEach, beforeEach, expect, it, vi} from 'vitest';
import type {ComponentPropsWithoutRef, ReactNode} from 'react';
import {CodeBlock, CopyButton, ThemeProvider, UIProvider} from '@whip/ui';
import {Prose} from '../src/timeline';
import {MarkdownBlock, markdownRows, type MarkdownRow} from '../src/streaming-markdown';
import {MotionContext} from '../src/transcript-motion';

const clipboardDescriptor = Object.getOwnPropertyDescriptor(navigator, 'clipboard');
beforeEach(() => {
  Object.defineProperty(navigator, 'clipboard', {configurable: true, get: () => undefined});
  vi.stubGlobal('matchMedia', () => ({matches: false, addEventListener() {}, removeEventListener() {}}));
});
afterEach(() => {
  vi.unstubAllGlobals();
  if (clipboardDescriptor) Object.defineProperty(navigator, 'clipboard', clipboardDescriptor);
  else Reflect.deleteProperty(navigator, 'clipboard');
});

function tree(children: ReactNode, copy?: (text: string) => Promise<void>) {
  return <UIProvider copy={copy}><ThemeProvider initialTheme="claude-code">{children}</ThemeProvider></UIProvider>;
}

it.each([undefined, 'text', 'unknown', 'javascript'])('copies exact source with language %s and keeps extra header actions', async language => {
  const code = '	const greeting = "🌍 <b>";  \n';
  const copy = vi.fn(async () => {});
  const view = render(tree(<CodeBlock code={code} language={language} headerActions={<button>Download</button>}/>, copy));
  const header = view.container.querySelector('figcaption')!;
  expect(within(header).getAllByRole('button')).toHaveLength(2);
  fireEvent.click(within(header).getByRole('button', {name: 'Copy code'}));
  await waitFor(() => expect(copy).toHaveBeenCalledExactlyOnceWith(code));
  expect(screen.getByRole('button', {name: 'Copied'})).toBeTruthy();
  if (language === 'javascript') await waitFor(() => expect(view.container.querySelector('[data-highlighted="true"]')).toBeTruthy());
});

it('copies the full loaded text instead of the bounded or decorated display', async () => {
  const code = '🌍  \n'.repeat(4000);
  const copy = vi.fn(async () => {});
  render(tree(<CodeBlock code={code} renderText={text => <span>decoration:{text}</span>}/>, copy));
  expect(screen.getByRole('status').textContent).toContain('bounded excerpt');
  expect(screen.getByRole('region').textContent).not.toBe(code);
  fireEvent.click(screen.getByRole('button', {name: 'Copy code'}));
  await waitFor(() => expect(copy).toHaveBeenCalledExactlyOnceWith(code));
});

it.each(['raw source\n', ''])('honors an explicit copyText override (%j)', async copyText => {
  const copy = vi.fn(async () => {});
  render(tree(<CodeBlock code="formatted preview" copyText={copyText} label="Output"/>, copy));
  fireEvent.click(screen.getByRole('button', {name: 'Copy Output'}));
  await waitFor(() => expect(copy).toHaveBeenCalledExactlyOnceWith(copyText));
});

it('keeps empty blocks copyable', async () => {
  const copy = vi.fn(async () => {});
  render(tree(<CodeBlock code=""/>, copy));
  fireEvent.click(screen.getByRole('button', {name: 'Copy code'}));
  await waitFor(() => expect(copy).toHaveBeenCalledExactlyOnceWith(''));
});

it('prefers an explicit callback to the provider and inherits through nested UI providers', async () => {
  const provider = vi.fn(async () => {}), explicit = vi.fn(async () => {});
  render(tree(<UIProvider><CopyButton text="explicit" copy={explicit}/><CodeBlock code="inherited"/></UIProvider>, provider));
  fireEvent.click(screen.getByRole('button', {name: 'Copy', exact: true}));
  fireEvent.click(screen.getByRole('button', {name: 'Copy code'}));
  await waitFor(() => expect(explicit).toHaveBeenCalledExactlyOnceWith('explicit'));
  expect(provider).toHaveBeenCalledExactlyOnceWith('inherited');
});

it('uses the browser clipboard for standalone UI', async () => {
  const writeText = vi.fn(async () => {});
  vi.spyOn(navigator, 'clipboard', 'get').mockReturnValue({writeText} as unknown as Clipboard);
  render(tree(<CodeBlock code="browser"/>));
  fireEvent.click(screen.getByRole('button', {name: 'Copy code'}));
  await waitFor(() => expect(writeText).toHaveBeenCalledExactlyOnceWith('browser'));
});

it('reports clipboard failure locally and clears it after retry or changed content', async () => {
  const copy = vi.fn<(text: string) => Promise<void>>().mockRejectedValueOnce(new Error('Permission denied')).mockResolvedValueOnce();
  const view = render(tree(<CodeBlock code="first"/>, copy));
  fireEvent.click(screen.getByRole('button', {name: 'Copy code'}));
  expect((await screen.findByRole('alert')).textContent).toContain('Permission denied');
  fireEvent.blur(screen.getByRole('button'));
  expect(screen.getAllByRole('alert')).toHaveLength(1);
  expect(screen.queryByRole('button', {name: 'Copied'})).toBeNull();
  fireEvent.click(screen.getByRole('button', {name: 'Could not copy. Try again.'}));
  await screen.findByRole('button', {name: 'Copied'});
  expect(screen.queryByRole('alert')).toBeNull();
  copy.mockRejectedValueOnce(new Error('Again'));
  fireEvent.click(screen.getByRole('button'));
  await screen.findByRole('alert');
  view.rerender(tree(<CodeBlock code="second"/>, copy));
  expect(screen.queryByRole('alert')).toBeNull();
  expect(screen.getByRole('button', {name: 'Copy code'})).toBeTruthy();
});

it('explains missing browser clipboard support without claiming success', async () => {
  vi.spyOn(navigator, 'clipboard', 'get').mockReturnValue(undefined as unknown as Clipboard);
  render(tree(<CodeBlock code="unavailable"/>));
  fireEvent.click(screen.getByRole('button', {name: 'Copy code'}));
  expect((await screen.findByRole('alert')).textContent).toContain('HTTPS or localhost');
  expect(screen.queryByRole('button', {name: 'Copied'})).toBeNull();
});

it.each(['resolve', 'reject'] as const)('ignores a stale clipboard %s after streaming changes the source', async outcome => {
  const pending = Promise.withResolvers<void>();
  const copy = vi.fn(() => pending.promise);
  const view = render(tree(<CodeBlock code="first"/>, copy));
  fireEvent.click(screen.getByRole('button'));
  await waitFor(() => expect(copy).toHaveBeenCalledWith('first'));
  view.rerender(tree(<CodeBlock code="second"/>, copy));
  await act(async () => { if (outcome === 'resolve') pending.resolve(); else pending.reject(new Error('Stale error')); });
  expect(screen.getByRole('button', {name: 'Copy code'})).toBeTruthy();
  expect(screen.queryByRole('alert')).toBeNull();
});

it('does not replace newer successful feedback with an older failed request', async () => {
  const pending = Promise.withResolvers<void>();
  const copy = vi.fn().mockReturnValueOnce(pending.promise).mockResolvedValueOnce(undefined);
  render(tree(<CodeBlock code="same"/>, copy));
  fireEvent.click(screen.getByRole('button'));
  fireEvent.click(screen.getByRole('button'));
  await screen.findByRole('button', {name: 'Copied'});
  await act(async () => pending.reject(new Error('Old request')));
  expect(screen.queryByRole('alert')).toBeNull();
  expect(screen.getByRole('button', {name: 'Copied'})).toBeTruthy();
});

it('adds one copy control to ordinary Markdown code blocks, but not inline code', async () => {
  const copy = vi.fn(async () => {});
  render(tree(<Prose text={'Inline `no button`\n\n```text\n  hello 🌍\n```'}/>, copy));
  expect(screen.getAllByRole('button')).toHaveLength(1);
  fireEvent.click(screen.getByRole('button', {name: 'Copy code'}));
  await waitFor(() => expect(copy).toHaveBeenCalledExactlyOnceWith('  hello 🌍'));
});

it('copies streaming Markdown code before and after settlement without decoration', async () => {
  const copy = vi.fn(async () => {}), cache = new Map();
  const components = {pre: (props: ComponentPropsWithoutRef<'pre'>) => <pre {...props}/>};
  const markdown = (code: string, live: boolean) => {
    const row = markdownRows([{id: 'text', role: 'assistant', text: '```text\n' + code + (live ? '' : '\n```'), live}], cache)[0] as MarkdownRow;
    return tree(<MotionContext.Provider value={true}><MarkdownBlock row={row} components={components}/></MotionContext.Provider>, copy);
  };
  const view = render(markdown('first', true));
  fireEvent.click(screen.getByRole('button', {name: 'Copy code'}));
  await waitFor(() => expect(copy).toHaveBeenCalledWith('first'));
  view.rerender(markdown('first\nsecond 🌍', false));
  expect(screen.getAllByRole('button')).toHaveLength(1);
  fireEvent.click(screen.getByRole('button', {name: 'Copy code'}));
  await waitFor(() => expect(copy).toHaveBeenLastCalledWith('first\nsecond 🌍'));
});
