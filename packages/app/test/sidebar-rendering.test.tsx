import { render, screen, waitFor } from '@testing-library/react';
import { QueryClientProvider } from '@tanstack/react-query';
import { ThemeProvider, UIProvider } from '@whip/ui';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import type { AnchorHTMLAttributes, ReactNode } from 'react';
import { providerFixture } from './provider-fixture';
import { RuntimeContext } from '../src/context';
import { SessionSidebar } from '../src/session-sidebar';
import { SessionActionsProvider } from '../src/session-actions';
import { SessionTabs } from '../src/session-tabs';
import { emptySidebarState } from '../src/sidebar-state';
import type { HostConnection } from '../src/hosts';

vi.hoisted(() => { globalThis.ResizeObserver = class { observe() {} unobserve() {} disconnect() {} }; });
vi.mock('@tanstack/react-router', () => ({
  useLocation: () => ({ pathname: '/' }), useNavigate: () => vi.fn(),
  Link: ({ children, params, search: _search, state: _state, preload: _preload, to, ...props }: AnchorHTMLAttributes<HTMLAnchorElement> & { children: ReactNode; params?: { runtimeId: string; rootId: string }; search: unknown; state: unknown; preload: unknown; to: string }) =>
    <a {...props} href={params ? `/h/${params.runtimeId}/s/${params.rootId}` : to}>{children}</a>,
}));
// Deliberately retain the real virtualizer. measure() schedules a render, unlike
// the navigation tests' no-op geometry fixture; unstable rows would loop here.
beforeEach(() => vi.stubGlobal('matchMedia', () => ({ matches: false, addEventListener() {}, removeEventListener() {} })));
afterEach(() => vi.unstubAllGlobals());

it.each(['empty', 'failed'] as const)('settles an %s Projects read with the real virtualizer', async kind => {
  const f = await providerFixture();
  f.data.handlers['trees.recent'] = () => {
    if (kind === 'failed') throw new Error('Fixture recent read failed');
    return { catalog_revision: '1', items: [], has_more: false };
  };
  const host = { id: 'local', name: 'Local', runtimeId: 'host', local: true, state: 'connected', client: f.client } as HostConnection;
  const state = { hosts: [host] };
  f.runtime.connections.host = () => host;
  Object.assign(f.runtime, { subscribe: () => () => {}, getSnapshot: () => state, tabs: new SessionTabs() });
  const view = render(<RuntimeContext.Provider value={f.runtime}><QueryClientProvider client={f.queries}><ThemeProvider initialTheme="light"><UIProvider><SessionActionsProvider>
    <SessionSidebar state={emptySidebarState()} setState={vi.fn()} onSearch={vi.fn()} onConnect={vi.fn()} onNavigate={vi.fn()} />
  </SessionActionsProvider></UIProvider></ThemeProvider></QueryClientProvider></RuntimeContext.Provider>);
  try {
    await screen.findByText(kind === 'empty' ? 'No saved sessions yet.' : 'Could not load sessions · Local');
    await waitFor(() => expect(f.count('trees.recent')).toBe(1));
    expect(screen.getByRole('region', { name: 'Projects' })).toBeTruthy();
  } finally { view.unmount(); f.queries.clear(); }
});
