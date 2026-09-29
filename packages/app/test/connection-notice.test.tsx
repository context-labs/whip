import { fireEvent, render, screen } from '@testing-library/react';
import { expect, it } from 'vitest';
import type { HostConnection } from '../src/hosts';
import { HostNotice } from '../src/connection-notice';

function fixture() {
  const host = { id: 'host', name: 'Server', state: 'connected' } as HostConnection;
  const view = render(<HostNotice host={host} onManage={() => {}} />);
  return { ...view, update(next: Pick<HostConnection, 'state' | 'error'>) {
    view.rerender(<HostNotice host={{ ...host, ...next }} onManage={() => {}} />);
  } };
}

it('shows a connection failure and removes it when the same host recovers', () => {
  const f = fixture();
  f.update({ state: 'connecting', error: 'WebSocket connection failed' });
  expect(screen.getByRole('alert').textContent).toContain('WebSocket connection failed');
  f.update({ state: 'connected' });
  expect(screen.queryByRole('alert')).toBeNull();
  f.unmount();
});

it('allows dismissal without hiding a later failure or incompatibility', () => {
  const f = fixture();
  const error = 'WebSocket connection failed';
  f.update({ state: 'connecting', error });
  fireEvent.click(screen.getByRole('button', { name: 'Dismiss host error' }));
  expect(screen.queryByRole('alert')).toBeNull();
  f.update({ state: 'closed', error: 'Unsupported protocol' });
  expect(screen.getByRole('alert').textContent).toContain('Unsupported protocol');
});
