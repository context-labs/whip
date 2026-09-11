import { fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

beforeEach(() => {
  vi.stubGlobal('matchMedia', () => ({
    matches: false,
    addEventListener() {},
    removeEventListener() {},
  }));
});
afterEach(() => vi.unstubAllGlobals());
import type { Session } from '@whip/sdk';
import type { SessionView } from '@whip/sdk/state';
import type { RootSnapshot } from '@whip/protocol';
import { ThemeProvider, UIProvider } from '@whip/ui';
import { RuntimeContext } from '../src/context';
import type { AppRuntime } from '../src/runtime';
import { PermissionModePicker } from '../src/permission-mode';

function fixture(permissionMode: string, agentId = 'root', activeTurns: Record<string, string> = {}) {
  const setPermissionMode = vi.fn(() => ({}));
  const session = { rootId: 'root', setPermissionMode } as unknown as Session;
  const root = {
    root_id: 'root',
    permission_mode: permissionMode,
    active_turns: activeTurns,
    meta: { id: 'root', model: 'model', provider: 'provider' },
  } as unknown as RootSnapshot;
  const run = vi.fn(async () => ({}));
  const runtime = { run, report: vi.fn() } as unknown as AppRuntime;
  const view = { session } as unknown as SessionView;
  const ui = (
    <RuntimeContext.Provider value={runtime}>
      <ThemeProvider>
        <UIProvider>
          <PermissionModePicker view={view} root={root} connected agentId={agentId} />
        </UIProvider>
      </ThemeProvider>
    </RuntimeContext.Provider>
  );
  return { setPermissionMode, run, runtime, ui };
}

describe('PermissionModePicker', () => {
  it('shows the current mode and applies the other mode on selection', async () => {
    const f = fixture('prompt');
    render(f.ui);
    fireEvent.click(screen.getByRole('button', { name: 'Permission approval mode' }));
    const option = await screen.findByRole('option', { name: /Full Access/ });
    expect(option.textContent).toContain('Access files outside this project and approve actions automatically');
    expect(screen.getByRole('option', { name: /Ask for approval/ }).getAttribute('aria-selected')).toBe('true');
    fireEvent.click(option);
    expect(f.setPermissionMode).toHaveBeenCalledWith(false);
    expect(f.run).toHaveBeenCalledWith(expect.anything(), 'Enable Full Access');
  });

  it('labels automatic mode in the trigger and can return to prompts', async () => {
    const f = fixture('automatic');
    render(f.ui);
    expect(screen.getByRole('button', { name: 'Permission approval mode' }).textContent).toContain('Full Access');
    fireEvent.click(screen.getByRole('button', { name: 'Permission approval mode' }));
    fireEvent.click(await screen.findByRole('option', { name: /Ask for approval/ }));
    expect(f.setPermissionMode).toHaveBeenCalledWith(true);
  });

  it('is root-only and waits for idle', () => {
    const child = fixture('prompt', 'root:child');
    const { container } = render(child.ui);
    expect(container.firstChild).toBeNull();
    const busy = fixture('prompt', 'root', { root: 'turn' });
    render(busy.ui);
    expect(screen.getByRole('button', { name: 'Permission approval mode' })).toHaveProperty('disabled', true);
  });
});

it('keeps a failed mode change at the picker and clears it after retry', async () => {
  const f = fixture('prompt');
  f.run.mockRejectedValueOnce(new Error('Mode change rejected'));
  render(f.ui);
  fireEvent.click(screen.getByRole('button', { name: 'Permission approval mode' }));
  fireEvent.click(await screen.findByRole('option', { name: /Full Access/ }));
  const alert = await screen.findByRole('alert');
  expect(alert.closest('[data-error-type]')?.getAttribute('data-error-type')).toBe('action');
  expect(alert.textContent).toContain('Mode change rejected');
  expect(f.runtime.report).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole('option', { name: /Full Access/ }));
  await screen.findByRole('button', { name: 'Permission approval mode' });
});
