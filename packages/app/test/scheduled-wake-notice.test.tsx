import { act, fireEvent, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, expect, it, vi } from 'vitest';
import type { ComponentProps } from 'react';
import { ScheduledWakeNotice, wakeTimestamp } from '../src/scheduled-wake-notice';

type Props = ComponentProps<typeof ScheduledWakeNotice>;
const wake = { id: 11, next_fire: '2026-09-21T01:30:00Z', prompt: 'Continue the authorized work.\nKeep the existing draft.' };
const state = (root = {}, status: Props['state']['status'] = 'live') => ({
  status, root: { root_id: 'root', upcoming_schedules: [wake], upcoming_schedule_count: 1, ...root },
  history: {}, collections: {}, retainedBytes: 0, truncated: false, unavailable: false,
}) as Props['state'];
const props = (overrides: Partial<Props> = {}): Props => ({ state: state(), agentId: 'root', connected: true, onSchedules: vi.fn(), ...overrides });
const heading = () => screen.getByRole('button', { name: /scheduled to wake up|wake-up was due/ });
afterEach(() => vi.useRealTimers());

it('starts collapsed, toggles with Enter and Space, and leaves the focused header in place', async () => {
  const user = userEvent.setup();
  render(<ScheduledWakeNotice {...props()} />);
  const button = heading();
  expect(button.getAttribute('aria-expanded')).toBe('false');
  expect(screen.queryByText('Message to be sent')).toBeNull();
  await user.tab();
  expect(document.activeElement).toBe(button);
  await user.keyboard('{Enter}');
  expect(button.getAttribute('aria-expanded')).toBe('true');
  expect(document.getElementById(button.getAttribute('aria-controls')!)).toBeTruthy();
  expect(screen.getByText('Message to be sent')).toBeTruthy();
  expect(document.activeElement).toBe(button);
  await user.keyboard(' ');
  expect(button.getAttribute('aria-expanded')).toBe('false');
  expect(document.activeElement).toBe(button);
});

it('renders plain selectable prompt text, never HTML, markdown, or links', () => {
  const prompt = '<script>alert(1)</script>\n[click](https://example.com)\n' + 'x'.repeat(4096);
  const { container } = render(<ScheduledWakeNotice {...props({ state: state({ upcoming_schedules: [{ ...wake, prompt }] }) })} />);
  fireEvent.click(heading());
  const text = screen.getByRole('region', { name: 'Scheduled messages' });
  expect(text.textContent).toContain(prompt);
  expect(text.querySelector('script, a, pre')).toBeNull();
  expect(container.querySelector('time')?.getAttribute('datetime')).toBe(wake.next_fire);
  expect(container.querySelector('time')?.getAttribute('aria-label')).toMatch(/2026/);
  expect(text.getAttribute('tabindex')).toBe('0');
});

it('uses the viewer calendar day, timezone and full accessible timestamp', () => {
  const same = wakeTimestamp('2026-09-21T01:30:00Z', new Date('2026-09-20T23:00:00Z'), 'en-US', 'America/Denver');
  expect(same.label).toBe('7:30 PM MDT');
  expect(same.full).toContain('September 20, 2026');
  expect(same.overdue).toBe(false);
  const tomorrow = wakeTimestamp('2026-09-21T07:30:00Z', new Date('2026-09-20T23:00:00Z'), 'en-US', 'America/Denver');
  expect(tomorrow.label).toContain('Sep 21, 2026');
  expect(tomorrow.label).toContain('MDT');
  const winter = wakeTimestamp('2026-12-21T01:30:00Z', new Date('2026-12-21T02:00:00Z'), 'en-US', 'America/Denver');
  expect(winter.label).toBe('6:30 PM MST');
  expect(winter.overdue).toBe(true);
});

it('describes overdue unclaimed work without pretending it fired', () => {
  vi.useFakeTimers(); vi.setSystemTime(new Date('2026-09-21T02:00:00Z'));
  render(<ScheduledWakeNotice {...props()} />);
  expect(heading().textContent).toContain('Scheduled wake-up was due at');
  fireEvent.click(heading());
  expect(screen.getByText('Message to be sent')).toBeTruthy();
});


it('updates due wording from wall time alone without claiming or removing the fixed occurrence', () => {
  vi.useFakeTimers();
  vi.setSystemTime(new Date('2026-09-21T01:29:59.500Z'));
  const snapshot = state();
  const options = props({ state: snapshot });
  render(<ScheduledWakeNotice {...options} />);
  const button = heading();
  fireEvent.click(button);
  expect(button.textContent).toContain('This session is scheduled to wake up at');

  act(() => vi.advanceTimersByTime(1000));

  expect(heading()).toBe(button);
  expect(button.textContent).toContain('Scheduled wake-up was due at');
  expect(button.getAttribute('aria-expanded')).toBe('true');
  expect(screen.getByRole('region', { name: 'Scheduled messages' }).textContent).toContain(wake.prompt);
  expect(snapshot.root!.upcoming_schedules).toEqual([wake]);
  expect(options.onSchedules).not.toHaveBeenCalled();
});

it.each([0, 1])('updates calendar-day formatting across local midnight for day offset %s', dayOffset => {
  vi.useFakeTimers();
  const before = new Date(2026, 8, 20, 23, 59, 59, 500);
  const after = new Date(before.getTime() + 1000);
  const next_fire = new Date(2026, 8, 20 + dayOffset, dayOffset ? 0 : 23, 30).toISOString();
  vi.setSystemTime(before);
  const snapshot = state({ upcoming_schedules: [{ ...wake, next_fire }] });
  const { container } = render(<ScheduledWakeNotice {...props({ state: snapshot })} />);
  const time = container.querySelector('time')!;
  expect(time.textContent).toBe(wakeTimestamp(next_fire, before).label);
  expect(wakeTimestamp(next_fire, before).label).not.toBe(wakeTimestamp(next_fire, after).label);

  act(() => vi.advanceTimersByTime(1000));

  expect(time.textContent).toBe(wakeTimestamp(next_fire, after).label);
  expect(time.getAttribute('datetime')).toBe(next_fire);
});

it('pauses its display clock while hidden, refreshes immediately on return, and cleans up on unmount', () => {
  vi.useFakeTimers();
  vi.setSystemTime(new Date('2026-09-21T01:29:59.500Z'));
  const hidden = vi.spyOn(document, 'hidden', 'get').mockReturnValue(false);
  const add = vi.spyOn(document, 'addEventListener');
  const remove = vi.spyOn(document, 'removeEventListener');
  const { unmount } = render(<ScheduledWakeNotice {...props()} />);
  const button = heading();
  expect(vi.getTimerCount()).toBe(1);
  const listener = add.mock.calls.find(([type]) => type === 'visibilitychange')![1];

  hidden.mockReturnValue(true);
  fireEvent(document, new Event('visibilitychange'));
  expect(vi.getTimerCount()).toBe(0);
  act(() => vi.advanceTimersByTime(2000));
  expect(button.textContent).toContain('This session is scheduled to wake up at');

  hidden.mockReturnValue(false);
  fireEvent(document, new Event('visibilitychange'));
  expect(button.textContent).toContain('Scheduled wake-up was due at');
  expect(vi.getTimerCount()).toBe(1);
  fireEvent(document, new Event('visibilitychange'));
  expect(vi.getTimerCount()).toBe(1);

  unmount();
  expect(remove).toHaveBeenCalledWith('visibilitychange', listener);
  expect(vi.getTimerCount()).toBe(0);
  fireEvent(document, new Event('visibilitychange'));
  expect(vi.getTimerCount()).toBe(0);
});

it('starts a clock mounted while hidden only after returning to the visible document', () => {
  vi.useFakeTimers();
  vi.setSystemTime(new Date('2026-09-21T01:29:59.500Z'));
  const hidden = vi.spyOn(document, 'hidden', 'get').mockReturnValue(true);
  render(<ScheduledWakeNotice {...props()} />);
  expect(vi.getTimerCount()).toBe(0);
  act(() => vi.advanceTimersByTime(2000));
  hidden.mockReturnValue(false);
  fireEvent(document, new Event('visibilitychange'));
  expect(heading().textContent).toContain('Scheduled wake-up was due at');
  expect(vi.getTimerCount()).toBe(1);
});

it.each([
  { connected: false }, { agentId: 'child' },
  { state: state({}, 'stale') },
  { state: state({ upcoming_schedules: undefined }) },
  { state: state({ upcoming_schedules: [], upcoming_schedule_count: 0 }) },
  { state: state({ upcoming_schedules: [], upcoming_schedule_count: 2, omitted: { upcoming_schedules: true } }) },
])('retires the display clock without live occurrence details and restarts it fresh: %j', overrides => {
  vi.useFakeTimers();
  vi.setSystemTime(new Date('2026-09-21T01:29:59.500Z'));
  const options = props();
  const { rerender } = render(<ScheduledWakeNotice {...options} />);
  expect(vi.getTimerCount()).toBe(1);
  rerender(<ScheduledWakeNotice {...options} {...overrides} />);
  expect(vi.getTimerCount()).toBe(0);
  act(() => vi.advanceTimersByTime(2000));
  fireEvent(document, new Event('visibilitychange'));
  expect(vi.getTimerCount()).toBe(0);
  rerender(<ScheduledWakeNotice {...options} />);
  expect(heading().textContent).toContain('Scheduled wake-up was due at');
  expect(vi.getTimerCount()).toBe(1);
});

it('groups host-ordered occurrences and resets expansion for a new recurring slot without replacing the header', () => {
  const second = { ...wake, id: 12, next_fire: '2026-09-22T01:30:00Z', prompt: 'Second prompt' };
  const p = props({ state: state({ upcoming_schedules: [wake, second], upcoming_schedule_count: 2 }) });
  const view = render(<ScheduledWakeNotice {...p} />);
  const button = heading(); button.focus(); fireEvent.click(button);
  expect(button.textContent).toContain('+1 more');
  expect(screen.getByText('Second prompt')).toBeTruthy();
  view.rerender(<ScheduledWakeNotice {...p} state={state({ upcoming_schedules: [{ ...wake, next_fire: second.next_fire }] })} />);
  expect(heading()).toBe(button);
  expect(document.activeElement).toBe(button);
  expect(button.getAttribute('aria-expanded')).toBe('false');
});

it('returns focus from a replaced occurrence to its stable heading without jumping the page', () => {
  const p = props();
  const view = render(<ScheduledWakeNotice {...p} />);
  const button = heading(); fireEvent.click(button);
  screen.getByRole('region', { name: 'Scheduled messages' }).focus();
  view.rerender(<ScheduledWakeNotice {...p} state={state({ upcoming_schedules: [{ ...wake, next_fire: '2026-09-22T01:30:00Z' }] })} />);
  expect(document.activeElement).toBe(button);
  fireEvent.click(button);
  screen.getByRole('region', { name: 'Scheduled messages' }).focus();
  view.rerender(<ScheduledWakeNotice {...p} state={state({ upcoming_schedules: [], upcoming_schedule_count: 0 })} />);
  expect(screen.queryByRole('button')).toBeNull();
  expect(document.activeElement).toBe(document.body);
});

it('marks a 4096-byte preview and omitted rows with explicit schedule recovery, without eager reads', () => {
  const onSchedules = vi.fn();
  render(<ScheduledWakeNotice {...props({ onSchedules, state: state({
    upcoming_schedules: [{ ...wake, prompt: 'x'.repeat(4096), prompt_truncated: true }],
    upcoming_schedule_count: 7, omitted: { upcoming_schedules: true },
  }) })} />);
  expect(heading().textContent).toContain('+6 more');
  expect(onSchedules).not.toHaveBeenCalled();
  fireEvent.click(heading());
  expect(screen.getByText('Message preview')).toBeTruthy();
  expect(screen.queryByText('Message to be sent')).toBeNull();
  expect(screen.getByText(/Prompt preview is incomplete/)).toBeTruthy();
  expect(screen.getByText(/Partial schedule list/)).toBeTruthy();
  expect(onSchedules).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole('button', { name: 'Open schedules' }));
  expect(onSchedules).toHaveBeenCalledOnce();
});

it('does not infer exact totals from partial rows without a count', () => {
  render(<ScheduledWakeNotice {...props({ state: state({ upcoming_schedule_count: undefined, omitted: { upcoming_schedules: true } }) })} />);
  expect(heading().textContent).toContain('schedule details incomplete');
  expect(heading().textContent).not.toContain('+');
});

it.each([1, 7])('offers explicit recovery when all occurrence rows are omitted (count %s)', total => {
  const onSchedules = vi.fn();
  const { container } = render(<ScheduledWakeNotice {...props({ onSchedules, state: state({
    upcoming_schedules: [], upcoming_schedule_count: total, omitted: { upcoming_schedules: true },
  }) })} />);
  const button = screen.getByRole('button', { name: `This session has ${total} scheduled wake-up${total === 1 ? '' : 's'}` });
  expect(button.getAttribute('aria-expanded')).toBe('false');
  expect(container.querySelector('time')).toBeNull();
  expect(onSchedules).not.toHaveBeenCalled();
  fireEvent.click(button);
  expect(screen.queryByText('Message to be sent')).toBeNull();
  expect(screen.getByText(/Partial schedule list/)).toBeTruthy();
  fireEvent.click(screen.getByRole('button', { name: 'Open schedules' }));
  expect(onSchedules).toHaveBeenCalledOnce();
});

it.each([undefined, null, 0])('does not revive a fired notice from an empty partial projection with count %s', total => {
  const { container } = render(<ScheduledWakeNotice {...props({ state: state({
    upcoming_schedules: [], upcoming_schedule_count: total, omitted: { upcoming_schedules: true },
  }) })} />);
  expect(container.textContent).toBe('');
});

it.each(['idle', 'loading', 'stale', 'error', 'closed'] as const)('hides %s evidence', status => {
  const { container } = render(<ScheduledWakeNotice {...props({ state: state({}, status) })} />);
  expect(container.textContent).toBe('');
});
it.each([
  { connected: false }, { agentId: 'child' },
  { state: state({ upcoming_schedules: undefined }) },
  { state: state({ upcoming_schedules: null, upcoming_schedule_count: null }) },
  { state: state({ upcoming_schedules: [], upcoming_schedule_count: 0 }) },
])('hides disconnected, child, unsupported, and empty projections: %j', overrides => {
  const { container } = render(<ScheduledWakeNotice {...props(overrides)} />);
  expect(container.textContent).toBe('');
});
