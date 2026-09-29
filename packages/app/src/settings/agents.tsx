import { useEffect, useRef, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { useForm, useStore } from '@tanstack/react-form';
import type { Client, Definition, DefinitionDocument } from '@whip/sdk';
import { assertValid } from '@whip/protocol';
import { Button, Checkbox, Collapsible, Dialog, Field, Fieldset, Input, Switch, Textarea } from '@whip/ui';
import * as stylex from '@stylexjs/stylex';
import { surface, typography, scale } from '@whip/ui/tokens.stylex';
import { useRuntime } from '../context';
import { ErrorNotice } from '../error-feedback';
import { definitionIdPattern } from '../session-tabs';
import { definitionsQueryKey, useDefinitions, type DefinitionSummary } from '../definitions';
import { SettingsGroup } from './section-layout';
import { useSettingsEdits } from './unsaved';

/** The editable, data-only part of a definition. Tools and hooks need code and stay with the SDK. */
type Module = NonNullable<DefinitionDocument['defaults']['modules']>[number];
export interface AgentValues {
  id: string; name: string; instructions: string; projectFiles: string;
  skillDiscovery: boolean; standingInstructions: boolean; modules: Module[];
  autoTitle: boolean; goalLoop: boolean; existing: boolean; defaults: DefinitionDocument['defaults'];
}
const blank: AgentValues = { id: '', name: '', instructions: '', projectFiles: '', skillDiscovery: false, standingInstructions: true, modules: [], autoTitle: true, goalLoop: false, existing: false, defaults: {} };
export function agentValues(definition: Definition, builtIn: boolean): AgentValues {
  const { document } = definition;
  return { id: builtIn ? `${document.id}-custom` : document.id, name: document.name,
    instructions: document.defaults.instructions?.text ?? '', projectFiles: (document.defaults.instructions?.project_files ?? []).join(', '),
    skillDiscovery: document.defaults.instructions?.discover_skills ?? false, standingInstructions: document.defaults.instructions?.standing_instructions ?? true,
    modules: [...(document.defaults.modules ?? [])], autoTitle: document.defaults.automatic_title ?? true, goalLoop: document.defaults.goals_enabled ?? false, existing: true, defaults: structuredClone(document.defaults) };
}
/** Preserve every unedited declaration, including tools/hooks/output and exact child revisions. */
export function agentDocument(value: AgentValues): DefinitionDocument {
  const defaults = structuredClone(value.defaults);
  if (!value.existing || JSON.stringify(value.modules) !== JSON.stringify(defaults.modules ?? [])) defaults.modules = [...value.modules];
  if (!value.existing || value.autoTitle !== (defaults.automatic_title ?? true)) defaults.automatic_title = value.autoTitle;
  if (!value.existing || value.goalLoop !== (defaults.goals_enabled ?? false)) defaults.goals_enabled = value.goalLoop;
  const instructions = { project_root: defaults.instructions?.project_root ?? null, text: value.instructions,
    project_files: value.projectFiles.split(',').map(item => item.trim()).filter(Boolean), discover_skills: value.skillDiscovery,
    standing_instructions: value.standingInstructions, skill_roots: defaults.instructions?.skill_roots ?? [] };
  const previous = defaults.instructions;
  if (!value.existing || value.instructions !== (previous?.text ?? '') || value.projectFiles !== (previous?.project_files ?? []).join(', ') || value.skillDiscovery !== (previous?.discover_skills ?? false) || value.standingInstructions !== (previous?.standing_instructions ?? true)) defaults.instructions = instructions;
  const document: DefinitionDocument = { id: value.id.trim(), name: value.name.trim() || value.id.trim(), defaults };
  assertValid('DefinitionDocument', document);
  return document;
}

export function AgentsSettings({ client, enabled }: { client: Client; enabled: boolean }) {
  const runtimeId = client.runtimeID;
  const definitions = useDefinitions(client, enabled);
  const builtin = client.builtins.find(ref => ref.id === 'coding') ?? client.builtins[0];
  // The built-in document supplies available module names, never grants.
  const catalog = useQuery({
    queryKey: ['definition', runtimeId, builtin],
    queryFn: ({ signal }) => { if (!builtin) throw new Error('No built-in definition advertised'); return client.agents.get(builtin, { signal }); },
    enabled: enabled && !!builtin,
  });
  const [editing, setEditing] = useState<{ key: string; values: AgentValues }>({ key: 'new', values: blank });
  const [editingDirty, setEditingDirty] = useState(false);
  const [editorOpen, setEditorOpen] = useState(false);
  const [loadError, setLoadError] = useState<unknown>();
  const opening = useRef<AbortController | null>(null);
  useEffect(() => () => opening.current?.abort(), [client]);
  const items = definitions.query.data?.items ?? [];
  const isBuiltin = (item: DefinitionSummary) => client.builtins.some(ref => ref.id === item.ref.id && ref.revision === item.ref.revision);
  async function open(item: DefinitionSummary) {
    if (!enabled) return;
    opening.current?.abort(); const controller = new AbortController(); opening.current = controller;
    setLoadError(undefined);
    try {
      const record = await client.agents.get(item.ref, { signal: controller.signal });
      if (controller.signal.aborted) return;
      setEditing({ key: `${item.ref.id}:${item.ref.revision}`, values: agentValues(record, isBuiltin(item)) });
      setEditorOpen(true);
    } catch (error) { if (!controller.signal.aborted) setLoadError(error); }
  }
  return <>
    <SettingsGroup title="Agents on this host" action={<Button variant="ghost" disabled={!enabled} onClick={() => { opening.current?.abort(); if (!editingDirty) setEditing({ key: `new:${Date.now()}`, values: blank }); setEditorOpen(true); }}>{editingDirty ? 'Continue editing' : 'New agent'}</Button>}>
      {editingDirty && <p role="status">Register or discard the current draft before opening another definition.</p>}
      {definitions.query.isPending && <p role="status" {...stylex.props(styles.note)}>Loading agents…</p>}
      {definitions.query.error && <ErrorNotice type="resource" owner={`${runtimeId}:definitions`} title="Could not load agents" error={definitions.query.error} />}
      <ul {...stylex.props(styles.list)} aria-label="Agent definitions">
        {items.map(item => <li key={`${item.ref.id}:${item.ref.revision}`} {...stylex.props(styles.item)}>
          <div {...stylex.props(styles.identity)}>
            <span {...stylex.props(styles.name)}>{item.name || item.ref.id}</span>
            <span {...stylex.props(styles.note)}>{isBuiltin(item) ? 'Built in · ' : ''}{item.ref.id} · revision {item.ref.revision.slice(0, 12)}</span>
          </div>
          <Button variant="ghost" disabled={!enabled || editingDirty} onClick={() => void open(item)}>{isBuiltin(item) ? 'Copy to new agent' : 'Edit revision'}</Button>
        </li>)}
      </ul>
      {definitions.query.hasNextPage && <Button disabled={!enabled || definitions.query.isFetchingNextPage} onClick={() => void definitions.query.fetchNextPage()}>Load more definitions</Button>}
      {definitions.truncated && <p role="status">Showing the first 1,000 revisions. Open an exact revision through the SDK to inspect older entries.</p>}
      <ErrorNotice type="action" owner={`${runtimeId}:definition-open`} title="Could not open the agent" error={loadError} />
    </SettingsGroup>
    <AgentEditor key={editing.key} client={client} enabled={enabled} initial={editing.values} open={editorOpen} onOpenChange={setEditorOpen}
      modules={catalog.data?.document.defaults.modules ?? []} onDirty={setEditingDirty} />
    <ErrorNotice type="resource" owner={`${runtimeId}:module-catalog`} title="Could not read built-in modules" error={catalog.error} />
  </>;
}

function AgentEditor({ client, enabled, initial, open, onOpenChange, modules, onDirty }: { client: Client; enabled: boolean; initial: AgentValues; open: boolean; onOpenChange(open: boolean): void; modules: readonly Module[]; onDirty(dirty: boolean): void }) {
  const runtime = useRuntime();
  const [error, setError] = useState('');
  const [errorType, setErrorType] = useState<'validation' | 'action'>('action');
  const [notice, setNotice] = useState('');
  const nameInput = useRef<HTMLInputElement>(null);
  const request = useRef<AbortController | null>(null);
  const saved = useRef(false);
  useEffect(() => () => { request.current?.abort(); request.current = null; }, [client]);
  const form = useForm({
    defaultValues: initial,
    onSubmit: async ({ value }) => {
      if (!enabled || request.current) return;
      setError(''); setNotice(''); setErrorType('action');
      if (!definitionIdPattern.test(value.id.trim())) { setErrorType('validation'); setError('Use a lowercase id of letters, digits, and hyphens, 2 to 64 characters, starting with a letter.'); return; }
      const controller = new AbortController();
      request.current = controller;
      try {
        const registered = await client.agents.register(agentDocument(value), { signal: controller.signal });
        if (controller.signal.aborted) return;
        saved.current = true;
        form.reset(value);
        setNotice(`Registered ${registered.ref.id} as revision ${registered.ref.revision.slice(0, 12)}. Existing sessions keep their revision.`);
        void runtime.queries.invalidateQueries({ queryKey: definitionsQueryKey(client) });
      } catch (value) {
        if (!controller.signal.aborted) setError(value instanceof Error ? value.message : String(value));
      } finally { if (!controller.signal.aborted) request.current = null; }
    },
  });
  const dirty = useStore(form.store, state => !state.isDefaultValue);
  const submitting = useStore(form.store, state => state.isSubmitting);
  useEffect(() => { onDirty(dirty); }, [dirty, onDirty]);
  useSettingsEdits({
    id: 'agent-editor', dirty, description: 'The agent editor has unsaved changes on this execution host.',
    discard: () => { form.reset(initial); setError(''); },
    save: async () => { saved.current = false; await form.handleSubmit(); return saved.current; },
  });
  const toggle = (list: Module[], name: Module, checked: boolean) => checked ? [...list.filter(item => item !== name), name] : list.filter(item => item !== name);
  const text = (name: 'id' | 'name' | 'projectFiles', label: string, description: string, placeholder?: string) => <form.Field key={name} name={name}>{field =>
    <Field label={label} description={description}>
      <Input ref={name === 'id' ? nameInput : undefined} aria-label={label} xstyle={name === 'projectFiles' ? styles.identifier : undefined} disabled={!enabled || submitting} placeholder={placeholder} value={field.state.value}
        onBlur={field.handleBlur} onChange={event => field.handleChange(event.target.value)} />
    </Field>}
  </form.Field>;
  const prose = (name: 'instructions', label: string, description: string, placeholder: string) => <form.Field key={name} name={name}>{field =>
    <Field label={label} description={description}>
      <Textarea aria-label={label} xstyle={styles.prose} rows={3} disabled={!enabled || submitting} placeholder={placeholder}
        value={field.state.value} onBlur={field.handleBlur} onChange={event => field.handleChange(event.target.value)} />
    </Field>}
  </form.Field>;
  const flag = (name: 'skillDiscovery' | 'standingInstructions' | 'autoTitle' | 'goalLoop', label: string, description: string) => <form.Field key={name} name={name}>{field =>
    <Switch aria-label={label} label={label} description={description} checked={field.state.value} disabled={!enabled || submitting} onCheckedChange={field.handleChange} />}
  </form.Field>;
  const choices = (name: 'modules', label: string, description: string, options: readonly Module[]) => <form.Field key={name} name={name}>{field =>
    <Fieldset legend={label} description={description} xstyle={styles.choiceField}>
      <div {...stylex.props(styles.choices)}>
        {options.map(option => <Checkbox key={option} label={option} checked={field.state.value.includes(option)} disabled={!enabled || submitting}
          onCheckedChange={checked => field.handleChange(toggle(field.state.value, option, checked === true))} />)}
        {!options.length && <span {...stylex.props(styles.note)}>The host has not advertised module choices.</span>}
      </div>
    </Fieldset>}
  </form.Field>;
  return <Dialog open={open} onOpenChange={onOpenChange} initialFocus={nameInput} title={initial.id ? `Edit ${initial.id}` : 'New agent'} xstyle={styles.editor}
    footer={<><Button type="submit" form="agent-editor" variant="primary" loading={submitting} disabled={!enabled || submitting || !dirty && !!notice}>Register agent</Button>
      {dirty && <Button disabled={submitting} onClick={() => { form.reset(); setError(''); }}>Discard draft</Button>}</>}>
    <form id="agent-editor" aria-label="Agent editor" {...stylex.props(styles.form)} onSubmit={event => { event.preventDefault(); void form.handleSubmit(); }}>
      {text('id', 'Agent id', 'Lowercase letters, digits, and hyphens. Reuse an id to add a revision.', 'support-triage')}
      {text('name', 'Display name', 'A human-readable name for this immutable revision.', 'Support triage')}
      {prose('instructions', 'Instructions', 'Agent persona and operating rules.', 'Who is this agent, and what does it do?')}
      {choices('modules', 'Host modules', 'Functions visible to the agent. Module visibility does not grant permission.', [...new Set([...modules, ...initial.modules])])}
      <Collapsible title="More options">
        <div {...stylex.props(styles.options)}>
          {text('projectFiles', 'Project files', 'Comma-separated instruction files. Leave empty to skip discovery.', 'CLAUDE.md, AGENTS.md')}
          {flag('skillDiscovery', 'Skill discovery', 'Offer project and user skill catalogs.')}
          {flag('standingInstructions', 'Standing instructions', 'Include your me.md rules.')}
          {flag('autoTitle', 'Automatic title', 'Name sessions from their first message.')}
          {flag('goalLoop', 'Goal loop', 'Continue until the agent reports done.')}
          <p {...stylex.props(styles.note)}>Tools, hooks, child definitions and output policies remain in the document; callback implementations belong to the SDK executor.</p>
        </div>
      </Collapsible>
      {error && <ErrorNotice type={errorType} owner="agent-editor" title="Could not register the agent" error={error} />}
      {notice && <p role="status" {...stylex.props(styles.note)}>{notice}</p>}
    </form>
  </Dialog>;
}

const styles = stylex.create({
  list: { listStyle: 'none', margin: 0, padding: 0, display: 'flex', flexDirection: 'column' },
  item: { display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: scale.space3, paddingBlock: scale.space2, borderBottomWidth: 1, borderBottomStyle: 'solid', borderBottomColor: surface.quietBorder },
  identity: { display: 'flex', flexDirection: 'column', gap: 2, minWidth: 0 },
  name: { fontWeight: 500 },
  note: { fontSize: typography.size12, color: surface.secondaryText, margin: 0, lineHeight: 1.5 },
  editor: { width: 'min(800px, calc(100vw - 32px))', padding: scale.space5, gap: scale.space4 },
  form: { display: 'flex', flexDirection: 'column', gap: scale.space4, minWidth: 0 },
  identifier: { fontFamily: typography.mono },
  prose: { minHeight: 60 },
  choiceField: { gap: scale.space2 },
  choices: { display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(min(100%, 9em), 1fr))', columnGap: scale.space2, rowGap: scale.space1 },
  options: { display: 'flex', flexDirection: 'column', gap: scale.space3, paddingTop: scale.space3 },
});
