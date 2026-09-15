import { StrictMode } from 'react';
import { act, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { defaultDisplayPreferences, displayStorageKey, ThemeProvider } from '@whip/ui';
import { StartupScreen } from '../src/startup-screen';

beforeEach(() => {
  vi.useFakeTimers();
  vi.stubGlobal('matchMedia', () => ({ matches: false, addEventListener() {}, removeEventListener() {} }));
});
afterEach(() => { vi.useRealTimers(); vi.unstubAllGlobals(); });

function fixture(startup: Promise<unknown>, savedMotion = false) {
  const storage = savedMotion ? { getItem: (key: string) => key === displayStorageKey ? JSON.stringify({ version: 1, display: { ...defaultDisplayPreferences, motion: 'reduce' } }) : null, setItem() {} } : undefined;
  const view = render(<StrictMode><ThemeProvider initialTheme="dark" storage={storage}>
    <StartupScreen startup={startup}><button>New session</button></StartupScreen>
  </ThemeProvider></StrictMode>);
  return { ...view, content: () => view.container.querySelector('[inert]') };
}

it('keeps the mounted application inert until startup finishes, including in StrictMode', async () => {
  const startup = Promise.withResolvers<void>();
  const view = fixture(startup.promise);
  expect(screen.getByRole('status').textContent).toBe('Loading whipcode…');
  expect(screen.getByRole('status').querySelector('svg')).not.toBeNull();
  expect(view.content()).not.toBeNull();
  expect(screen.queryByRole('button', { name: 'New session' })).toBeNull();
  await act(async () => startup.resolve());
  expect(screen.getByRole('status')).toBeDefined();
  act(() => vi.advanceTimersByTime(220));
  expect(screen.queryByRole('status')).toBeNull();
  expect(screen.getByRole('button', { name: 'New session' })).toBeDefined();
  expect(view.content()).toBeNull();
  act(() => vi.advanceTimersByTime(260));
  expect(view.container.firstElementChild?.getAttribute('data-startup-phase')).toBe('visible');
});

it.each(['failed', 'stalled'])('reveals connection recovery when startup is %s', async kind => {
  const startup = Promise.withResolvers<void>();
  fixture(startup.promise);
  if (kind === 'failed') await act(async () => startup.reject(new Error('Host offline')));
  else act(() => vi.advanceTimersByTime(3000));
  act(() => vi.advanceTimersByTime(220));
  expect(screen.queryByRole('status')).toBeNull();
  expect(screen.getByRole('button', { name: 'New session' })).toBeDefined();
});

it.each(['system', 'saved'])('skips animation for %s reduced motion', async source => {
  if (source === 'system') vi.stubGlobal('matchMedia', (query: string) => ({ matches: query.includes('reduced-motion'), addEventListener() {}, removeEventListener() {} }));
  const startup = Promise.withResolvers<void>();
  fixture(startup.promise, source === 'saved');
  await act(async () => startup.resolve());
  expect(screen.queryByRole('status')).toBeNull();
  expect(screen.getByRole('button', { name: 'New session' })).toBeDefined();
});

it('does not replay when the mounted application navigates or reconnects', async () => {
  const startup = Promise.withResolvers<void>();
  const view = fixture(startup.promise);
  await act(async () => startup.resolve());
  act(() => vi.advanceTimersByTime(220));
  act(() => vi.advanceTimersByTime(260));
  view.rerender(<StrictMode><ThemeProvider initialTheme="dark">
    <StartupScreen startup={new Promise(() => {})}><button>Settings</button></StartupScreen>
  </ThemeProvider></StrictMode>);
  expect(screen.queryByRole('status')).toBeNull();
  expect(screen.getByRole('button', { name: 'Settings' })).toBeDefined();
});

it('cleans up pending work on disposal', async () => {
  const startup = Promise.withResolvers<void>();
  const view = fixture(startup.promise);
  view.unmount();
  expect(vi.getTimerCount()).toBe(0);
  await act(async () => startup.resolve());
  expect(vi.getTimerCount()).toBe(0);
});
