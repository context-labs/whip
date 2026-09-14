import { act, fireEvent, render, screen, within } from '@testing-library/react';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { createMemoryHistory, createRootRoute, createRouter, RouterProvider } from '@tanstack/react-router';
import { formatForDisplay } from '@tanstack/react-hotkeys';
import { ThemeProvider, UIProvider } from '@whip/ui';
import { AppRuntime } from '../src/runtime';
import { RuntimeContext, ShellCommandsContext } from '../src/context';
import { EmptyWorkspace } from '../src/empty-workspace';
import type { HostConnection } from '../src/hosts';

const runtimes: AppRuntime[] = [];
beforeEach(() => vi.stubGlobal('matchMedia', () => ({ matches: false, addEventListener() {}, removeEventListener() {} })));
afterEach(() => { for (const runtime of runtimes.splice(0)) runtime.dispose(); vi.unstubAllGlobals(); });

function fixture({ missing = false, subject, items = [{ id: 's1' }], commands = vi.fn() }: {
  missing?: boolean; subject?: 'draft' | 'terminal'; items?: readonly { id: string }[]; commands?: ((action: string) => void) | null;
} = {}) {
  const values = new Map<string, string>();
  const storage = { keys: () => [...values.keys()], getItem: (key: string) => values.get(key) ?? null, setItem: (key: string, value: string) => { values.set(key, value); }, removeItem: (key: string) => { values.delete(key); } };
  const runtime = new AppRuntime({ storage, defaultEndpoint: 'http://localhost:8080', copy: async () => {}, download: async () => {}, openExternal: async () => {} });
  runtimes.push(runtime);
  const list = { subscribe: () => () => {}, getSnapshot: () => ({ status: 'live', page: { items }, truncated: false }) };
  const host = { id: 'local', runtimeId: 'host', name: 'Local', state: 'connected', list } as unknown as HostConnection;
  vi.spyOn(runtime, 'getSnapshot').mockReturnValue({ ...runtime.getSnapshot(), hosts: [host] });
  const route = createRootRoute({
    component: () => <RuntimeContext.Provider value={runtime}><ThemeProvider initialTheme="dark"><UIProvider>
      <ShellCommandsContext.Provider value={commands}><EmptyWorkspace missing={missing} subject={subject} /></ShellCommandsContext.Provider>
    </UIProvider></ThemeProvider></RuntimeContext.Provider>,
    notFoundComponent: () => null,
  });
  const router = createRouter({ routeTree: route, history: createMemoryHistory({ initialEntries: ['/'] }) });
  render(<RouterProvider router={router} />);
  return { runtime, router, commands };
}

it('lists the shell actions with the live shortcuts and runs them', async () => {
  const f = fixture();
  await screen.findByRole('heading', { name: 'What do you want to work on?', level: 1 });
  const preferences = f.runtime.getSnapshot().preferences;
  expect(within(screen.getByRole('button', { name: 'New terminal' })).getByText(formatForDisplay(preferences.terminalShortcut))).toBeTruthy();
  expect(within(screen.getByRole('button', { name: 'Commands' })).getByText(formatForDisplay(preferences.commandShortcut))).toBeTruthy();
  expect(screen.queryByText('Reopen closed tab')).toBeNull();
  expect(screen.queryByText(/Choose a project folder/)).toBeNull();
  fireEvent.click(screen.getByRole('button', { name: 'Search sessions' }));
  fireEvent.click(screen.getByRole('button', { name: 'New session' }));
  expect(f.commands).toHaveBeenNthCalledWith(1, 'navigation');
  expect(f.commands).toHaveBeenNthCalledWith(2, 'new');
  act(() => { const id = f.runtime.tabs.open('host', 'a'); f.runtime.tabs.closeViews([id]); });
  fireEvent.click(await screen.findByRole('button', { name: 'Reopen closed tab' }));
  expect(f.commands).toHaveBeenLastCalledWith('tabs:reopen');
});

it('explains the first run once every connected host has an empty catalog', async () => {
  fixture({ items: [] });
  expect(await screen.findByText('Choose a project folder on a host, then describe the task.')).toBeTruthy();
});

it('keeps the missing-tab variants with the same actions', async () => {
  fixture({ missing: true, subject: 'terminal' });
  await screen.findByRole('heading', { name: 'This terminal isn’t open here.', level: 1 });
  expect(screen.getByText('Its tab may belong to another window, or its shell has ended.')).toBeTruthy();
  expect(screen.queryByText(/Choose a project folder/)).toBeNull();
  expect(screen.getByRole('button', { name: 'New session' })).toBeTruthy();
});
