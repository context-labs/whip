import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { createMemoryHistory, createRootRoute, createRouter, RouterProvider } from '@tanstack/react-router';
import { ThemeProvider, UIProvider } from '@whip/ui';
import type { ProviderCatalogsResult } from '@whip/protocol';
import type { ComponentProps } from 'react';
import { modelOptions } from '../src/model-options';
import { ModelPicker } from '../src/model-selection';
import { RuntimeContext } from '../src/context';
import type { AppRuntime } from '../src/runtime';

beforeEach(() => vi.stubGlobal('matchMedia', () => ({ matches: false, addEventListener() {}, removeEventListener() {} })));
afterEach(() => vi.unstubAllGlobals());

const catalog = {
  models: { 'gpt-5.5': { providers: ['openrouter'] }, review: { id: 'gpt-5.5', providers: ['openai-codex'] } },
  providers: {},
  catalogs: {
    openrouter: { models: [{ id: 'gpt-5.5', context_length: 400000, reasoning_efforts: ['low'] }] },
    'openai-codex': { models: [
      { id: 'gpt-5.5', context_length: 258400, reasoning_efforts: ['low', 'xhigh'] },
      { id: 'gpt-6-astra', context_length: 258400, reasoning_efforts: ['low', 'ultra'] },
    ] },
  },
} as unknown as ProviderCatalogsResult;

it('offers discovered models and keeps explicit provider routes and configured aliases', () => {
  const options = modelOptions(catalog);
  expect(options.map(option => [option.name, option.provider])).toEqual([
    ['gpt-5.5', 'openai-codex'], ['gpt-5.5', 'openrouter'],
    ['gpt-6-astra', 'openai-codex'], ['review', 'openai-codex'],
  ]);
  expect(options.find(option => option.name === 'review')?.model.context_length).toBe(258400);
  const shadowed = modelOptions({ ...catalog, models: { 'gpt-5.5': { id: 'gpt-6-astra', providers: ['openai-codex'] } } });
  expect(shadowed.filter(option => option.name === 'gpt-5.5').map(option => [option.provider, option.model.id]))
    .toEqual([['openai-codex', 'gpt-6-astra']]);
});

it('removes unavailable provider routes from configured and cached model choices', () => {
  const options = modelOptions({ ...catalog, providers: { openrouter: { available: false } } });
  expect(options.some(option => option.provider === 'openrouter')).toBe(false);
  expect(options.some(option => option.provider === 'openai-codex')).toBe(true);
});

it('switches the provider when the selected model name is unchanged', async () => {
  const setModel = vi.fn(async () => ({}));
  const queries = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } });
  const runtime = { run: (result: Promise<unknown>) => result, report: vi.fn() } as unknown as AppRuntime;
  const props = {
    connected: true, root: { meta: { model: 'gpt-5.5', provider: 'openrouter' }, active_turns: {} },
    view: { session: { setModel, client: {
      getSnapshot: () => ({ info: { runtime_id: 'host' } }),
      providers: { catalogs: async () => ({ result: catalog }) },
    } } },
  } as unknown as ComponentProps<typeof ModelPicker>;
  const route = createRootRoute({ component: () => <QueryClientProvider client={queries}>
    <RuntimeContext.Provider value={runtime}><ThemeProvider initialTheme="light"><UIProvider>
      <ModelPicker {...props} />
    </UIProvider></ThemeProvider></RuntimeContext.Provider>
  </QueryClientProvider> });
  const router = createRouter({ routeTree: route, history: createMemoryHistory() });
  render(<RouterProvider router={router} />);
  fireEvent.click(await screen.findByRole('button', { name: 'Model', exact: true }));
  const current = await screen.findByRole('option', { name: 'gpt-5.5 · openrouter' });
  expect(current.getAttribute('aria-selected')).toBe('true');
  fireEvent.change(screen.getByRole('textbox', { name: 'Search models' }), { target: { value: 'openai-codex' } });
  expect(screen.queryByRole('option', { name: 'gpt-5.5 · openrouter' })).toBeNull();
  const subscription = screen.getByRole('option', { name: 'gpt-5.5 · openai-codex' });
  expect(subscription.getAttribute('aria-selected')).toBe('false');
  fireEvent.click(subscription);
  await waitFor(() => expect(setModel).toHaveBeenCalledExactlyOnceWith('gpt-5.5', 'openai-codex'));
});
