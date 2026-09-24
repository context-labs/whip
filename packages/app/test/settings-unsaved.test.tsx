import { useState } from 'react';
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { createMemoryHistory, createRootRoute, createRoute, createRouter, Link, Outlet, RouterProvider } from '@tanstack/react-router';
import { ThemeProvider, UIProvider } from '@whip/ui';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { SettingsEditsProvider, useSettingsEdits } from '../src/settings/unsaved';

beforeEach(() => { vi.stubGlobal('matchMedia', () => ({ matches: false, addEventListener() {}, removeEventListener() {} })); vi.spyOn(window, 'scrollTo').mockImplementation(() => {}); });
afterEach(() => vi.unstubAllGlobals());
function fixture(secret = false, succeeds = true) {
  const save = vi.fn(async () => succeeds);
  function Editor() {
    const [value, setValue] = useState('');
    useSettingsEdits({ id: 'test', dirty: value.length > 0, description: secret ? 'Unsaved API key' : 'Unsaved host defaults', discard: () => setValue(''), ...(secret ? {} : { save }) });
    return <><label>Draft<input value={value} onChange={event => setValue(event.target.value)} /></label><Link to="/">Leave</Link></>;
  }
  const root = createRootRoute({ component: () => <Outlet /> });
  const settings = createRoute({ getParentRoute: () => root, path: '/settings', validateSearch: (search: Record<string, unknown>) => ({ section: search.section, host: search.host, setting: search.setting }), component: () => <SettingsEditsProvider><Editor /></SettingsEditsProvider> });
  const home = createRoute({ getParentRoute: () => root, path: '/', component: () => <p>Workspace returned</p> });
  const history = createMemoryHistory({ initialEntries: ['/settings?section=providers&host=one'] });
  const router = createRouter({ routeTree: root.addChildren([settings, home]), history });
  render(<ThemeProvider initialTheme="light"><UIProvider><RouterProvider router={router} /></UIProvider></ThemeProvider>);
  return { router, history, save };
}

it('offers Stay, Discard and explicit Save when navigating away from dirty settings', async () => {
  const f = fixture();
  fireEvent.change(await screen.findByLabelText('Draft'), { target: { value: 'unsaved' } });
  fireEvent.click(screen.getByRole('link', { name: 'Leave' }));
  fireEvent.click(await screen.findByRole('button', { name: 'Stay' }));
  await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
  expect((screen.getByLabelText('Draft') as HTMLInputElement).value).toBe('unsaved');
  expect(f.save).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole('link', { name: 'Leave' }));
  fireEvent.click(await screen.findByRole('button', { name: 'Save changes' }));
  await screen.findByText('Workspace returned');
  expect(f.save).toHaveBeenCalledOnce();
});

it('discards secrets only after the explicit discard choice and never saves them implicitly', async () => {
  const f = fixture(true);
  fireEvent.change(await screen.findByLabelText('Draft'), { target: { value: 'private-key' } });
  fireEvent.click(screen.getByRole('link', { name: 'Leave' }));
  await screen.findByRole('dialog', { name: 'Unsaved settings' });
  expect(screen.queryByRole('button', { name: 'Save changes' })).toBeNull();
  fireEvent.click(screen.getByRole('button', { name: 'Discard changes' }));
  await screen.findByText('Workspace returned');
  expect(f.save).not.toHaveBeenCalled();
});

it('keeps the form and destination pending when an explicit save fails', async () => {
  const f = fixture(false, false);
  fireEvent.change(await screen.findByLabelText('Draft'), { target: { value: 'unsaved' } });
  fireEvent.click(screen.getByRole('link', { name: 'Leave' }));
  fireEvent.click(await screen.findByRole('button', { name: 'Save changes' }));
  await screen.findByText(/The changes could not be saved/);
  expect(screen.queryByText('Workspace returned')).toBeNull();
  expect(f.save).toHaveBeenCalledOnce();
  fireEvent.click(screen.getByRole('button', { name: 'Stay' }));
  await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
  expect((screen.getByLabelText('Draft') as HTMLInputElement).value).toBe('unsaved');
});

it('guards category and host changes but allows focusing another control in the same form', async () => {
  const f = fixture();
  fireEvent.change(await screen.findByLabelText('Draft'), { target: { value: 'unsaved' } });
  await act(async () => { f.history.replace('/settings?section=providers&host=one&setting=default_model'); });
  expect(screen.queryByRole('dialog')).toBeNull();
  act(() => f.history.replace('/settings?section=providers&host=two'));
  fireEvent.click(await screen.findByRole('button', { name: 'Stay' }));
  await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
  act(() => f.history.replace('/settings?section=execution&host=one'));
  fireEvent.click(await screen.findByRole('button', { name: 'Discard changes' }));
  await waitFor(() => expect(f.history.location.search).toContain('section=execution'));
  expect(f.save).not.toHaveBeenCalled();
});
