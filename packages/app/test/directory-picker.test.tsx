import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { ThemeProvider, UIProvider } from '@whip/ui';
import type { WhipClient } from '@whip/sdk';
import { DirectoryPicker } from '../src/directory-picker';

beforeEach(() => { vi.stubGlobal('matchMedia', () => ({ matches: false, addEventListener() {}, removeEventListener() {} })); });
afterEach(() => vi.unstubAllGlobals());
function fixture() {
  let finish: (value: { path: string }) => void = () => {};
  const pickDirectory = vi.fn(() => new Promise<{ path: string }>(resolve => { finish = resolve; }));
  const directories = vi.fn(async ({ path }: { path?: string }) => ({ path: path || '/start', entries: [], has_more: false }));
  const connection = { state: 'connected', info: { runtime_id: 'local' } };
  const client = { subscribe: () => () => {}, getSnapshot: () => connection, host: { pickDirectory, directories } } as unknown as WhipClient;
  const onSelect = vi.fn(), submit = vi.fn();
  const query = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } });
  const tree = (target = client, native = false) => <ThemeProvider initialTheme="light"><UIProvider><QueryClientProvider client={query}>
    <form onSubmit={event => { event.preventDefault(); submit(); }}><DirectoryPicker client={target} native={native} value="/start" onSelect={onSelect} disabled={false} /></form>
  </QueryClientProvider></UIProvider></ThemeProvider>;
  return { client, tree, onSelect, submit, pickDirectory, directories, finish: (path: string) => finish({ path }) };
}
it('browses remote directories without submitting the session form through the portal', async () => {
  const f = fixture(); render(f.tree());
  expect(screen.queryByRole('button', { name: 'Choose folder…', exact: true })).toBeNull();
  fireEvent.click(screen.getByRole('button', { name: 'Browse host', exact: true }));
  fireEvent.change(screen.getByLabelText('Host path'), { target: { value: '/remote/project' } });
  fireEvent.click(screen.getByRole('button', { name: 'Go', exact: true }));
  await waitFor(() => expect(f.directories).toHaveBeenCalledWith(expect.objectContaining({ path: '/remote/project' }), expect.anything()));
  await screen.findByText('/remote/project', { exact: true });
  fireEvent.click(screen.getByRole('button', { name: 'Use this folder', exact: true }));
  expect(f.onSelect).toHaveBeenCalledExactlyOnceWith('/remote/project');
  expect(f.submit).not.toHaveBeenCalled();
  expect(f.pickDirectory).not.toHaveBeenCalled();
});
it('ignores a native picker response after the target client changes', async () => {
  const f = fixture(), view = render(f.tree(f.client, true));
  fireEvent.click(screen.getByRole('button', { name: 'Choose folder…', exact: true }));
  const connection = { state: 'connected', info: { runtime_id: 'remote' } };
  const remote = { ...f.client, getSnapshot: () => connection } as unknown as WhipClient;
  view.rerender(f.tree(remote, false));
  await act(async () => f.finish('/local/stale'));
  expect(f.onSelect).not.toHaveBeenCalled();
});
