import { useEffect, useRef, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { useForm, useStore } from '@tanstack/react-form';
import type { WhipClient } from '@whip/sdk';
import { defineAgent, type Definition } from '@whip/sdk/agents';
import { Button, Checkbox, Input, Switch, Textarea } from '@whip/ui';
import * as stylex from '@stylexjs/stylex';
import { surface, typography, scale } from '@whip/ui/tokens.stylex';
import { useRuntime } from '../context';
import { ErrorNotice } from '../error-feedback';
import { layout } from '../styles';
import { definitionIdPattern } from '../session-tabs';
import { definitionsQueryKey, useDefinitions, type DefinitionSummary } from '../definitions';
import { SettingsGroup, SettingRow, settingsSection } from './section-layout';
import { useSettingsEdits } from './unsaved';

/** The editable, data-only part of a definition. Tools and hooks need code and stay with the SDK. */
interface AgentValues {
  id: string; persona: string; rules: string; projectFiles: string;
  skillDiscovery: boolean; standingInstructions: boolean;
  modules: string[]; capabilities: string[];
  autoTitle: boolean; goalLoop: boolean;
}
const blank: AgentValues = { id: '', persona: '', rules: '', projectFiles: '', skillDiscovery: false, standingInstructions: true, modules: [], capabilities: [], autoTitle: true, goalLoop: false };

/** Loads a stored definition into the editor; a built-in gets a fresh id because its own is reserved. */
export function agentValues(definition: Definition, builtIn: boolean): AgentValues {
  return {
    id: builtIn ? `${definition.id}-custom` : definition.id,
    persona: definition.instructions.persona, rules: definition.instructions.rules,
    projectFiles: (definition.instructions.project_files ?? []).join(', '),
    skillDiscovery: definition.instructions.skill_discovery, standingInstructions: definition.instructions.standing_instructions,
    modules: [...(definition.modules ?? [])], capabilities: [...(definition.capabilities ?? [])],
    autoTitle: definition.surface.auto_title, goalLoop: definition.surface.goal_loop,
  };
}

/** Builds the canonical document the daemon stores. The daemon is the authority on validation. */
export function agentDocument(value: AgentValues): Definition {
  return defineAgent({
    id: value.id.trim(),
    instructions: {
      persona: value.persona.trim(), rules: value.rules.trim(),
      projectFiles: value.projectFiles.split(',').map(item => item.trim()).filter(Boolean),
      skillDiscovery: value.skillDiscovery, standingInstructions: value.standingInstructions,
    },
    modules: value.modules, capabilities: value.capabilities,
    surface: { autoTitle: value.autoTitle, goalLoop: value.goalLoop },
  }).document;
}

export function AgentsSettings({ client, enabled }: { client: WhipClient; enabled: boolean }) {
  const runtimeId = client.getSnapshot().info?.runtime_id;
  const definitions = useDefinitions(client, enabled);
  // The coding agent selects every module and capability, so it is the catalog.
  const catalog = useQuery({
    queryKey: ['definition', runtimeId, 'coding', ''],
    queryFn: ({ signal }) => client.agents.get('coding', undefined, { signal }),
    enabled: definitions.supported,
  });
  const [editing, setEditing] = useState<{ key: string; values: AgentValues }>({ key: 'new', values: blank });
  const [loadError, setLoadError] = useState<unknown>();
  if (!definitions.supported) {
    return enabled ? <SettingsGroup title="Agents"><p role="status" {...stylex.props(styles.note)}>This host does not support agent definitions. Update it to author agents here.</p></SettingsGroup> : null;
  }
  const items = definitions.query.data?.items ?? [];
  async function open(item: DefinitionSummary) {
    setLoadError(undefined);
    try {
      const record = await client.agents.get(item.id, item.built_in ? undefined : item.revision);
      setEditing({ key: `${item.id}:${item.revision}`, values: agentValues(record.definition, record.built_in) });
    } catch (error) { setLoadError(error); }
  }
  return <>
    <SettingsGroup title="Agents on this host" action={<Button variant="ghost" disabled={!enabled} onClick={() => setEditing({ key: `new:${Date.now()}`, values: blank })}>New agent</Button>}>
      {definitions.query.isPending && <p role="status" {...stylex.props(styles.note)}>Loading agents…</p>}
      {definitions.query.error && <ErrorNotice type="resource" owner={`${runtimeId}:definitions`} title="Could not load agents" error={definitions.query.error} />}
      <ul {...stylex.props(styles.list)} aria-label="Agent definitions">
        {items.map(item => <li key={`${item.id}:${item.revision}`} {...stylex.props(styles.item)}>
          <div {...stylex.props(styles.identity)}>
            <span {...stylex.props(styles.name)}>{item.id}</span>
            <span {...stylex.props(styles.note)}>{item.built_in ? 'Built in' : `Registered by ${item.registered_by || 'a client'} · revision ${item.revision.slice(0, 12)}`}</span>
          </div>
          <Button variant="ghost" disabled={!enabled} onClick={() => void open(item)}>{item.built_in ? 'Copy to new agent' : 'Edit'}</Button>
        </li>)}
      </ul>
      <ErrorNotice type="action" owner={`${runtimeId}:definition-open`} title="Could not open the agent" error={loadError} />
    </SettingsGroup>
    <AgentEditor key={editing.key} client={client} enabled={enabled} initial={editing.values}
      modules={catalog.data?.definition.modules ?? []} capabilities={catalog.data?.definition.capabilities ?? []} />
  </>;
}

function AgentEditor({ client, enabled, initial, modules, capabilities }: { client: WhipClient; enabled: boolean; initial: AgentValues; modules: readonly string[]; capabilities: readonly string[] }) {
  const runtime = useRuntime();
  const [error, setError] = useState('');
  const [errorType, setErrorType] = useState<'validation' | 'action'>('action');
  const [notice, setNotice] = useState('');
  const request = useRef<AbortController | null>(null);
  const saved = useRef(false);
  useEffect(() => () => { request.current?.abort(); request.current = null; }, [client]);
  const form = useForm({
    defaultValues: initial,
    onSubmit: async ({ value }) => {
      if (!enabled || request.current) return;
      setError(''); setNotice(''); setErrorType('action');
      if (!definitionIdPattern.test(value.id.trim())) { setErrorType('validation'); setError('Use a lowercase id of letters, digits, and hyphens, 2 to 64 characters, starting with a letter.'); return; }
      if (!value.modules.length) { setErrorType('validation'); setError('Select at least one host module.'); return; }
      const controller = new AbortController();
      request.current = controller;
      try {
        const registered = await client.agents.register(agentDocument(value), { signal: controller.signal });
        if (controller.signal.aborted) return;
        saved.current = true;
        form.reset(value);
        setNotice(registered.created ? `Registered ${registered.id} as revision ${registered.revision.slice(0, 12)}. New sessions can select it.` : `${registered.id} is unchanged; revision ${registered.revision.slice(0, 12)} already exists.`);
        void runtime.queries.invalidateQueries({ queryKey: definitionsQueryKey(client) });
      } catch (value) {
        if (!controller.signal.aborted) setError(value instanceof Error ? value.message : String(value));
      } finally { if (!controller.signal.aborted) request.current = null; }
    },
  });
  const dirty = useStore(form.store, state => !state.isDefaultValue);
  const submitting = useStore(form.store, state => state.isSubmitting);
  useSettingsEdits({
    id: 'agent-editor', dirty, description: 'The agent editor has unsaved changes on this execution host.',
    discard: () => { form.reset(initial); setError(''); },
    save: async () => { saved.current = false; await form.handleSubmit(); return saved.current; },
  });
  const toggle = (list: string[], name: string, checked: boolean) => checked ? [...list.filter(item => item !== name), name] : list.filter(item => item !== name);
  const text = (name: 'id' | 'projectFiles', label: string, description: string, placeholder?: string) => <form.Field key={name} name={name}>{field =>
    <SettingRow id={name === 'id' ? 'agents' : `agent_${name}`} label={label} description={description}>
      <Input aria-label={label} xstyle={settingsSection.control} disabled={!enabled || submitting} placeholder={placeholder} value={field.state.value}
        onBlur={field.handleBlur} onChange={event => field.handleChange(event.target.value)} />
    </SettingRow>}
  </form.Field>;
  const prose = (name: 'persona' | 'rules', label: string, description: string, placeholder: string) => <form.Field key={name} name={name}>{field =>
    <SettingRow id={`agent_${name}`} label={label} description={description}>
      <Textarea aria-label={label} xstyle={styles.prose} rows={name === 'rules' ? 5 : 2} disabled={!enabled || submitting} placeholder={placeholder}
        value={field.state.value} onBlur={field.handleBlur} onChange={event => field.handleChange(event.target.value)} />
    </SettingRow>}
  </form.Field>;
  const flag = (name: 'skillDiscovery' | 'standingInstructions' | 'autoTitle' | 'goalLoop', label: string, description: string) => <form.Field key={name} name={name}>{field =>
    <SettingRow id={`agent_${name}`} label={label} description={description}>
      <Switch aria-label={label} checked={field.state.value} disabled={!enabled || submitting} onCheckedChange={field.handleChange} />
    </SettingRow>}
  </form.Field>;
  const choices = (name: 'modules' | 'capabilities', label: string, description: string, options: readonly string[]) => <form.Field key={name} name={name}>{field =>
    <SettingRow id={`agent_${name}`} label={label} description={description}>
      <div role="group" aria-label={label} {...stylex.props(styles.choices)}>
        {options.map(option => <Checkbox key={option} label={option} checked={field.state.value.includes(option)} disabled={!enabled || submitting}
          onCheckedChange={checked => field.handleChange(toggle(field.state.value, option, checked === true))} />)}
        {!options.length && <span {...stylex.props(styles.note)}>Loading the host's catalog…</span>}
      </div>
    </SettingRow>}
  </form.Field>;
  return <form {...stylex.props(layout.column)} aria-label="Agent editor" onSubmit={event => { event.preventDefault(); void form.handleSubmit(); }}>
    <SettingsGroup title={initial.id ? `Edit ${initial.id}` : 'New agent'}>
      {text('id', 'Agent id', 'Lowercase letters, digits, and hyphens. Registering an existing id adds a revision; running sessions keep theirs.', 'support-triage')}
      {prose('persona', 'Persona', 'The first sentence of the system prompt: who the agent is.', 'You triage support tickets for a small engineering team.')}
      {prose('rules', 'Rules', 'Operating rules that follow the runtime guide. One per line.', 'Operating rules:\n- Look tickets up before describing them.')}
      {text('projectFiles', 'Project files', 'Instruction files read along the project directory chain, comma separated. Empty disables discovery.', 'CLAUDE.md, AGENTS.md')}
      {flag('skillDiscovery', 'Skill discovery', 'Offer the project and user skill catalogs.')}
      {flag('standingInstructions', 'Standing instructions', 'Append the user’s me.md rules.')}
    </SettingsGroup>
    <SettingsGroup title="Reach">
      {choices('modules', 'Host modules', 'What the model is told about and may call. The kernel installs only these.', modules)}
      {choices('capabilities', 'Capabilities', 'Authority the agent receives at session start. Children can only narrow it.', capabilities)}
    </SettingsGroup>
    <SettingsGroup title="Surface">
      {flag('autoTitle', 'Automatic title', 'Name the session after its first turn.')}
      {flag('goalLoop', 'Goal loop', 'Allow goal commands that continue the agent until it reports done.')}
    </SettingsGroup>
    <div {...stylex.props(layout.row)}>
      <Button type="submit" variant="primary" loading={submitting} disabled={!enabled || submitting || !dirty && !!notice}>Register agent</Button>
      {notice && <span role="status" {...stylex.props(styles.note)}>{notice}</span>}
    </div>
    {error && <ErrorNotice type={errorType} owner="agent-editor" title="Could not register the agent" error={error} />}
  </form>;
}

const styles = stylex.create({
  list: { listStyle: 'none', margin: 0, padding: 0, display: 'flex', flexDirection: 'column' },
  item: { display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: scale.space3, paddingBlock: scale.space2, borderBottomWidth: 1, borderBottomStyle: 'solid', borderBottomColor: surface.quietBorder },
  identity: { display: 'flex', flexDirection: 'column', gap: 2, minWidth: 0 },
  name: { fontWeight: 500 },
  note: { fontSize: typography.size12, color: surface.secondaryText, margin: 0, lineHeight: 1.5 },
  prose: { width: '100%', minHeight: 60, resize: 'vertical', fontSize: typography.size13 },
  choices: { display: 'flex', flexWrap: 'wrap', gap: scale.space2, maxWidth: 420 },
});
