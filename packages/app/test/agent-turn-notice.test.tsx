import { act, fireEvent, screen, waitFor } from '@testing-library/react';
import { beforeEach, afterEach, expect, it, vi } from 'vitest';
import type { Turn } from '@whip/sdk';
import { AgentTurnNotice, useSelectedAgent } from '../src/agent-turn-notice';
import { providerFixture, sessionRecord } from './provider-fixture';

beforeEach(() => vi.stubGlobal('matchMedia', () => ({ matches: false, addEventListener() {}, removeEventListener() {} })));
afterEach(() => vi.unstubAllGlobals());
const agent = { ...sessionRecord('child'), definition: { ...sessionRecord('child').definition, id: 'architecture-researcher' } };
const turn: Turn = { id: 'turn', session_id: 'child', history_revision: '1', goal: null, kind: 'prompt', config_revision: agent.config_revision, state: 'failed', failure: '400 Bad Request: Invalid prompt_cache_key', started_at: '2026-09-28T00:00:00Z', finished_at: '2026-09-28T00:01:00Z' };

it('shows and copies the recorded native failure, then clears for another turn', async () => {
  const f = await providerFixture();
  const app = (activeTurn?: string, outcome = turn) => f.wrap(<AgentTurnNotice session={f.client.session('child')} selected={agent} turn={outcome} activeTurn={activeTurn} />);
  const mounted = f.mount(<AgentTurnNotice session={f.client.session('child')} selected={agent} turn={turn} />);
  expect(screen.getByRole('alert').textContent).toContain('architecture-researcher · Last turn failed');
  fireEvent.click(screen.getByRole('button', { name: 'Error details' }));
  expect(screen.getByRole('alert').textContent).toContain('Invalid prompt_cache_key');
  fireEvent.click(screen.getByRole('button', { name: 'Copy error' }));
  await waitFor(() => expect(f.runtime.platform.copy).toHaveBeenCalledWith(turn.failure));
  mounted.rerender(app('next-turn')); expect(screen.queryByRole('alert')).toBeNull();
  mounted.rerender(app(undefined, { ...turn, id: 'next', state: 'succeeded', failure: null })); expect(screen.queryByRole('alert')).toBeNull();
  mounted.rerender(app(undefined, { ...turn, id: 'interrupted', state: 'interrupted', failure: 'Host restarted' }));
  expect(screen.getByRole('status').textContent).toContain('Last turn interrupted'); expect(screen.queryByRole('alert')).toBeNull();
});

it('does not attribute a foreign turn or changed current model to the last turn', async () => {
  const f = await providerFixture();
  const mounted = f.mount(<AgentTurnNotice session={f.client.session('child')} selected={agent} turn={{ ...turn, session_id: 'other' }} />);
  expect(mounted.container.firstChild).toBeNull();
  mounted.rerender(f.wrap(<AgentTurnNotice session={f.client.session('child')} selected={{ ...agent, config_revision: '9007199254740994' }} turn={turn} />));
  expect(screen.getByRole('alert').textContent).not.toContain('openrouter');
});

it('reads only the selected session, refreshes explicitly, and never admits work', async () => {
  const f = await providerFixture();
  f.data.handlers['sessions.get'] = request => request.method === 'sessions.get' ? { ...agent, id: request.params.session_id } : null;
  function Selected({ id = 'child', connected = true }: { id?: string; connected?: boolean }) {
    const query = useSelectedAgent(f.client.session(id), connected);
    return <span>{query.data?.id} {query.data?.definition.id}</span>;
  }
  const mounted = f.mount(<Selected />);
  await screen.findByText('child architecture-researcher'); expect(f.count('sessions.get')).toBe(1);
  mounted.rerender(f.wrap(<Selected />)); expect(f.count('sessions.get')).toBe(1);
  await act(async () => { await f.queries.invalidateQueries({ queryKey: ['selected-agent'] }); });
  await waitFor(() => expect(f.count('sessions.get')).toBe(2));
  mounted.rerender(f.wrap(<Selected id="other" />)); await screen.findByText('other architecture-researcher');
  expect(f.count('sessions.get')).toBe(3);
  mounted.rerender(f.wrap(<Selected id="offline" connected={false} />)); expect(f.count('sessions.get')).toBe(3);
  expect(f.calls.every(call => ['initialize', 'sessions.get'].includes(call.method))).toBe(true);
});
