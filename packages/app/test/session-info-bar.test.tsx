import { fireEvent, render, screen } from '@testing-library/react';
import { expect, it, vi } from 'vitest';
import { ThemeProvider, UIProvider } from '@whip/ui';
import type { SessionViewSnapshot } from '@whip/sdk/state';
import { SessionInfoBar } from '../src/session-info-bar';
import { activityStatus, ChatActivity, CurrentActivity } from '../src/chat-activity';

it('keeps full host/path identity accessible and opens scoped actions', () => {
  const onRepl = vi.fn(), onAgents = vi.fn(), onDetails = vi.fn();
  render(<ThemeProvider><UIProvider><SessionInfoBar host="Remote" cwd="/workspace/whip" agentName="reviewer" kind="chat"
    onRepl={onRepl} onAgents={onAgents} onDetails={onDetails} /></UIProvider></ThemeProvider>);
  expect(screen.getByLabelText('Remote / /workspace/whip')).toBeDefined();
  fireEvent.click(screen.getByRole('button', { name: 'Open REPL' }));
  fireEvent.click(screen.getByRole('button', { name: 'Agent: reviewer' }));
  fireEvent.click(screen.getByRole('button', { name: 'Session details' }));
  expect(onRepl).toHaveBeenCalledOnce(); expect(onAgents).toHaveBeenCalledOnce(); expect(onDetails).toHaveBeenCalledOnce();
});

it('shows one current status in the bar while child activity stays below it', () => {
  const state = { status: 'live', root: { root_id: 'root', active_turns: {}, agents: [], permissions: [], questions: [{ question_id: 'q' }] } } as unknown as SessionViewSnapshot;
  const app = (connected: boolean) => <ThemeProvider><UIProvider>
    <SessionInfoBar host="Local" cwd="/whip" agentName="Root" kind="chat"
      activity={<CurrentActivity status={activityStatus(state, 'root', [], connected)} connected={connected} onDetails={vi.fn()} />} />
    <ChatActivity state={state} agentId="root" connected={connected} onAgent={vi.fn()} onAllAgents={vi.fn()} />
  </UIProvider></ThemeProvider>;
  const view = render(app(true));
  expect(screen.getAllByRole('status')).toHaveLength(1);
  expect(screen.getByRole('status').textContent).toBe('Waiting for your answer');
  view.rerender(app(false));
  expect(screen.getAllByRole('status')).toHaveLength(1);
  expect(screen.getByRole('status').textContent).toBe('Reconnecting · activity updates paused');
});

it('preserves the activity motion control and hides session-only controls for New Chat', () => {
  const view = render(<ThemeProvider><UIProvider><SessionInfoBar host="Local" cwd="/whip" agentName="Root" kind="repl"
    activity={<CurrentActivity status={{ text: 'Working', active: true }} connected onDetails={vi.fn()} />} /></UIProvider></ThemeProvider>);
  fireEvent.click(screen.getByRole('button', { name: 'Pause activity animation' }));
  expect(screen.getByRole('button', { name: 'Use system motion setting' })).toBeDefined();
  view.rerender(<ThemeProvider><UIProvider><SessionInfoBar host="Choose a host" kind="new" /></UIProvider></ThemeProvider>);
  expect(screen.getByText('Not started')).toBeDefined();
  expect(screen.queryByRole('button', { name: 'Open REPL' })).toBeNull();
  expect(screen.queryByRole('button', { name: 'Session actions' })).toBeNull();
});
