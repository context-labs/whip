import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { ThemeProvider, UIProvider, themeCatalog } from '@whip/ui';
import type { WhipClient } from '@whip/sdk';
import type { Resolved } from '@whip/protocol';
import { CustomThemes } from '../src/settings/custom-themes';

beforeEach(() => vi.stubGlobal('matchMedia', () => ({ matches: false, addEventListener() {}, removeEventListener() {} })));
afterEach(() => vi.unstubAllGlobals());

it('loads local themes only on demand and discards a pending import after dismissal', async () => {
  const snapshot = { state: 'connected', info: { runtime_id: 'home' } };
  const list = vi.fn(async () => ({ themes: [], errors: [], truncated: false }));
  let complete!: (value: Resolved) => void;
  const resolveJSON = vi.fn((_json: string, _options: { signal: AbortSignal }) => new Promise<Resolved>(resolve => { complete = resolve; }));
  const storage = { getItem: () => null, setItem: vi.fn() };
  const client = { subscribe: () => () => {}, getSnapshot: () => snapshot, host: { themes: { list, resolveJSON } } } as unknown as WhipClient;
  const queries = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const view = render(<QueryClientProvider client={queries}><ThemeProvider initialTheme="dark" storage={storage}><UIProvider><CustomThemes client={client} /></UIProvider></ThemeProvider></QueryClientProvider>);
  expect(list).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole('button', { name: 'Import theme', exact: true }));
  await waitFor(() => expect(list).toHaveBeenCalledOnce());
  const file = { name: 'theme.json', size: 2, text: async () => '{}' };
  fireEvent.change(screen.getByLabelText('Theme JSON file'), { target: { files: [file] } });
  await waitFor(() => expect(resolveJSON).toHaveBeenCalledOnce());
  const signal = resolveJSON.mock.calls[0]![1].signal;
  const saved = storage.setItem.mock.calls.length;
  fireEvent.click(screen.getByRole('button', { name: 'Cancel', exact: true }));
  expect(signal.aborted).toBe(true);
  const base = themeCatalog[0]!;
  const { onPrimary, borderFocus, diffAdd, diffDel, ...colors } = base.colors;
  await act(async () => complete({ ...base, id: 'late', name: 'Late theme', colors: { ...colors, on_primary: onPrimary, border_focus: borderFocus, diff_add: diffAdd, diff_del: diffDel } } as Resolved));
  expect(storage.setItem.mock.calls.length).toBe(saved);
  expect(document.documentElement.dataset.theme).toBe('dark');
  expect(screen.queryByRole('dialog')).toBeNull();
  view.unmount(); queries.clear();
});
