import type { ReactNode } from 'react';
import type { LocalRuntimeStatus } from '../src/platform';
import { fireEvent, render, screen } from '@testing-library/react';
import { expect, it, vi } from 'vitest';
import { SessionSidebar } from '../src/session-sidebar';
import { emptySidebarState } from '../src/sidebar-state';

vi.hoisted(() => { globalThis.ResizeObserver = class { observe() {} unobserve() {} disconnect() {} }; });

const app = vi.hoisted(() => ({ hosts: [] as { id: string; name: string; state: string; endpoint: string; localRuntime?: LocalRuntimeStatus }[] }));
vi.mock('../src/context', () => ({
  useAppState: () => app,
  useRuntime: () => ({}),
}));
vi.mock('@tanstack/react-router', () => ({
  Link: ({ children }: { children: ReactNode }) => <a>{children}</a>,
  useNavigate: () => vi.fn(),
}));

const local = { id: 'local', name: 'Local', state: 'closed', endpoint: 'http://localhost' };
const remote = { id: 'remote', name: 'Remote', state: 'closed', endpoint: 'https://remote.example' };
const sidebar = () => <SessionSidebar state={emptySidebarState()} setState={vi.fn()}
  onSearch={vi.fn()} onConnect={vi.fn()} onNavigate={vi.fn()} />;

it('omits the single-server heading without hiding its content or server management', () => {
  app.hosts = [local];
  render(sidebar());
  expect(screen.queryByRole('button', { name: /^Local\s*Offline$/ })).toBeNull();
  expect(screen.getByRole('button', { name: 'Connect Local' })).toBeTruthy();
  expect(screen.getByRole('button', { name: 'Manage servers' })).toBeTruthy();
});

it.each([false, true])('omits redundant local setup actions while keeping remote connections available (repairRequired=%s)', repairRequired => {
  app.hosts = [{ ...local, localRuntime: { state: 'missing', home: '/tmp/whip-test', message: 'No installation.', canInstall: true, repairRequired } }, remote];
  render(sidebar());
  expect(screen.queryByRole('button', { name: 'Set up this Mac' })).toBeNull();
  expect(screen.queryByRole('button', { name: 'Repair this Mac' })).toBeNull();
  expect(screen.queryByRole('button', { name: 'Connect Local' })).toBeNull();
  expect(screen.getByRole('button', { name: 'Connect Remote' })).toBeTruthy();
  expect(screen.getByRole('button', { name: 'Manage servers' })).toBeTruthy();
});

it('keeps multiple servers collapsible and reveals a collapsed server when it becomes the only one', () => {
  app.hosts = [local, remote];
  const { rerender } = render(sidebar());
  const heading = screen.getByRole('button', { name: /^Local\s*Offline$/ });
  expect(screen.getByRole('button', { name: /^Remote\s*Offline$/ })).toBeTruthy();
  fireEvent.click(heading);
  expect(heading.getAttribute('aria-expanded')).toBe('false');
  expect(screen.queryByRole('button', { name: 'Connect Local' })).toBeNull();
  expect(screen.getByRole('button', { name: 'Connect Remote' })).toBeTruthy();
  app.hosts = [local];
  rerender(sidebar());
  expect(screen.queryByRole('button', { name: /^Local\s*Offline$/ })).toBeNull();
  expect(screen.getByRole('button', { name: 'Connect Local' })).toBeTruthy();
});
