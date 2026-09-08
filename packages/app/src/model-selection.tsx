import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Button, Combobox, Field, Input, Popover, Select } from '@whip/ui';
import { ChevronDown } from 'lucide-react';
import * as stylex from '@stylexjs/stylex';
import { surface } from '@whip/ui/tokens.stylex';
import { useRuntime } from './context';
import { layout } from './styles';
import { Action, Empty, type InspectorProps } from './details/shared';

type ModelProps = Pick<InspectorProps, 'view' | 'root' | 'connected'>;

/** Shared by the composer picker and the session inspector. */
export function ModelSelection({ view, root, connected }: ModelProps) {
  const runtime = useRuntime();
  const [model, setModel] = useState(root.meta.model);
  const [provider, setProvider] = useState(root.meta.provider);
  const [effort, setEffort] = useState(root.meta.effort || 'off');
  const idle = !Object.keys(root.active_turns ?? {}).length;
  const catalog = useQuery({
    queryKey: ['provider-catalogs', view.session.client.getSnapshot().info?.runtime_id],
    queryFn: ({ signal }) => view.session.client.providers.catalogs({ signal }),
    enabled: connected,
  });
  return <>
    {!idle && <Empty>Wait for active turns to finish before changing the model or reasoning.</Empty>}
    <Field label="Model" description="Choose a catalog model or enter an exact model ID.">
      <Combobox label="Model" value={model} onValueChange={setModel} onInputValueChange={setModel}
        loading={catalog.isLoading}
        options={Object.keys(catalog.data?.result?.models ?? {}).slice(0, 1000).map(name => ({ value: name, label: name }))} />
    </Field>
    {catalog.error && <p role="alert" {...stylex.props(layout.error)}>Could not load the model catalog. You can still enter an exact model ID.</p>}
    <Field label="Provider">
      <Input value={provider} onChange={event => setProvider(event.target.value)} />
    </Field>
    <Action disabled={!connected || !idle || !model.trim()}
      run={() => runtime.run(view.session.setModel(model, provider), 'Change model')}>Apply model</Action>
    <Field label="Reasoning effort">
      <Select label="Reasoning effort" value={effort} onValueChange={setEffort}
        options={['off', 'minimal', 'low', 'medium', 'high', 'xhigh', 'max'].map(value => ({ value, label: value }))} />
    </Field>
    <Action disabled={!connected || !idle}
      run={() => runtime.run(view.session.setEffort(effort), 'Set reasoning effort')}>Apply effort</Action>
  </>;
}

export function SessionModelPicker({ agentId, ...props }: ModelProps & Pick<InspectorProps, 'agentId'>) {
  const [open, setOpen] = useState(false);
  const isRoot = agentId === props.view.session.rootId;
  const agent = props.root.agents?.find(item => item.id === agentId);
  const model = isRoot ? props.root.meta.model : agent?.model;
  const provider = isRoot ? props.root.meta.provider : agent?.provider;
  const effort = isRoot ? props.root.meta.effort : undefined;
  return <Popover open={open} onOpenChange={setOpen} title={isRoot ? 'Model & reasoning' : 'Agent model'} xstyle={styles.popup}
    trigger={<Button variant="ghost" aria-label="Model and reasoning" disabled={!props.connected}
      title={`${model || 'Model unavailable'}${provider ? ` · ${provider}` : ''}`}
      xstyle={styles.trigger}>
      <span {...stylex.props(layout.ellipsis)}>{model || 'Choose model'}</span>
      {effort && effort !== 'off' && <span {...stylex.props(styles.effort)}>{effort}</span>}
      <ChevronDown size={14} {...stylex.props(styles.chevron)} />
    </Button>}>
    {open && <div {...stylex.props(layout.column, styles.content)}>
      {isRoot ? <ModelSelection {...props} /> : <>
        <span>{model || 'Model unavailable'}{provider && ` · ${provider}`}</span>
        <Empty>Child-agent models are set when the agent is created. Change the root session’s model from its composer.</Empty>
      </>}
    </div>}
  </Popover>;
}

const styles = stylex.create({
  trigger: { minWidth: 0, maxWidth: 260, flexShrink: 1, paddingInline: 6 },
  effort: { color: surface.secondaryText, fontSize: 12 },
  chevron: { flexShrink: 0 },
  popup: { padding: 12, maxHeight: 'min(520px, var(--available-height))' },
  content: { width: 'min(300px, calc(100vw - 48px))', paddingTop: 12 },
});
