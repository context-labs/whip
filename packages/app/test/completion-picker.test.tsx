import { act, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { CompletionPicker } from '../src/completion-picker';
import userEvent from '@testing-library/user-event';
import { providerFixture } from './provider-fixture';

beforeEach(() => { vi.stubGlobal('matchMedia', () => ({ matches: false, addEventListener() {}, removeEventListener() {} })); vi.stubGlobal('ResizeObserver', class { observe() {} disconnect() {} }); });
afterEach(() => vi.unstubAllGlobals());

it('inserts native host file metadata for the exact selected child without admitting work', async () => {
  const user = userEvent.setup();
  const f = await providerFixture();
  f.data.handlers['workspace.complete'] = () => ({ working_directory: '/child', candidates: [{ text: '@file.ts', description: '' }], truncated: false });
  const select = vi.fn(), close = vi.fn();
  f.mount(<CompletionPicker session={f.client.session('child')} connected onSelect={select} onClose={close} />);
  await waitFor(() => expect(f.count('workspace.complete')).toBe(1));
  expect(f.calls.at(-1)?.params).toEqual({ session_id: 'child', kind: 'mention', prefix: '', limit: 32 });
  await user.click(screen.getByRole('combobox', { name: 'Search on the host' }));
  await user.click(await screen.findByRole('option', { name: '@file.ts' }));
  expect(select).toHaveBeenCalledExactlyOnceWith('@file.ts'); expect(close).toHaveBeenCalledOnce();
  expect(f.calls.every(call => ['initialize', 'workspace.complete'].includes(call.method))).toBe(true);
});

it('switches to native skill metadata and omits disabled skills', async () => {
  const user = userEvent.setup();
  const f = await providerFixture();
  f.data.handlers['workspace.complete'] = () => ({ working_directory: '/child', candidates: [], truncated: false });
  const source = { kind: 'skill_metadata', scope: 'project', root_id: null, path: '/project/SKILL.md', bytes: '1', sha256: 'a'.repeat(64) };
  f.data.handlers['skills.list'] = () => ({ items: [{ name: 'active', description: 'Available', disabled: false, source }, { name: 'disabled', description: 'Disabled', disabled: true, source }], next_after: null });
  const select = vi.fn(); f.mount(<CompletionPicker session={f.client.session('child')} connected onSelect={select} onClose={() => {}} />);
  await user.click(screen.getByRole('combobox', { name: 'Context type' }));
  await user.click(await screen.findByRole('option', { name: 'Skill', exact: true }));
  await waitFor(() => expect(f.count('skills.list')).toBe(1));
  await user.click(screen.getByRole('combobox', { name: 'Search on the host' }));
  await user.click(await screen.findByRole('option', { name: /\$active/ }));
  expect(select).toHaveBeenCalledWith('$active'); expect(screen.queryByText('$disabled')).toBeNull();
  expect(f.calls.find(call => call.method === 'skills.list')?.params).toEqual({ session_id: 'child', prefix: '', limit: 32 });
});

it('does not expose a late old-owner result after changing the selected session or going offline', async () => {
  const user = userEvent.setup();
  const f = await providerFixture(); let finish!: (value: unknown) => void;
  f.data.handlers['workspace.complete'] = request => request.method === 'workspace.complete' && request.params.session_id === 'old'
    ? new Promise(resolve => { finish = resolve; })
    : { working_directory: '/new', candidates: [{ text: '@new', description: '' }], truncated: false };
  const select = vi.fn(), app = (id: string, connected = true) => <CompletionPicker session={f.client.session(id)} connected={connected} onSelect={select} onClose={() => {}} />;
  const mounted = f.mount(app('old')); await waitFor(() => expect(f.count('workspace.complete')).toBe(1));
  mounted.rerender(f.wrap(app('new')));
  await act(async () => finish({ working_directory: '/old', candidates: [{ text: '@old', description: '' }], truncated: false }));
  await user.click(screen.getByRole('combobox', { name: 'Search on the host' }));
  await screen.findByRole('option', { name: '@new' }); expect(screen.queryByText('@old')).toBeNull();
  mounted.rerender(f.wrap(app('new', false)));
  expect(screen.getByRole('combobox', { name: 'Search on the host' })).toHaveProperty('disabled', true);
  expect(select).not.toHaveBeenCalled(); expect(f.count('workspace.complete')).toBe(2);
});
