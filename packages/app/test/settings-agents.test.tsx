import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { ThemeProvider, UIProvider } from '@whip/ui';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import type { WhipClient } from '@whip/sdk';
import type { Definition } from '@whip/sdk/agents';
import type { ReactNode } from 'react';
import { RuntimeContext } from '../src/context';
import type { AppRuntime } from '../src/runtime';
import { AgentsSettings, agentDocument, agentValues } from '../src/settings/agents';

beforeEach(() => vi.stubGlobal('matchMedia', () => ({ matches: false, addEventListener() {}, removeEventListener() {} })));
afterEach(() => vi.unstubAllGlobals());

const coding: Definition = {
  id: 'coding',
  instructions: { persona: 'You are a coding agent.', rules: 'Rules.', project_files: ['CLAUDE.md'], skill_discovery: true, standing_instructions: true },
  modules: ['context', 'files', 'shell', 'agents', 'user'], capabilities: ['read', 'write', 'shell', 'mcp'],
  model: { model: '', provider: '', effort: '' }, compaction: { model: '', provider: '', threshold: 0 }, mcp: { servers: null },
  tools: null, output: null, children: {}, surface: { auto_title: true, goal_loop: true }, hooks: null,
};
const triage: Definition = { ...coding, id: 'support-triage', instructions: { ...coding.instructions, persona: 'You triage tickets.', project_files: null }, modules: ['context', 'files'], capabilities: ['read'], surface: { auto_title: true, goal_loop: false } };

function fixture(supported = true) {
  const registered = [{ id: 'support-triage', revision: 'b'.repeat(64), built_in: false, registered_by: 'app', created_at: '2026-09-11T00:00:00Z' }];
  const list = vi.fn(async () => ({ items: [{ id: 'coding', revision: '', built_in: true, registered_by: '', created_at: '' }, ...registered] }));
  const get = vi.fn(async (id: string) => ({ definition: id === 'coding' ? coding : triage, revision: id === 'coding' ? '' : 'b'.repeat(64), built_in: id === 'coding', registered_by: '', created_at: '' }));
  const register = vi.fn(async (definition: Definition) => {
    if (definition.id === 'coding') throw new Error('agent definition id "coding" is reserved for a built-in definition');
    registered.push({ id: definition.id, revision: 'c'.repeat(64), built_in: false, registered_by: 'app', created_at: '2026-09-11T00:00:01Z' });
    return { id: definition.id, revision: 'c'.repeat(64), created: true };
  });
  const client = { getSnapshot: () => ({ state: 'connected', info: { runtime_id: 'host-a' } }), supports: () => supported, agents: { list, get, register } } as unknown as WhipClient;
  const queries = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } });
  const runtime = { queries, report: vi.fn(), getSnapshot: () => ({ commands: [] }), subscribe: () => () => {} } as unknown as AppRuntime;
  const wrapper = (children: ReactNode) => <RuntimeContext.Provider value={runtime}><ThemeProvider initialTheme="light"><UIProvider><QueryClientProvider client={queries}>{children}</QueryClientProvider></UIProvider></ThemeProvider></RuntimeContext.Provider>;
  return { list, get, register, queries, render: () => render(wrapper(<AgentsSettings client={client} enabled />)) };
}

it('lists built-in and registered agents and registers a new data-only definition', async () => {
  const f = fixture(); f.render();
  const rows = await screen.findAllByRole('listitem', {}, { timeout: 3000 });
  expect(rows.map(row => row.textContent)).toEqual([expect.stringContaining('codingBuilt in'), expect.stringContaining('support-triageRegistered by app · revision bbbbbbbbbbbb')]);
  const modules = await screen.findByRole('group', { name: 'Host modules' }, { timeout: 3000 });
  await within(modules).findByRole('checkbox', { name: 'shell' }, { timeout: 3000 });
  const capabilities = screen.getByRole('group', { name: 'Capabilities' });
  fireEvent.change(screen.getByLabelText('Agent id'), { target: { value: 'release-notes' } });
  fireEvent.change(screen.getByLabelText('Persona'), { target: { value: 'You write release notes.' } });
  fireEvent.change(screen.getByLabelText('Rules'), { target: { value: 'Operating rules:\n- Cite commits.' } });
  fireEvent.change(screen.getByLabelText('Project files'), { target: { value: 'CHANGELOG.md, docs/releases.md' } });
  fireEvent.click(within(modules).getByRole('checkbox', { name: 'context' }));
  fireEvent.click(within(modules).getByRole('checkbox', { name: 'files' }));
  fireEvent.click(within(capabilities).getByRole('checkbox', { name: 'read' }));
  fireEvent.click(screen.getByRole('button', { name: 'Register agent' }));
  await screen.findByText(/Registered release-notes as revision cccccccccccc/);
  expect(f.register).toHaveBeenCalledOnce();
  const document = f.register.mock.calls[0][0] as Definition;
  expect(document.id).toBe('release-notes');
  expect(document.instructions).toEqual({ persona: 'You write release notes.', rules: 'Operating rules:\n- Cite commits.', project_files: ['CHANGELOG.md', 'docs/releases.md'], skill_discovery: false, standing_instructions: true });
  expect(document.modules).toEqual(['context', 'files']);
  expect(document.capabilities).toEqual(['read']);
  expect(document.tools).toBeNull();
  expect(document.hooks).toBeNull();
  expect(document.surface).toEqual({ auto_title: true, goal_loop: false });
  await waitFor(() => expect(f.list).toHaveBeenCalledTimes(2));
  await waitFor(() => expect(within(screen.getByRole('list', { name: 'Agent definitions' })).getAllByRole('listitem')).toHaveLength(3));
});

it('validates locally before registering and shows the daemon rejection otherwise', async () => {
  const f = fixture(); f.render();
  const modules = await screen.findByRole('group', { name: 'Host modules' }, { timeout: 3000 });
  await within(modules).findByRole('checkbox', { name: 'shell' }, { timeout: 3000 });
  fireEvent.change(screen.getByLabelText('Agent id'), { target: { value: 'Bad Id' } });
  fireEvent.click(within(modules).getByRole('checkbox', { name: 'context' }));
  fireEvent.click(screen.getByRole('button', { name: 'Register agent' }));
  expect(await screen.findByText(/Use a lowercase id/)).toBeTruthy();
  fireEvent.change(screen.getByLabelText('Agent id'), { target: { value: 'coding' } });
  fireEvent.click(screen.getByRole('button', { name: 'Register agent' }));
  expect(await screen.findByText(/reserved for a built-in definition/)).toBeTruthy();
  expect(f.register).toHaveBeenCalledOnce();
});

it('opens a registered agent for editing and copies a built-in under a new id', async () => {
  const f = fixture(); f.render();
  const rows = await screen.findAllByRole('listitem', {}, { timeout: 3000 });
  fireEvent.click(within(rows[1]).getByRole('button', { name: 'Edit' }));
  await screen.findByRole('heading', { name: 'Edit support-triage' });
  expect((screen.getByLabelText('Agent id') as HTMLInputElement).value).toBe('support-triage');
  expect((screen.getByLabelText('Persona') as HTMLTextAreaElement).value).toBe('You triage tickets.');
  const modules = await screen.findByRole('group', { name: 'Host modules' }, { timeout: 3000 });
  await within(modules).findByRole('checkbox', { name: 'shell' }, { timeout: 3000 });
  expect(within(modules).getByRole('checkbox', { name: 'files' }).getAttribute('aria-checked')).toBe('true');
  expect(within(modules).getByRole('checkbox', { name: 'shell' }).getAttribute('aria-checked')).toBe('false');
  fireEvent.click(within(rows[0]).getByRole('button', { name: 'Copy to new agent' }));
  await screen.findByRole('heading', { name: 'Edit coding-custom' });
  expect((screen.getByLabelText('Agent id') as HTMLInputElement).value).toBe('coding-custom');
  expect(f.get).toHaveBeenCalledWith('support-triage', 'b'.repeat(64));
  expect(f.get).toHaveBeenCalledWith('coding', undefined);
});

it('tells an older host it cannot author agents', async () => {
  const f = fixture(false); f.render();
  expect(await screen.findByText(/does not support agent definitions/)).toBeTruthy();
  expect(f.list).not.toHaveBeenCalled();
});

it('round-trips a definition through the editor values', () => {
  const values = agentValues(triage, false);
  const document = agentDocument(values);
  expect(document).toEqual({ ...triage, instructions: { ...triage.instructions, project_files: null }, model: triage.model });
  expect(agentValues(coding, true).id).toBe('coding-custom');
});
