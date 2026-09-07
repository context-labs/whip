import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { expect, it, vi } from 'vitest';
import { UIProvider } from '@whip/ui';
import type { Session } from '@whip/sdk';
import { Composer } from '../src/composer';
import { RuntimeContext } from '../src/context';
import type { AppRuntime } from '../src/runtime';

function fixture() {
  const drafts = new Map<string, string>([['runtime:root:a', 'same draft'], ['runtime:root:b', 'same draft']]);
  const waits: { accepted(): void; finish(): void }[] = [];
  const snapshot = { commands: [], endpoint: 'http://localhost' };
  const runtime = {
    getSnapshot: () => snapshot, subscribe: () => () => {},
    draft: (key: string) => drafts.get(key) ?? '',
    setDraft: (key: string, text: string) => drafts.set(key, text),
    report: vi.fn(),
    run: (_handle: unknown, _label: string, accepted: () => void) => new Promise<void>(finish => waits.push({ accepted, finish })),
  } as unknown as AppRuntime;
  const session = { rootId: 'root', agents: { submit: vi.fn(() => ({})) } } as unknown as Session;
  const app = (agentId: string) => <RuntimeContext.Provider value={runtime}><UIProvider><Composer key={agentId} session={session} agentId={agentId} connected runtimeId="runtime" /></UIProvider></RuntimeContext.Provider>;
  return { drafts, waits, app, session };
}
it('a late acceptance for one recipient cannot clear another recipient’s identical draft', async () => {
  const f = fixture();
  const rendered = render(f.app('a'));
  fireEvent.click(screen.getByRole('button', { name: 'Send message' }));
  expect(f.waits).toHaveLength(1);
  rendered.rerender(f.app('b'));
  act(() => f.waits[0]!.accepted());
  expect((screen.getByLabelText('Message this agent') as HTMLTextAreaElement).value).toBe('same draft');
  expect(f.drafts.get('runtime:root:a')).toBe('');
  expect(f.drafts.get('runtime:root:b')).toBe('same draft');
  await act(async () => f.waits[0]!.finish());
});
it('an older terminal outcome cannot unlock a newer submission awaiting acceptance', async () => {
  const f = fixture(); render(f.app('a'));
  fireEvent.click(screen.getByRole('button', { name: 'Send message' }));
  act(() => f.waits[0]!.accepted());
  fireEvent.change(screen.getByLabelText('Message this agent'), { target: { value: 'second message' } });
  fireEvent.click(screen.getByRole('button', { name: 'Send message' }));
  expect(f.waits).toHaveLength(2);
  await act(async () => f.waits[0]!.finish());
  expect((screen.getByRole('button', { name: 'Send message' }) as HTMLButtonElement).disabled).toBe(true);
  act(() => f.waits[1]!.accepted());
  await act(async () => f.waits[1]!.finish());
  await waitFor(() => expect(f.drafts.get('runtime:root:a')).toBe(''));
});
it('IME composition and Shift Enter do not submit', () => {
  const f = fixture(); render(f.app('a'));
  const input = screen.getByLabelText('Message this agent');
  fireEvent.keyDown(input, { key: 'Enter', isComposing: true });
  fireEvent.keyDown(input, { key: 'Enter', shiftKey: true });
  expect(f.waits).toHaveLength(0);
});
