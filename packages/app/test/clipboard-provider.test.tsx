import {fireEvent, render, screen, waitFor} from '@testing-library/react';
import {afterEach, expect, it, vi} from 'vitest';
import {createMemoryHistory} from '@tanstack/react-router';
import {CodeBlock} from '@whip/ui';
import {createWhipApplication} from '../src';
import type {AppPlatform, AppStorage} from '../src/platform';

vi.hoisted(() => {
  vi.stubGlobal('ResizeObserver', class {observe() {} unobserve() {} disconnect() {}});
});

// Exercise real application providers without mounting a route or connecting a host.
vi.mock('@tanstack/react-router', async importOriginal => ({
  ...await importOriginal<typeof import('@tanstack/react-router')>(),
  RouterProvider: () => null,
}));
afterEach(() => vi.unstubAllGlobals());

it('wires shared code blocks through AppPlatform.copy with its receiver intact', async () => {
  vi.stubGlobal('matchMedia', () => ({matches: false, addEventListener() {}, removeEventListener() {}}));
  const values = new Map<string, string>();
  const storage: AppStorage = {keys: () => [...values.keys()], getItem: key => values.get(key) ?? null,
    setItem: (key, value) => {values.set(key, value);}, removeItem: key => {values.delete(key);}};
  const copy = vi.fn(async function(this: AppPlatform, text: string) {
    expect(this).toBe(platform);
    expect(text).toBe('native clipboard');
  });
  const platform: AppPlatform = {defaultEndpoint: 'http://127.0.0.1:8080', storage, windowStorage: storage, copy,
    openExternal: async () => {}, download: async () => {}};
  const app = createWhipApplication(platform, createMemoryHistory({initialEntries: ['/']}));
  const view = render(<app.Application><CodeBlock code="native clipboard"/></app.Application>);
  try {
    fireEvent.click(screen.getByRole('button', {name: 'Copy code'}));
    await waitFor(() => expect(copy).toHaveBeenCalledExactlyOnceWith('native clipboard'));
    await screen.findByRole('button', {name: 'Copied'});
  } finally {
    view.unmount();
    app.dispose();
  }
});
