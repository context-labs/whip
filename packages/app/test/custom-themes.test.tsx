import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { QueryClientProvider } from '@tanstack/react-query';
import { ThemeProvider, UIProvider, themeCatalog } from '@whip/ui';
import type { HostThemeResolved } from '@whip/sdk';
import { CustomThemes } from '../src/settings/custom-themes';
import { providerFixture } from './provider-fixture';

beforeEach(() => vi.stubGlobal('matchMedia', () => ({ matches: false, addEventListener() {}, removeEventListener() {} })));
afterEach(() => vi.unstubAllGlobals());
it('loads host themes only on demand and discards a valid late import after dismissal', async () => {
  const f = await providerFixture();
  let complete!: (value: HostThemeResolved) => void, signal: AbortSignal | undefined;
  f.data.handlers['host.themes.list'] = () => ({ themes: [], errors: [], truncated: false });
  f.data.handlers['host.themes.resolve'] = (_request, optionsSignal) => { signal = optionsSignal; return new Promise<HostThemeResolved>(resolve => { complete = resolve; }); };
  const storage = { getItem: () => null, setItem: vi.fn() };
  const view = render(<QueryClientProvider client={f.queries}><ThemeProvider initialTheme="dark" storage={storage}><UIProvider><CustomThemes client={f.client} /></UIProvider></ThemeProvider></QueryClientProvider>);
  expect(f.count('host.themes.list')).toBe(0);
  fireEvent.click(screen.getByRole('button', { name: 'Import theme' }));
  await waitFor(() => expect(f.count('host.themes.list')).toBe(1));
  const file = { name: 'theme.json', size: 2, text: async () => '{}' };
  fireEvent.change(screen.getByLabelText('Theme JSON file'), { target: { files: [file] } });
  await waitFor(() => expect(f.count('host.themes.resolve')).toBe(1));
  expect(f.calls.find(call => call.method === 'host.themes.resolve')?.params).toEqual({ name: '', json: '{}' });
  const saved = storage.setItem.mock.calls.length;
  fireEvent.click(screen.getByRole('button', { name: 'Cancel' })); expect(signal?.aborted).toBe(true);
  const base = themeCatalog[0]!;
  const { onPrimary, borderFocus, diffAdd, diffDel, ...colors } = base.colors;
  const resolved: HostThemeResolved = { id: 'late', name: 'Late theme', dark: base.dark, syntax: base.syntax, markdown: base.markdown,
    code: { foreground: base.code.foreground, background: base.code.background, tokens: null },
    colors: { ...colors, on_primary: onPrimary, border_focus: borderFocus, diff_add: diffAdd, diff_del: diffDel } };
  await act(async () => complete(resolved));
  expect(storage.setItem.mock.calls.length).toBe(saved); expect(document.documentElement.dataset.theme).toBe('dark'); expect(screen.queryByRole('dialog')).toBeNull();
  view.unmount(); f.queries.clear();
});
