import { fireEvent, render, screen } from '@testing-library/react';
import { expect, it, vi } from 'vitest';
import { ErrorNotice } from '../src/error-feedback';

it('keeps technical diagnostics collapsed and recovery at the owning surface', () => {
  const retry = vi.fn();
  const { container } = render(<ErrorNotice type="resource" owner="files:session-a" error={new Error('ENOENT /private/runtime.sock')}
    action={<button onClick={retry}>Retry</button>} />);
  expect(screen.getByRole('alert').textContent).toContain('This content could not load');
  expect(container.querySelector('details')?.open).toBe(false);
  expect(container.querySelector('[data-error-owner="files:session-a"]')).not.toBeNull();
  fireEvent.click(screen.getByRole('button', { name: 'Retry' }));
  expect(retry).toHaveBeenCalledOnce();
});

it('shows field validation directly and silently ignores observer cancellation', () => {
  const view = render(<ErrorNotice type="validation" owner="server-url" error="Enter a valid server URL." />);
  expect(screen.getByText('Enter a valid server URL.')).toBeTruthy();
  expect(view.container.querySelector('details')).toBeNull();
  view.rerender(<ErrorNotice type="resource" owner="server-url" error={new DOMException('Disposed', 'AbortError')} />);
  expect(screen.queryByRole('alert')).toBeNull();
});
