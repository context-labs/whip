import { act, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { ThemeProvider, UIProvider } from '@whip/ui';
import { MessageRow } from '../src/timeline';
import { RuntimeContext } from '../src/context';
import { AppRuntime } from '../src/runtime';

beforeEach(() => vi.stubGlobal('matchMedia', () => ({ matches: false, addEventListener() {}, removeEventListener() {} })));
afterEach(() => vi.unstubAllGlobals());
it('changes only loaded tool presentation, preserves explicit choices, and leaves reasoning closed', () => {
  const data = new Map<string, string>();
  const storage = { keys: () => [...data.keys()], getItem: (key: string) => data.get(key) ?? null, setItem: (key: string, value: string) => { data.set(key, value); }, removeItem: (key: string) => { data.delete(key); } };
  const runtime = new AppRuntime({ defaultEndpoint: 'http://127.0.0.1:8080', storage, copy: async () => {}, openExternal: async () => {}, download: async () => {} });
  const readBody = vi.fn();
  const row = { id: 'tool', role: 'tool', label: 'Starlark execution', args: '{"code":"print(42)"}', text: `one\ntwo\nthree\n${'extra'.repeat(1000)}` };
  const view = render(<RuntimeContext.Provider value={runtime}><ThemeProvider storage={storage}><UIProvider>
    <MessageRow row={row} readBody={readBody} />
    <MessageRow row={{ id: 'reasoning', role: 'reasoning', text: 'private reasoning display' }} readBody={readBody} />
  </UIProvider></ThemeProvider></RuntimeContext.Provider>);
  const tool = view.container.querySelector<HTMLDetailsElement>('[data-message-id="tool"] details')!;
  expect(tool.open).toBe(false); expect(view.container.querySelector('[data-tool-preview]')).toBeNull();
  act(() => runtime.setPreferences({ toolDensity: 'comfortable' }));
  expect(tool.open).toBe(false); expect(view.container.querySelector('[data-tool-preview]')?.textContent).toBe('one\ntwo\nthree');
  act(() => runtime.setPreferences({ toolDensity: 'detailed' }));
  expect(tool.open).toBe(true);
  expect(view.container.querySelector<HTMLDetailsElement>('[data-message-id="reasoning"] details')!.open).toBe(false);
  expect(readBody).not.toHaveBeenCalled();
  fireEvent.click(screen.getByText('Starlark execution'));
  act(() => runtime.setPreferences({ toolDensity: 'compact' }));
  act(() => runtime.setPreferences({ toolDensity: 'detailed' }));
  expect(tool.open).toBe(false);
  view.unmount(); runtime.dispose();
});
