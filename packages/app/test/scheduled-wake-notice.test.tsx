import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
} from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import type { ComponentProps } from 'react';
import {
  ScheduledWakeDisplay,
  ScheduledWakeNotice,
  wakeTimestamp,
} from '../src/scheduled-wake-notice';
import { providerFixture } from './provider-fixture';

type Props = ComponentProps<typeof ScheduledWakeDisplay>;
const wake: Props['wakes'][number] = {
  id: 'schedule',
  session_id: 'root',
  expression: '2026-09-21T01:30:00Z',
  first_due: '2026-09-21T01:30:00Z',
  next_due: '2026-09-21T01:30:00Z',
  cancelled_at: null,
  failure: null,
  created_at: '2026-09-20T00:00:00Z',
  parts_bytes: '50',
  preview: 'Continue the authorized work.\nKeep the existing draft.',
  preview_truncated: false,
  latest: null,
};
const props = (overrides: Partial<Props> = {}): Props => ({
  owner: 'root',
  wakes: [wake],
  partial: false,
  onSchedules: vi.fn(),
  ...overrides,
});
const heading = () =>
  screen.getByRole('button', { name: /scheduled to wake up|wake-up was due/ });
beforeEach(() =>
  vi.stubGlobal('matchMedia', () => ({
    matches: false,
    addEventListener() {},
    removeEventListener() {},
  })),
);
afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

it('starts collapsed and toggles with Enter and Space without replacing the focused header', async () => {
  const user = userEvent.setup();
  render(<ScheduledWakeDisplay {...props()} />);
  const button = heading();
  expect(button.getAttribute('aria-expanded')).toBe('false');
  await user.tab();
  expect(document.activeElement).toBe(button);
  await user.keyboard('{Enter}');
  expect(button.getAttribute('aria-expanded')).toBe('true');
  expect(screen.getByText('Message to be sent')).toBeTruthy();
  await user.keyboard(' ');
  expect(button.getAttribute('aria-expanded')).toBe('false');
  expect(document.activeElement).toBe(button);
});
it('renders plain selectable preview text and an accessible timestamp without interpreting markup', () => {
  const preview = '<script>alert(1)</script>\n[click](https://example.com)';
  const view = render(
    <ScheduledWakeDisplay {...props({ wakes: [{ ...wake, preview }] })} />,
  );
  fireEvent.click(heading());
  const region = screen.getByRole('region', { name: 'Scheduled messages' });
  expect(region.textContent).toContain(preview);
  expect(region.querySelector('script,a')).toBeNull();
  expect(view.container.querySelector('time')?.getAttribute('datetime')).toBe(
    wake.next_due,
  );
  expect(region.tabIndex).toBe(0);
});
it('uses viewer calendar day and timezone and distinguishes overdue from executed', () => {
  const same = wakeTimestamp(
    '2026-09-21T01:30:00Z',
    new Date('2026-09-20T23:00:00Z'),
    'en-US',
    'America/Denver',
  );
  expect(same.label).toBe('7:30 PM MDT');
  expect(same.full).toContain('September 20, 2026');
  expect(same.overdue).toBe(false);
  expect(
    wakeTimestamp(
      '2026-09-21T07:30:00Z',
      new Date('2026-09-20T23:00:00Z'),
      'en-US',
      'America/Denver',
    ).label,
  ).toContain('Sep 21, 2026');
  expect(
    wakeTimestamp(
      '2026-12-21T01:30:00Z',
      new Date('2026-12-21T02:00:00Z'),
      'en-US',
      'America/Denver',
    ),
  ).toMatchObject({ label: '6:30 PM MST', overdue: true });
});
it('local clock updates due wording without claiming, deleting or replacing an occurrence', () => {
  vi.useFakeTimers();
  vi.setSystemTime(new Date('2026-09-21T01:29:59.500Z'));
  const input = props();
  render(<ScheduledWakeDisplay {...input} />);
  const button = heading();
  fireEvent.click(button);
  act(() => vi.advanceTimersByTime(1000));
  expect(heading()).toBe(button);
  expect(button.textContent).toContain('was due');
  expect(button.getAttribute('aria-expanded')).toBe('true');
  expect(input.wakes).toEqual([wake]);
  expect(input.onSchedules).not.toHaveBeenCalled();
});
it.each([0, 1])(
  'refreshes viewer calendar labels across midnight for day offset %s',
  (offset) => {
    vi.useFakeTimers();
    const before = new Date(2026, 8, 20, 23, 59, 59, 500);
    vi.setSystemTime(before);
    const next_due = new Date(
      2026,
      8,
      20 + offset,
      offset ? 0 : 23,
      30,
    ).toISOString();
    const view = render(
      <ScheduledWakeDisplay {...props({ wakes: [{ ...wake, next_due }] })} />,
    );
    expect(view.container.querySelector('time')?.textContent).toBe(
      wakeTimestamp(next_due, before).label,
    );
    act(() => vi.advanceTimersByTime(1000));
    expect(view.container.querySelector('time')?.textContent).toBe(
      wakeTimestamp(next_due, new Date(before.getTime() + 1000)).label,
    );
  },
);
it('pauses hidden clocks, resumes visible clocks and joins cleanup', () => {
  vi.useFakeTimers();
  const hidden = vi.spyOn(document, 'hidden', 'get').mockReturnValue(false);
  const view = render(<ScheduledWakeDisplay {...props()} />);
  expect(vi.getTimerCount()).toBe(1);
  hidden.mockReturnValue(true);
  act(() => document.dispatchEvent(new Event('visibilitychange')));
  expect(vi.getTimerCount()).toBe(0);
  hidden.mockReturnValue(false);
  act(() => document.dispatchEvent(new Event('visibilitychange')));
  expect(vi.getTimerCount()).toBe(1);
  view.unmount();
  expect(vi.getTimerCount()).toBe(0);
});
it('marks partial lists and truncated previews without claiming a complete total', () => {
  const input = props({
    partial: true,
    wakes: [{ ...wake, preview_truncated: true }],
  });
  render(<ScheduledWakeDisplay {...input} />);
  expect(heading().textContent).toContain('details incomplete');
  fireEvent.click(heading());
  expect(screen.getByText('Message preview')).toBeTruthy();
  fireEvent.click(screen.getByRole('button', { name: 'Open schedules' }));
  expect(input.onSchedules).toHaveBeenCalledOnce();
});
it('collapses a replaced occurrence and returns focus from removed details', () => {
  const input = props();
  const view = render(<ScheduledWakeDisplay {...input} />);
  const button = heading();
  fireEvent.click(button);
  act(() => screen.getByRole('region', { name: 'Scheduled messages' }).focus());
  view.rerender(
    <ScheduledWakeDisplay
      {...props({ wakes: [{ ...wake, next_due: '2026-10-01T00:00:00Z' }] })}
    />,
  );
  expect(heading()).toBe(button);
  expect(button.getAttribute('aria-expanded')).toBe('false');
  expect(document.activeElement).toBe(button);
});
it('reads only bounded native upcoming metadata for the exact owner and hides stale disconnected times', async () => {
  const f = await providerFixture();
  f.data.handlers['schedules.list'] = () => ({
    items: [wake],
    next_cursor: null,
    next_after: null,
  });
  const session = f.client.session('root'),
    input = (
      <ScheduledWakeNotice session={session} connected onSchedules={vi.fn()} />
    );
  const view = f.mount(input);
  await waitFor(() => expect(heading()).toBeTruthy());
  expect(
    f.calls.find((call) => call.method === 'schedules.list')?.params,
  ).toEqual({ session_id: 'root', upcoming: true, limit: 16 });
  view.rerender(
    f.wrap(
      <ScheduledWakeNotice
        session={session}
        connected={false}
        onSchedules={vi.fn()}
      />,
    ),
  );
  expect(
    screen.queryByRole('region', { name: 'Scheduled wake-ups' }),
  ).toBeNull();
  expect(f.count('schedules.get')).toBe(0);
  expect(f.count('sessions.submit')).toBe(0);
});
