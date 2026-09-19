import { fireEvent, render, screen } from '@testing-library/react';
import { expect, it, vi } from 'vitest';
import { ThemeProvider, UIProvider } from '@whip/ui';
import type { SessionViewSnapshot } from '@whip/sdk/state';
import { SessionTopBar } from '../src/session-top-bar';
import { activityStatus, CurrentActivity } from '../src/chat-activity';

it('keeps full host/path identity accessible and opens scoped actions', () => {
  const onRepl = vi.fn(), onAgents = vi.fn(), onDetails = vi.fn();
  render(<ThemeProvider><UIProvider><SessionTopBar host="Remote" cwd="/workspace/whip" agentName="reviewer" kind="chat"
    onRepl={onRepl} onAgents={onAgents} onDetails={onDetails} /></UIProvider></ThemeProvider>);
  expect(screen.getByLabelText('Remote / /workspace/whip')).toBeDefined();
  fireEvent.click(screen.getByRole('button', { name: 'Open REPL' }));
  fireEvent.click(screen.getByRole('button', { name: 'Agent: reviewer' }));
  fireEvent.click(screen.getByRole('button', { name: 'Session details' }));
  expect(onRepl).toHaveBeenCalledOnce(); expect(onAgents).toHaveBeenCalledOnce(); expect(onDetails).toHaveBeenCalledOnce();
});

it.each(['chat', 'repl', 'trace'] as const)('uses the same top bar and action definitions for %s', async kind => {
  const onRepl = vi.fn(), onTrace = vi.fn(), onDetails = vi.fn(), onRoot = vi.fn(), onRename = vi.fn();
  render(<ThemeProvider><UIProvider><SessionTopBar host="Local" cwd="/work/whip" agentName="reviewer" kind={kind}
    onRepl={onRepl} onTrace={onTrace} onDetails={onDetails} onRoot={onRoot}
    actions={[{ id: 'rename', label: 'Rename session', onSelect: onRename }]} /></UIProvider></ThemeProvider>);
  expect(screen.getAllByRole('banner', { name: 'Session information' })).toHaveLength(1);
  const controls = [
    { label: 'Open REPL', enabled: kind !== 'repl', callback: onRepl },
    { label: 'Open trace', enabled: kind !== 'trace', callback: onTrace },
    { label: 'Session details', enabled: true, callback: onDetails },
  ];
  for (const control of controls) {
    if (control.enabled) {
      fireEvent.click(screen.getByRole('button', { name: control.label }));
      expect(control.callback).toHaveBeenCalledOnce();
    } else {
      expect(screen.queryByRole('button', { name: control.label })).toBeNull();
    }
  }
  for (const control of controls) {
    fireEvent.click(screen.getByRole('button', { name: 'Session actions' }));
    await screen.findByRole('menu');
    if (control.enabled) {
      fireEvent.click(screen.getByRole('menuitem', { name: control.label }));
      expect(control.callback).toHaveBeenCalledTimes(2);
    } else {
      expect(screen.queryByRole('menuitem', { name: control.label })).toBeNull();
      fireEvent.keyDown(screen.getByRole('menu'), { key: 'Escape' });
    }
  }
  fireEvent.click(screen.getByRole('button', { name: 'Session actions' }));
  fireEvent.click(await screen.findByRole('menuitem', { name: 'Root conversation' }));
  expect(onRoot).toHaveBeenCalledOnce();
  fireEvent.click(screen.getByRole('button', { name: 'Session actions' }));
  fireEvent.click(await screen.findByRole('menuitem', { name: 'Rename session' }));
  expect(onRename).toHaveBeenCalledOnce();
});

it('keeps the shared identity bar while a session is opening', () => {
  render(<ThemeProvider><UIProvider><SessionTopBar host="Local" agentName="Root" kind="trace" pending
    activity={<span role="status">Loading session…</span>} /></UIProvider></ThemeProvider>);
  expect(screen.getByLabelText('Local')).toBeDefined();
  expect(screen.queryByText('Directory unavailable')).toBeNull();
  expect(screen.getByRole('status').textContent).toBe('Loading session…');
});

it('shows one current status in the bar without a duplicate agent dock', () => {
  const state = { status: 'live', root: { root_id: 'root', active_turns: {}, agents: [], permissions: [], questions: [{ question_id: 'q' }] } } as unknown as SessionViewSnapshot;
  const app = (connected: boolean) => <ThemeProvider><UIProvider>
    <SessionTopBar host="Local" cwd="/whip" agentName="Root" kind="chat"
      activity={<CurrentActivity status={activityStatus(state, 'root', [], connected)} connected={connected} onDetails={vi.fn()} />} />
  </UIProvider></ThemeProvider>;
  const view = render(app(true));
  expect(screen.getAllByRole('status')).toHaveLength(1);
  expect(screen.getByRole('status').textContent).toBe('Waiting for your answer');
  view.rerender(app(false));
  expect(screen.getAllByRole('status')).toHaveLength(1);
  expect(screen.getByRole('status').textContent).toBe('Reconnecting · activity updates paused');
});

it('preserves the activity motion control and hides session-only controls for New Chat', () => {
  const view = render(<ThemeProvider><UIProvider><SessionTopBar host="Local" cwd="/whip" agentName="Root" kind="repl"
    activity={<CurrentActivity status={{ text: 'Working', active: true }} connected onDetails={vi.fn()} />} /></UIProvider></ThemeProvider>);
  fireEvent.click(screen.getByRole('button', { name: 'Pause activity animation' }));
  expect(screen.getByRole('button', { name: 'Use system motion setting' })).toBeDefined();
  view.rerender(<ThemeProvider><UIProvider><SessionTopBar host="Choose a host" kind="new" /></UIProvider></ThemeProvider>);
  expect(screen.getByText('Not started')).toBeDefined();
  expect(screen.queryByRole('button', { name: 'Open REPL' })).toBeNull();
  expect(screen.queryByRole('button', { name: 'Session actions' })).toBeNull();
});
