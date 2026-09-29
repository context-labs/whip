import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { type Definition, type DefinitionDocument, type DefinitionRef } from '@whip/sdk';
import { AgentsSettings, agentDocument, agentValues } from '../src/settings/agents';
import { definitionOptions, parseDefinitionOption } from '../src/definitions';
import { providerFixture, revision, nextRevision } from './provider-fixture';
beforeEach(() => vi.stubGlobal('matchMedia', () => ({ matches: false, addEventListener() {}, removeEventListener() {} })));
afterEach(() => vi.unstubAllGlobals());
const coding: Definition = { ref: { id: 'coding', revision }, document: { id: 'coding', name: 'Coding', defaults: { modules: ['context', 'files', 'shell'], automatic_title: true, goals_enabled: true, instructions: { project_root: null, text: 'You help with code.', project_files: ['AGENTS.md'], discover_skills: true, standing_instructions: true, skill_roots: [] } } }, created_at: '2026-09-28T00:00:00Z' };
const custom: Definition = { ref: { id: 'triage', revision: nextRevision }, document: { id: 'triage', name: 'Ticket triage', defaults: { modules: ['files'], tools: { lookup: { description: 'Lookup', timeout_millis: 1000, input_schema: { type: 'object' }, output_schema: null } }, hooks: { before_tool: { optional: false, timeout_millis: 1000, operations: ['files.read'] } }, children: { assistant: coding.ref }, output: { schema: { type: 'object' } }, model: { provider: 'openrouter', name: 'fixture', effort: 'low' } } }, created_at: '2026-09-28T01:00:00Z' };
async function fixture() {
  const f = await providerFixture({ builtins: [coding.ref] }); const records = [coding, custom];
  f.data.handlers['definitions.list'] = () => ({ items: records.map(record => ({ ref: record.ref, name: record.document.name, created_at: record.created_at })), next_cursor: null });
  f.data.handlers['definitions.get'] = request => records.find(record => record.ref.id === (request.params as DefinitionRef).id)!;
  f.data.handlers['definitions.register'] = request => { const document = request.params as DefinitionDocument; const result = { ref: { id: document.id, revision: 'c'.repeat(64) }, document, created_at: coding.created_at }; records.push(result); return result; };
  return { ...f, records };
}
it('lists immutable refs and registers data-only instructions without invented capability grants', async () => {
  const f = await fixture(); f.mount(<AgentsSettings client={f.client} enabled />);
  expect(await screen.findByRole('button', { name: 'Copy to new agent' })).toBeTruthy(); expect(screen.queryByRole('group', { name: 'Capabilities' })).toBeNull();
  fireEvent.click(screen.getByRole('button', { name: 'New agent' }));
  fireEvent.change(screen.getByLabelText('Agent id'), { target: { value: 'release-notes' } }); fireEvent.change(screen.getByLabelText('Display name'), { target: { value: 'Release notes' } }); fireEvent.change(screen.getByLabelText('Instructions'), { target: { value: 'Write notes.\nCite commits.' } });
  const modules = screen.getByRole('group', { name: 'Host modules' }); fireEvent.click(await within(modules).findByRole('checkbox', { name: 'files' })); fireEvent.click(screen.getByRole('button', { name: 'Register agent' })); await screen.findByText(/Registered release-notes as revision/);
  expect(f.calls.find(call => call.method === 'definitions.register')?.params).toMatchObject({ id: 'release-notes', name: 'Release notes', defaults: { modules: ['files'], instructions: { text: 'Write notes.\nCite commits.' }, automatic_title: true, goals_enabled: false } });
  expect(f.count('definitions.register')).toBe(1); await waitFor(() => expect(f.count('definitions.list')).toBe(2));
});
it('copies built-ins by exact advertised revision and preserves existing revisions', async () => {
  const f = await fixture(); f.mount(<AgentsSettings client={f.client} enabled />); fireEvent.click(await screen.findByRole('button', { name: 'Copy to new agent' }));
  await waitFor(() => expect((screen.getByLabelText('Agent id') as HTMLInputElement).value).toBe('coding-custom'));
  expect(f.calls.filter(call => call.method === 'definitions.get').every(call => (call.params as DefinitionRef).revision === revision)).toBe(true); expect(f.count('definitions.register')).toBe(0);
});
it('editing display text preserves tools hooks output children models and omitted defaults', () => {
  const values = agentValues(custom, false); values.name = 'Renamed triage';
  const result = agentDocument(values); expect(result).toEqual({ ...custom.document, name: 'Renamed triage' });
  expect(custom.document.defaults.automatic_title).toBeUndefined();
  values.instructions = 'New rules'; expect(agentDocument(values).defaults.instructions?.text).toBe('New rules');
  expect(agentDocument(values).defaults.tools).toEqual(custom.document.defaults.tools);
});
it('validates identity locally without a speculative registration', async () => {
  const f = await fixture(); f.mount(<AgentsSettings client={f.client} enabled />); fireEvent.click(screen.getByRole('button', { name: 'New agent' })); fireEvent.change(screen.getByLabelText('Agent id'), { target: { value: 'Bad ID' } }); fireEvent.click(screen.getByRole('button', { name: 'Register agent' })); await screen.findByText(/Use a lowercase id/); expect(f.count('definitions.register')).toBe(0);
});
it('definition choices round-trip exact revisions and never infer latest by ID', () => {
  const items = [custom, { ...custom, ref: { ...custom.ref, revision } }].map(value => ({ ref: value.ref, name: value.document.name, created_at: value.created_at }));
  expect(definitionOptions(items).map(item => parseDefinitionOption(item.value))).toEqual(items.map(item => item.ref)); expect(() => parseDefinitionOption('triage')).toThrow();
});
it('loads further metadata only on explicit paging', async () => {
  const f = await fixture(); let calls = 0; f.data.handlers['definitions.list'] = () => ++calls === 1 ? { items: [{ ref: coding.ref, name: 'Coding', created_at: coding.created_at }], next_cursor: coding.ref } : { items: [{ ref: custom.ref, name: 'Ticket triage', created_at: custom.created_at }], next_cursor: null };
  f.mount(<AgentsSettings client={f.client} enabled />); await screen.findByRole('button', { name: 'Load more definitions' }); expect(f.count('definitions.list')).toBe(1);
  fireEvent.click(screen.getByRole('button', { name: 'Load more definitions' })); await screen.findByText('Ticket triage'); expect(f.calls.filter(call => call.method === 'definitions.list')[1]?.params).toEqual({ limit: 100, after: coding.ref });
});
it('preserves an unfinished draft until explicit discard before changing definitions', async () => {
  const f = await fixture(); f.mount(<AgentsSettings client={f.client} enabled />);
  await screen.findByRole('button', { name: 'Copy to new agent' });
  fireEvent.click(screen.getByRole('button', { name: 'New agent' }));
  fireEvent.change(screen.getByLabelText('Agent id'), { target: { value: 'unfinished' } });
  expect((screen.getByRole('button', { name: 'Copy to new agent', hidden: true }) as HTMLButtonElement).disabled).toBe(true);
  fireEvent.click(screen.getByRole('button', { name: 'Copy to new agent', hidden: true }));
  expect((screen.getByLabelText('Agent id') as HTMLInputElement).value).toBe('unfinished');
  fireEvent.click(screen.getByRole('button', { name: 'Discard draft' }));
  fireEvent.click(screen.getByRole('button', { name: 'Copy to new agent', hidden: true }));
  await waitFor(() => expect((screen.getByLabelText('Agent id') as HTMLInputElement).value).toBe('coding-custom'));
  expect(f.count('definitions.register')).toBe(0);
});
it('keeps advanced options collapsed and retains a draft when the compact editor closes', async () => {
  const f = await fixture(); f.mount(<AgentsSettings client={f.client} enabled />);
  expect(screen.queryByRole('dialog')).toBeNull();
  fireEvent.click(screen.getByRole('button', { name: 'New agent' }));
  expect(screen.getByRole('button', { name: 'More options' }).getAttribute('aria-expanded')).toBe('false');
  fireEvent.click(screen.getByRole('button', { name: 'More options' }));
  expect(screen.getByRole('button', { name: 'More options' }).getAttribute('aria-expanded')).toBe('true');
  fireEvent.change(screen.getByLabelText('Agent id'), { target: { value: 'draft-agent' } });
  fireEvent.click(screen.getByRole('button', { name: 'Close', exact: true }));
  await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
  fireEvent.click(screen.getByRole('button', { name: 'Continue editing' }));
  expect((screen.getByLabelText('Agent id') as HTMLInputElement).value).toBe('draft-agent');
  expect(f.count('definitions.register')).toBe(0);
});
