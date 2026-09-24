import { act, fireEvent, render, screen } from '@testing-library/react';
import { expect, it } from 'vitest';
import type { ConnectionSnapshot, WhipClient } from '@whip/sdk';
import { ConnectionNotice } from '../src/connection-notice';

function fixture() {
  let snapshot: ConnectionSnapshot = { state: 'connected' };
  const listeners = new Set<() => void>();
  const client = {
    getSnapshot: () => snapshot,
    subscribe: (listener: () => void) => { listeners.add(listener); return () => listeners.delete(listener); },
  } as WhipClient;
  const view = render(<ConnectionNotice client={client} />);
  return {
    ...view,
    listeners,
    update(next: ConnectionSnapshot) {
      act(() => { snapshot = next; for (const listener of listeners) listener(); });
    },
  };
}

it('shows a connection failure and removes it when the same client recovers', () => {
  const f = fixture();
  f.update({ state: 'reconnecting', error: new Error('WebSocket connection failed') });
  expect(screen.getByRole('alert').textContent).toContain('WebSocket connection failed');
  f.update({ state: 'connected' });
  expect(screen.queryByRole('alert')).toBeNull();
  f.unmount();
  expect(f.listeners.size).toBe(0);
});

it('allows dismissal without hiding a later failure or incompatibility', () => {
  const f = fixture();
  const error = new Error('WebSocket connection failed');
  f.update({ state: 'reconnecting', error });
  fireEvent.click(screen.getByRole('button', { name: 'Dismiss host error' }));
  expect(screen.queryByRole('alert')).toBeNull();
  f.update({ state: 'incompatible', error: new Error('Unsupported protocol') });
  expect(screen.getByRole('alert').textContent).toContain('Unsupported protocol');
});
