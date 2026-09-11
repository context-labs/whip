import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { UIProvider } from '@whip/ui';
import { AppRuntime } from '../src/runtime';
import { RuntimeContext } from '../src/context';
import { WelcomeRecovery } from '../src/welcome-recovery';
import type { WelcomeSubmission } from '../src/welcome-submission';
import { selectedSessionTab } from '../src/session-tabs';

const routing = vi.hoisted(() => ({ location: { href: '/', state: { __TSR_key: 'home' } }, navigate: vi.fn(async () => {}) }));
vi.mock('@tanstack/react-router', async importOriginal => ({
  ...await importOriginal<typeof import('@tanstack/react-router')>(),
  useLocation: () => routing.location,
  useNavigate: () => routing.navigate,
}));
const runtimes: AppRuntime[] = [];
afterEach(() => { for (const runtime of runtimes.splice(0)) runtime.dispose(); routing.location = { href: '/', state: { __TSR_key: 'home' } }; routing.navigate.mockReset(); });

function fixture(accepted = false) {
  const values = new Map<string, string>();
  const storage = { keys: () => [...values.keys()], getItem: (key: string) => values.get(key) ?? null, setItem: (key: string, value: string) => { values.set(key, value); }, removeItem: (key: string) => { values.delete(key); } };
  const runtime = new AppRuntime({ defaultEndpoint: 'http://127.0.0.1:8080', storage, copy: async () => {}, openExternal: async () => {}, download: async () => {} });
  runtimes.push(runtime);
  const record = { draftId: 'saved', state: accepted ? 'accepted' : 'uncertain', create: { runtimeId: 'host' }, params: { cwd: '/project' } } as WelcomeSubmission;
  const list = vi.spyOn(runtime.welcome, 'list').mockReturnValue([record]);
  vi.spyOn(runtime.welcome, 'get').mockImplementation(id => id === record.draftId ? record : undefined);
  const complete = vi.spyOn(runtime.welcome, 'completeAccepted').mockResolvedValue('root');
  const report = vi.spyOn(runtime, 'report');
  const current = runtime.tabs.openNew({});
  const tree = (id = current.id) => <RuntimeContext.Provider value={runtime}><UIProvider><WelcomeRecovery currentId={id} /></UIProvider></RuntimeContext.Provider>;
  return { runtime, list, complete, report, tree, current };
}

it('keeps a saved-message list read failure local and retries it', async () => {
  const f = fixture(); f.list.mockImplementationOnce(() => { throw new Error('Recovery index unreadable'); });
  const view = render(f.tree());
  expect(view.container.querySelector('[data-error-type="resource"]')).not.toBeNull();
  expect(f.report).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole('button', { name: 'Retry', exact: true }));
  expect(await screen.findByRole('button', { name: /Recover first message/ })).toBeTruthy();
  expect(screen.queryByRole('alert')).toBeNull();
});

it('shows recovery action failures beside the saved request and clears them on retry', async () => {
  const f = fixture();
  const recover = vi.spyOn(f.runtime, 'recoverWelcome').mockImplementationOnce(() => { throw new Error('Close a tab before reopening this request'); });
  const view = render(f.tree());
  fireEvent.click(screen.getByRole('button', { name: /Recover first message/ }));
  expect(view.container.querySelector('[data-error-owner="recover:saved"]')).not.toBeNull();
  expect(f.report).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole('button', { name: /Recover first message/ }));
  await waitFor(() => expect(routing.navigate).toHaveBeenCalledOnce());
  expect(recover).toHaveBeenCalledTimes(2);
  expect(screen.queryByRole('alert')).toBeNull();
});

it('does not navigate or display a late completion error in a different recovery owner', async () => {
  const f = fixture(true);
  let reject!: (error: Error) => void;
  f.complete.mockReturnValue(new Promise((_resolve, fail) => { reject = fail; }));
  const view = render(f.tree());
  fireEvent.click(screen.getByRole('button', { name: /Recover first message/ }));
  routing.location = { href: '/new/other', state: { __TSR_key: 'other' } };
  view.rerender(f.tree('other'));
  await act(async () => reject(new Error('Old storage failure')));
  expect(screen.queryByRole('alert')).toBeNull();
  expect(routing.navigate).not.toHaveBeenCalled();
  expect(f.report).not.toHaveBeenCalled();
});

it('does not steal focus when another tab is selected while accepted recovery finishes', async () => {
  const f = fixture(true);
  let resolve!: (value: string) => void;
  f.complete.mockReturnValue(new Promise(done => { resolve = done; }));
  render(f.tree());
  fireEvent.click(screen.getByRole('button', { name: /Recover first message/ }));
  let other!: string;
  act(() => { other = f.runtime.tabs.openNew({}).id; });
  await act(async () => resolve('root'));
  expect(selectedSessionTab(f.runtime.tabs.workspace())?.id).toBe(other);
  expect(routing.navigate).not.toHaveBeenCalled();
});
