import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
} from '@testing-library/react';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { UIProvider } from '@whip/ui';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { Session } from '@whip/sdk';
import { Composer } from '../src/composer';
import { RuntimeContext } from '../src/context';
import type { AppRuntime, CommandNotice } from '../src/runtime';
import type { ComponentProps } from 'react';
import { CompositionStore } from '../src/compositions';
import { SubmittedInputs } from '../src/input-presentation';
import { SessionTabs } from '../src/session-tabs';

beforeEach(() => {
  vi.stubGlobal('ResizeObserver', class {
    observe() {}
    disconnect() {}
  });
  vi.stubGlobal('matchMedia', vi.fn(() => ({ matches: true })));
});
afterEach(() => vi.unstubAllGlobals());

function fixture(catalog = false) {
  const onAccepted = vi.fn();
  const drafts = new Map<string, string>([
    ['runtime:root:a', 'same draft'],
    ['runtime:root:b', 'same draft'],
  ]);
  const draftListeners = new Map<string, Set<() => void>>();
  const waits: { accepted(): void; finish(): void; reject(error: Error): void }[] = [];
  const snapshot = { commands: [] as CommandNotice[], endpoint: 'http://localhost' };
  const runtime = {
    compositions: new CompositionStore(),
    submittedInputs: new SubmittedInputs(),
    tabs: new SessionTabs(),
    getSnapshot: () => snapshot,
    subscribe: () => () => {},
    draft: (key: string) => drafts.get(key) ?? '',
    setDraft: (key: string, text: string) => { drafts.set(key, text); draftListeners.get(key)?.forEach(fn => fn()); },
    subscribeDraft: (key: string, fn: () => void) => { const listeners = draftListeners.get(key) ?? new Set(); listeners.add(fn); draftListeners.set(key, listeners); return () => { listeners.delete(fn); }; },
    report: vi.fn(),
    run: (_handle: unknown, _label: string, accepted: () => void) =>
      new Promise((resolve, reject) => waits.push({ accepted, reject, finish: () => resolve({ result: { inbox_seq: String(waits.length) } }) })),
  } as unknown as AppRuntime;
  const connection = { state: 'connected', info: { runtime_id: 'runtime', negotiated_capabilities: ['workspace_completion', ...(catalog ? ['skill_catalog_completion'] : [])] } };
  const session = {
    rootId: 'root',
    client: { clientId: 'composer-test', getSnapshot: () => connection, subscribe: () => () => {}, call: vi.fn(async () => ({ candidates: [{ text: '$ponytail', description: 'Least code that works.' }] })) },
    command: vi.fn(() => ({})),
    submit: vi.fn(() => ({})),
  } as unknown as Session;
  const queries = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0, staleTime: 10_000 } } });
  const app = (agentId: string, viewId?: string, active = false, lastTurn?: ComponentProps<typeof Composer>['lastTurn'], extra: Partial<ComponentProps<typeof Composer>> = {}) => (
    <RuntimeContext.Provider value={runtime}>
      <QueryClientProvider client={queries}><UIProvider>
        <Composer
          key={viewId ?? agentId}
          viewId={viewId}
          session={session}
          agentId={agentId}
          connected
          runtimeId="runtime"
          active={active}
          lastTurn={lastTurn}
          onAccepted={onAccepted}
          {...extra}
        />
      </UIProvider></QueryClientProvider>
    </RuntimeContext.Provider>
  );
  return { drafts, waits, app, session, runtime, snapshot, onAccepted };
}

it('keeps the wake notice, agent dock, and queue in the composer region in that order', () => {
  const f = fixture();
  render(f.app('root', undefined, false, undefined, {
    notice: <div data-testid="wake-slot">Upcoming wake</div>,
    agents: <div data-testid="agent-slot">Agents</div>,
    queue: <div data-testid="queue-slot">Queue</div>,
  }));
  const wake = screen.getByTestId('wake-slot');
  const agents = screen.getByTestId('agent-slot');
  const queue = screen.getByTestId('queue-slot');
  const input = screen.getByRole('textbox');
  expect(wake.parentElement).toBe(agents.parentElement);
  expect(wake.parentElement).toBe(queue.parentElement);
  expect(wake.parentElement?.contains(input)).toBe(true);
  expect(wake.compareDocumentPosition(agents) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  expect(agents.compareDocumentPosition(queue) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  expect(queue.compareDocumentPosition(input) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
});

it.each(['root', 'a'])('notifies only the sending composer on admission for %s, including queued messages', async agentId => {
  const f = fixture();
  f.runtime.setDraft(`runtime:root:${agentId}`, 'Send this');
  render(f.app(agentId, undefined, false, undefined, { queueEnabled: true, activeTurn: 'turn' }));
  fireEvent.click(screen.getByRole('button', { name: 'Queue message' }));
  expect(f.onAccepted).not.toHaveBeenCalled();
  await act(async () => { f.waits[0]!.accepted(); });
  expect(f.onAccepted).toHaveBeenCalledTimes(1);
  await act(async () => { f.waits[0]!.finish(); });
  expect(f.onAccepted).toHaveBeenCalledTimes(1);
});

it('does not request a scroll on rejection or after the sending composer unmounts', async () => {
  const f = fixture();
  const mounted = render(f.app('a'));
  fireEvent.click(screen.getByRole('button', { name: 'Send message' }));
  await act(async () => { f.waits[0]!.reject(new Error('Rejected')); });
  expect(f.onAccepted).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole('button', { name: 'Send message' }));
  mounted.rerender(f.app('b'));
  await act(async () => { f.waits[1]!.accepted(); f.waits[1]!.finish(); });
  expect(f.onAccepted).not.toHaveBeenCalled();
});

it('replaces the send icon with one spinner until submission finishes', async () => {
  const f = fixture();
  render(f.app('a'));
  const send = screen.getByRole('button', { name: 'Send message' }) as HTMLButtonElement;
  expect(send.querySelector('.lucide-arrow-up')).not.toBeNull();
  fireEvent.click(send);
  expect(send.getAttribute('aria-busy')).toBe('true');
  expect(send.disabled).toBe(true);
  expect(send.querySelectorAll('svg')).toHaveLength(1);
  expect(send.querySelector('[aria-label="Loading"]')).not.toBeNull();
  expect(send.querySelector('.lucide-arrow-up')).toBeNull();
  await act(async () => { f.waits[0]!.accepted(); f.waits[0]!.finish(); });
  expect(send.getAttribute('aria-busy')).toBeNull();
  expect(send.querySelectorAll('svg')).toHaveLength(1);
  expect(send.querySelector('.lucide-arrow-up')).not.toBeNull();
  expect(send.querySelector('[aria-label="Loading"]')).toBeNull();
});

it('focuses the composer when its desktop chat view becomes active', async () => {
  const f = fixture();
  const rendered = render(f.app('a', 'one'));
  const input = screen.getByLabelText('Message this agent');
  expect(document.activeElement).not.toBe(input);
  rendered.rerender(f.app('a', 'one', true));
  await waitFor(() => expect(document.activeElement).toBe(input));
});

it('does not summon the composer on compact screens', async () => {
  vi.mocked(window.matchMedia).mockReturnValue({ matches: false } as MediaQueryList);
  const f = fixture();
  render(f.app('a', 'one', true));
  const input = screen.getByLabelText('Message this agent');
  await act(async () => { await new Promise(resolve => requestAnimationFrame(resolve)); });
  expect(document.activeElement).not.toBe(input);
});

it('does not move focus out of an open overlay', async () => {
  const f = fixture();
  const overlay = document.createElement('div');
  overlay.setAttribute('role', 'dialog');
  const button = document.createElement('button');
  overlay.append(button);
  document.body.append(overlay);
  button.focus();
  render(f.app('a', 'one', true));
  await act(async () => { await new Promise(resolve => requestAnimationFrame(resolve)); });
  expect(document.activeElement).toBe(button);
  overlay.remove();
});

it('a late acceptance for one recipient cannot clear another recipient’s identical draft', async () => {
  const f = fixture();
  const rendered = render(f.app('a'));
  fireEvent.click(screen.getByRole('button', { name: 'Send message' }));
  expect(f.waits).toHaveLength(1);
  // The message is available to every view before the host acknowledges it.
  expect(f.runtime.submittedInputs.getSnapshot()).toMatchObject([
    { runtimeId: 'runtime', rootId: 'root', agentId: 'a', text: 'same draft', accepted: false },
  ]);
  rendered.rerender(f.app('b'));
  act(() => f.waits[0]!.accepted());
  expect(
    (screen.getByLabelText('Message this agent') as HTMLTextAreaElement).value,
  ).toBe('same draft');
  expect(f.drafts.get('runtime:root:a')).toBe('');
  expect(f.drafts.get('runtime:root:b')).toBe('same draft');
  await act(async () => f.waits[0]!.finish());
});
it('an older terminal outcome cannot unlock a newer submission awaiting acceptance', async () => {
  const f = fixture();
  render(f.app('a'));
  fireEvent.click(screen.getByRole('button', { name: 'Send message' }));
  act(() => f.waits[0]!.accepted());
  fireEvent.change(screen.getByLabelText('Message this agent'), {
    target: { value: 'second message' },
  });
  fireEvent.click(screen.getByRole('button', { name: 'Send message' }));
  expect(f.waits).toHaveLength(2);
  await act(async () => f.waits[0]!.finish());
  expect(
    (screen.getByRole('button', { name: 'Send message' }) as HTMLButtonElement)
      .disabled,
  ).toBe(true);
  act(() => f.waits[1]!.accepted());
  await act(async () => f.waits[1]!.finish());
  await waitFor(() => expect(f.drafts.get('runtime:root:a')).toBe(''));
});
it('IME composition and Shift Enter do not submit', () => {
  const f = fixture();
  render(f.app('a'));
  const input = screen.getByLabelText('Message this agent');
  fireEvent.keyDown(input, { key: 'Enter', isComposing: true });
  fireEvent.keyDown(input, { key: 'Enter', shiftKey: true });
  expect(f.waits).toHaveLength(0);
});

it('keeps the acceptance latch and a newer draft through closing and reopening the same recipient', async () => {
  const f = fixture();
  const first = render(f.app('a'));
  fireEvent.click(screen.getByRole('button', { name: 'Send message' }));
  first.unmount();
  f.runtime.setDraft('runtime:root:a', 'newer unsent draft');
  render(f.app('a'));
  expect(
    (screen.getByRole('button', { name: 'Send message' }) as HTMLButtonElement)
      .disabled,
  ).toBe(true);
  act(() => f.waits[0]!.accepted());
  expect(f.runtime.draft('runtime:root:a')).toBe('newer unsent draft');
  expect(
    (screen.getByLabelText('Message this agent') as HTMLTextAreaElement).value,
  ).toBe('newer unsent draft');
  await act(async () => f.waits[0]!.finish());
});
it('restores selection on reopening while the chat view is inactive', () => {
  const f = fixture();
  const first = render(f.app('a'));
  const input = screen.getByLabelText(
    'Message this agent',
  ) as HTMLTextAreaElement;
  input.setSelectionRange(2, 5);
  fireEvent.select(input);
  first.unmount();
  render(f.app('a'));
  const reopened = screen.getByLabelText(
    'Message this agent',
  ) as HTMLTextAreaElement;
  expect([reopened.selectionStart, reopened.selectionEnd]).toEqual([2, 5]);
  expect(document.activeElement).not.toBe(reopened);
});

it('clears an unchanged reopened composer when its detached submission is accepted', async () => {
  const f = fixture();
  const first = render(f.app('a'));
  fireEvent.click(screen.getByRole('button', { name: 'Send message' }));
  first.unmount();
  render(f.app('a'));
  act(() => f.waits[0]!.accepted());
  expect(
    (screen.getByLabelText('Message this agent') as HTMLTextAreaElement).value,
  ).toBe('');
  await act(async () => f.waits[0]!.finish());
});

it('shares recipient text and submission lock across views without stealing focus on late acceptance', async () => {
  const f = fixture();
  f.runtime.tabs.visit('runtime', 'root', { agent: 'a' });
  const duplicate = f.runtime.tabs.split('root', 'right');
  render(<>{f.app('a', 'root')}{f.app('a', duplicate)}</>);
  const inputs = screen.getAllByLabelText('Message this agent') as HTMLTextAreaElement[];
  fireEvent.change(inputs[0]!, { target: { value: 'Shared recipient draft' } });
  expect(inputs[1]!.value).toBe('Shared recipient draft');
  fireEvent.click(screen.getAllByRole('button', { name: 'Send message' })[0]!);
  expect(f.waits).toHaveLength(1);
  expect(screen.getAllByRole('button', { name: 'Send message' }).every(button => (button as HTMLButtonElement).disabled)).toBe(true);
  inputs[1]!.focus();
  act(() => f.waits[0]!.accepted());
  expect(inputs.map(input => input.value)).toEqual(['', '']);
  expect(document.activeElement).toBe(inputs[1]);
  await act(async () => f.waits[0]!.finish());
});

it('restores independent caret positions for two views of the same recipient', () => {
  const f = fixture();
  const app = <>{f.app('a', 'one')}{f.app('a', 'two')}</>;
  const first = render(app);
  const inputs = screen.getAllByLabelText('Message this agent') as HTMLTextAreaElement[];
  inputs[0]!.setSelectionRange(1, 3); fireEvent.select(inputs[0]!);
  inputs[1]!.setSelectionRange(6, 8); fireEvent.select(inputs[1]!);
  first.unmount(); render(app);
  const restored = screen.getAllByLabelText('Message this agent') as HTMLTextAreaElement[];
  expect(restored.map(input => [input.selectionStart, input.selectionEnd])).toEqual([[1, 3], [6, 8]]);
});

for (const previous of [undefined, { event_seq: '10', error: 'EOF' }]) {
  it(`keeps an accepted failure before a recorded turn visible${previous ? ' beside an older identical failure' : ''}`, async () => {
    const f = fixture();
    render(f.app('a', undefined, false, previous));
    fireEvent.click(screen.getByRole('button', { name: 'Send message' }));
    await act(async () => { f.waits[0]!.accepted(); f.waits[0]!.reject(new Error('EOF')); });
    const notice = screen.getByRole('alert');
    expect(notice.textContent).toContain('Your message could not complete');
    expect(notice.closest('[data-error-type]')?.getAttribute('data-error-type')).toBe('submission');
    expect(f.runtime.report).not.toHaveBeenCalled();
  });
}
it('suppresses only a newly recorded matching failure after accepted submission', async () => {
  const f = fixture();
  const rendered = render(f.app('a', undefined, false, { event_seq: '10', error: 'EOF' }));
  fireEvent.click(screen.getByRole('button', { name: 'Send message' }));
  await act(async () => { f.waits[0]!.accepted(); f.waits[0]!.reject(new Error('EOF')); });
  expect(screen.getByRole('alert').textContent).toContain('EOF');
  rendered.rerender(f.app('a', undefined, false, { event_seq: '11', error: 'Provider overloaded' }));
  expect(screen.getByRole('alert').textContent).toContain('EOF');
  rendered.rerender(f.app('a', undefined, false, { event_seq: '12', error: 'EOF' }));
  expect(screen.queryByRole('alert')).toBeNull();
});
it('does not hide a rejected submission when an unrelated turn records the same error', async () => {
  const f = fixture();
  const rendered = render(f.app('a'));
  fireEvent.click(screen.getByRole('button', { name: 'Send message' }));
  await act(async () => { f.waits[0]!.reject(new Error('EOF')); });
  rendered.rerender(f.app('a', undefined, false, { event_seq: '12', error: 'EOF' }));
  expect(screen.getByRole('alert').textContent).toContain('EOF');
  expect(f.runtime.draft('runtime:root:a')).toBe('same draft');
});
for (const delivery of ['uncertain', 'absent'] as const) {
  it(`keeps failed ${delivery} recovery in the original submission notice`, async () => {
    const f = fixture();
    f.snapshot.commands.push({ id: 'pending', commandId: 'original', runtimeId: 'runtime', label: 'Message child', status: 'Acceptance unresolved', draftKey: 'runtime:root:a', delivery });
    const check = vi.fn().mockRejectedValue(new Error('Status check unavailable'));
    const retry = vi.fn().mockRejectedValue(new Error('Retry not accepted'));
    Object.assign(f.runtime, { checkCommand: check, retryCommand: retry });
    render(f.app('a'));
    await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'Check status' })); });
    expect(document.querySelectorAll('[data-error-type="submission"]')).toHaveLength(1);
    expect(screen.getByRole('status').textContent).toContain('Status check unavailable');
    expect(screen.queryByRole('alert')).toBeNull();
    expect(check).toHaveBeenCalledExactlyOnceWith('pending');
    expect(f.runtime.draft('runtime:root:a')).toBe('same draft');
    expect(screen.getByRole('button', { name: 'Send message' })).toHaveProperty('disabled', true);
    if (delivery === 'absent') {
      await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'Retry original submission' })); });
      expect(document.querySelectorAll('[data-error-type="submission"]')).toHaveLength(1);
      expect(screen.getByRole('status').textContent).toContain('Retry not accepted');
      expect(retry).toHaveBeenCalledExactlyOnceWith('pending');
    } else expect(screen.queryByRole('button', { name: 'Retry original submission' })).toBeNull();
    expect(f.waits).toHaveLength(0);
    expect(f.runtime.report).not.toHaveBeenCalled();
  });
}
for (const outcome of ['cancelled', 'interrupted']) {
  it(`presents accepted ${outcome} submissions as a neutral outcome`, async () => {
    const f = fixture();
    render(f.app('a'));
    fireEvent.click(screen.getByRole('button', { name: 'Send message' }));
    const input = f.runtime.submittedInputs.getSnapshot()[0]!;
    f.snapshot.commands.push({ id: 'terminal', commandId: input.id, runtimeId: 'runtime', label: 'Message child', status: outcome, draftKey: 'runtime:root:a' });
    await act(async () => { f.waits[0]!.accepted(); f.waits[0]!.reject(new Error(`Message child: ${outcome}`)); });
    expect(screen.queryByRole('alert')).toBeNull();
    expect(screen.getByRole('status').textContent).toContain(`Your message was ${outcome}`);
  });
}

for (const truncated of [false, true]) {
  it(`${truncated ? 'matches an explicitly truncated' : 'does not match a merely similar'} recorded error`, async () => {
    const f = fixture();
    const rendered = render(f.app('a'));
    fireEvent.click(screen.getByRole('button', { name: 'Send message' }));
    await act(async () => { f.waits[0]!.accepted(); f.waits[0]!.reject(new Error('EOF while streaming response')); });
    rendered.rerender(f.app('a', undefined, false, { event_seq: '12', error: 'EOF', error_truncated: truncated }));
    expect(screen.queryByRole('alert') === null).toBe(truncated);
  });
}

for (const outcome of ['cancelled', 'interrupted']) {
  it(`defers ${outcome} submission feedback only to a newer matching recorded turn without error details`, async () => {
    const f = fixture();
    const rendered = render(f.app('a', undefined, false, { event_seq: '10', status: outcome }));
    fireEvent.click(screen.getByRole('button', { name: 'Send message' }));
    const input = f.runtime.submittedInputs.getSnapshot()[0]!;
    f.snapshot.commands.push({ id: 'terminal', commandId: input.id, runtimeId: 'runtime', label: 'Message child', status: outcome, draftKey: 'runtime:root:a' });
    await act(async () => { f.waits[0]!.accepted(); f.waits[0]!.reject(new Error(`Message child: ${outcome}`)); });
    expect(screen.getByRole('status').textContent).toContain(`Your message was ${outcome}`);
    rendered.rerender(f.app('a', undefined, false, { event_seq: '11', status: outcome === 'cancelled' ? 'interrupted' : 'cancelled' }));
    expect(screen.getByRole('status').textContent).toContain(`Your message was ${outcome}`);
    rendered.rerender(f.app('a', undefined, false, { event_seq: '12', status: outcome }));
    expect(document.querySelector('[data-error-type="submission"]')).toBeNull();
    expect(f.runtime.report).not.toHaveBeenCalled();
  });
}


it('switches from Send to Stop after accepting an active-turn draft', async () => {
  const f = fixture();
  const session = { ...f.session, cancelTurn: vi.fn(), agents: { cancelTurn: vi.fn() } } as unknown as Session;
  render(<RuntimeContext.Provider value={f.runtime}><QueryClientProvider client={new QueryClient()}><UIProvider><Composer session={session} agentId="a" runtimeId="runtime" connected activeTurn="turn" queueEnabled /></UIProvider></QueryClientProvider></RuntimeContext.Provider>);
  expect(screen.queryByLabelText('Message delivery')).toBeNull();
  expect(screen.queryByRole('button', { name: 'Pause this turn' })).toBeNull();
  fireEvent.click(screen.getByRole('button', { name: 'Queue message' }));
  expect(f.session.command).toHaveBeenCalledWith('agent.submit', expect.objectContaining({ delivery: 'queued', text: 'same draft' }), expect.anything());
  await act(async () => { f.waits[0]!.accepted(); f.waits[0]!.finish(); });
  expect((screen.getByLabelText('Message this agent') as HTMLTextAreaElement).value).toBe('');
  expect(screen.queryByRole('button', { name: 'Queue message' })).toBeNull();
  expect(screen.getByRole('button', { name: 'Pause this turn' })).toBeTruthy();
  fireEvent.change(screen.getByLabelText('Message this agent'), { target: { value: 'Another message' } });
  expect(screen.getByRole('button', { name: 'Queue message' })).toBeTruthy();
  expect(screen.queryByRole('button', { name: 'Pause this turn' })).toBeNull();
  fireEvent.change(screen.getByLabelText('Message this agent'), { target: { value: '   ' } });
  expect(screen.queryByRole('button', { name: 'Queue message' })).toBeNull();
  expect(screen.getByRole('button', { name: 'Pause this turn' })).toBeTruthy();
});

it('stacks agent and queue slots below submission notices without remounting the draft', async () => {
  const f = fixture();
  const agents = <section aria-label="Test agents">Agents</section>;
  const queue = <section aria-label="Test queue">Queue</section>;
  const app = (showAgents = true, showQueue = true) => f.app('a', undefined, false, undefined, {
    agents: showAgents ? agents : undefined, queue: showQueue ? queue : undefined,
  });
  const rendered = render(app());
  const input = screen.getByLabelText('Message this agent') as HTMLTextAreaElement;
  const before = (a: Element, b: Element) => expect(a.compareDocumentPosition(b) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  before(screen.getByRole('region', { name: 'Test agents' }), screen.getByRole('region', { name: 'Test queue' }));
  before(screen.getByRole('region', { name: 'Test queue' }), input);
  fireEvent.click(screen.getByRole('button', { name: 'Send message' }));
  await act(async () => { f.waits[0]!.reject(new Error('Submission unavailable')); });
  before(screen.getByRole('alert'), screen.getByRole('region', { name: 'Test agents' }));
  for (const [showAgents, showQueue] of [[true, false], [false, true], [false, false], [true, true]]) {
    rendered.rerender(app(showAgents, showQueue));
    expect(screen.queryAllByRole('region', { name: 'Test agents' })).toHaveLength(showAgents ? 1 : 0);
    expect(screen.queryAllByRole('region', { name: 'Test queue' })).toHaveLength(showQueue ? 1 : 0);
    expect(screen.getByLabelText('Message this agent')).toBe(input);
    expect(input.value).toBe('same draft');
  }
});

it.each(['root', 'a'])('inserts a skill for %s without sending/queuing, including repeated Enter', async agentId => {
  const f = fixture(); render(f.app(agentId, undefined, true, undefined, { queueEnabled: true, activeTurn: 'working' }));
  const input = screen.getByRole('textbox') as HTMLTextAreaElement;
  act(() => input.focus());
  fireEvent.change(input, { target: { value: 'Use /po' } });
  await screen.findByRole('option', { name: /ponytail/ });
  expect(f.session.client.call).toHaveBeenCalledWith('workspace.complete', { root_id: 'root', agent_id: agentId, kind: 'skill', prefix: 'po', limit: 32 }, { signal: expect.any(AbortSignal) });
  fireEvent.keyDown(input, { key: 'Enter' });
  expect(input.value).toBe('Use $ponytail ');
  fireEvent.keyDown(input, { key: 'Enter', repeat: true });
  expect(f.session.submit).not.toHaveBeenCalled(); expect(f.waits).toHaveLength(0);
  fireEvent.keyDown(input, { key: 'Enter' });
  expect(f.waits).toHaveLength(1);
  await act(async () => { f.waits[0]!.accepted(); f.waits[0]!.finish(); });
});

it('does not resurrect dismissed skills after another composer overlay or an inactive-view roundtrip', async () => {
  const f = fixture(); const mounted = render(f.app('a', 'view', true));
  const input = screen.getByRole('textbox') as HTMLTextAreaElement;
  act(() => input.focus()); fireEvent.change(input, { target: { value: '/po' } });
  await screen.findByRole('option', { name: /ponytail/ });
  mounted.rerender(f.app('a', 'view', false));
  expect(screen.queryByRole('listbox')).toBeNull();
  mounted.rerender(f.app('a', 'view', true));
  expect(screen.queryByRole('listbox')).toBeNull();
  expect(input.value).toBe('/po');
});
it('compatibility IME Enter does not send an existing draft even with no popup', () => {
  const f = fixture(); render(f.app('a', undefined, true));
  fireEvent.keyDown(screen.getByRole('textbox'), { key: 'Enter', keyCode: 229, isComposing: false });
  expect(f.waits).toHaveLength(0);
});
it('Add context dialog roundtrip does not resurrect the inline picker', async () => {
  const f = fixture(); render(f.app('a', undefined, true));
  const input = screen.getByRole('textbox') as HTMLTextAreaElement;
  act(() => input.focus()); fireEvent.change(input, { target: { value: '/po' } });
  await screen.findByRole('option', { name: /ponytail/ });
  fireEvent.click(screen.getByRole('button', { name: 'Add context' }));
  await screen.findByRole('dialog', { name: 'Insert host context' });
  expect(screen.queryByRole('listbox', { name: 'Skills' })).toBeNull();
  fireEvent.click(screen.getByRole('button', { name: 'Close' }));
  await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
  expect(screen.queryByRole('listbox', { name: 'Skills' })).toBeNull();
  expect(input.value).toBe('/po');
});

it.each(['root', 'a'])('preloads and locally filters the active %s composer catalog', async agentId => {
  const f = fixture(true); render(f.app(agentId, 'view', true));
  const input = screen.getByRole('textbox') as HTMLTextAreaElement;
  act(() => input.focus());
  await waitFor(() => expect(f.session.client.call).toHaveBeenCalledWith('workspace.complete', expect.objectContaining({ root_id: 'root', agent_id: agentId, prefix: '', limit: 1024 }), { signal: expect.any(AbortSignal) }));
  fireEvent.change(input, { target: { value: '/' } }); await screen.findByRole('option', { name: /ponytail/ });
  for (const value of ['/p', '/po', '/p']) {
    fireEvent.change(input, { target: { value } }); expect(screen.getByRole('option', { name: /ponytail/ })).toBeTruthy();
  }
  expect(f.session.client.call).toHaveBeenCalledTimes(1); expect(f.session.submit).not.toHaveBeenCalled();
});
