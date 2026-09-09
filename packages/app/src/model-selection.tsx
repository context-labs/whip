import { typography } from '@whip/ui/tokens.stylex';
import { useMemo, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import { Button, Combobox, Field, Input, Popover, Select, Tooltip } from '@whip/ui';
import { Check, ChevronDown, Search, SlidersHorizontal, Sparkles } from 'lucide-react';
import * as stylex from '@stylexjs/stylex';
import { colors, surface } from '@whip/ui/tokens.stylex';
import { useRuntime } from './context';
import { layout } from './styles';
import { Action, Empty, type InspectorProps } from './details/shared';
import { modelOptions } from './model-options';

type ModelProps = Pick<InspectorProps, 'view' | 'root' | 'connected'>;
type CatalogResult = Awaited<ReturnType<ModelProps['view']['session']['client']['providers']['catalogs']>>['result'];
type CatalogModel = NonNullable<CatalogResult>['catalogs'][string]['models'] extends (infer M)[] | null ? M : never;

const effortOrder = ['low', 'medium', 'high', 'xhigh', 'max'] as const;
const effortLabels: Record<string, string> = {
  off: 'Default', none: 'Default', minimal: 'Minimal', low: 'Low',
  medium: 'Medium', high: 'High', xhigh: 'Extra high', max: 'Max',
};

function useCatalog(view: ModelProps['view'], connected: boolean) {
  return useQuery({
    queryKey: ['provider-catalogs', view.session.client.getSnapshot().info?.runtime_id],
    queryFn: ({ signal }) => view.session.client.providers.catalogs({ signal }),
    enabled: connected,
  });
}

function catalogModels(catalog: CatalogResult | undefined, selectedProvider: string) {
  return new Map(modelOptions(catalog).filter(option => option.provider === selectedProvider)
    .map(option => [option.name, option.model]));
}

/** Effort levels the daemon accepts for this model: its catalog list, or every level when unknown. */
export function modelEfforts(models: Map<string, { reasoning_efforts?: null | string[] }>, model: string): string[] {
  const listed = models.get(model)?.reasoning_efforts;
  const known = listed?.filter(level => level !== 'none' && level !== 'off') ?? [];
  const levels = known.length ? known : [...effortOrder];
  return ['off', ...levels];
}

function effortLabel(level: string): string {
  return effortLabels[level] ?? level;
}

/** Effort menu: only levels the selected model supports; applies immediately. */
export function EffortPicker({ view, root, connected }: ModelProps) {
  const runtime = useRuntime();
  const [open, setOpen] = useState(false);
  const idle = !Object.keys(root.active_turns ?? {}).length;
  const catalog = useCatalog(view, connected);
  const models = useMemo(() => catalogModels(catalog.data?.result, root.meta.provider), [catalog.data, root.meta.provider]);
  const levels = modelEfforts(models, root.meta.model);
  const current = root.meta.effort || 'off';
  return <Popover open={open} onOpenChange={setOpen} xstyle={styles.popup}
    trigger={<Button variant="ghost" aria-label="Reasoning effort" disabled={!connected || !idle}
      title={idle ? 'Reasoning effort' : 'Wait for active turns to finish before changing reasoning'}
      xstyle={styles.trigger}>
      <span {...stylex.props(layout.ellipsis)}>{effortLabel(current)}</span>
      <ChevronDown size={14} {...stylex.props(styles.chevron)} />
    </Button>}>
    {open && <div {...stylex.props(styles.menu)} role="listbox" aria-label="Reasoning effort" aria-activedescendant={current}>
      {levels.map(level => <button key={level} id={level} role="option" aria-selected={level === current}
        disabled={!connected || !idle}
        {...stylex.props(styles.menuItem, level === current && styles.optionActive)}
        onClick={() => {
          setOpen(false);
          if (level !== current)
            runtime.run(view.session.setEffort(level), 'Set reasoning effort').catch(error => runtime.report(error));
        }}>
        <span {...stylex.props(layout.grow)}>{effortLabel(level)}</span>
        {level === current && <Check size={14} {...stylex.props(styles.check)} />}
      </button>)}
    </div>}
  </Popover>;
}

/** Details for the hovered or keyboard-focused catalog model. */
function ModelCard({ model }: { model: CatalogModel & { providers: string[] } }) {
  const modalities = model.input_modalities?.filter(item => item !== 'text') ?? [];
  return <>
    <div {...stylex.props(styles.cardRow)}><span {...stylex.props(styles.cardLabel)}>Model</span><span {...stylex.props(styles.cardValue)} title={model.id}>{model.id}</span></div>
    <div {...stylex.props(styles.cardRow)}><span {...stylex.props(styles.cardLabel)}>Provider</span><span {...stylex.props(styles.cardValue)}>{model.providers.join(', ')}</span></div>
    <div {...stylex.props(styles.cardRow)}><span {...stylex.props(styles.cardLabel)}>Inputs</span><span {...stylex.props(styles.cardValue)}>{['text', ...modalities].join(', ')}</span></div>
    <div {...stylex.props(styles.cardRow)}><span {...stylex.props(styles.cardLabel)}>Reasoning</span><span {...stylex.props(styles.cardValue)}>{model.reasoning_efforts?.length ? 'Yes' : 'No'}</span></div>
    {!!model.context_length && <div {...stylex.props(styles.cardRow)}><span {...stylex.props(styles.cardLabel)}>Context</span><span {...stylex.props(styles.cardValue)}>{model.context_length.toLocaleString()}</span></div>}
  </>;
}

export function ModelPicker({ view, root, connected }: ModelProps) {
  const runtime = useRuntime();
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState('');
  const current = JSON.stringify([root.meta.model, root.meta.provider]);
  const [highlighted, setHighlighted] = useState(current);
  const idle = !Object.keys(root.active_turns ?? {}).length;
  const catalog = useCatalog(view, connected);
  const options = useMemo(() => modelOptions(catalog.data?.result), [catalog.data]);
  const filtered = query.trim()
    ? options.filter(option => option.label.toLowerCase().includes(query.trim().toLowerCase()))
    : options;
  const searching = !!query.trim();
  const selected = options.find(option => option.value === current);
  const ordered = searching || !selected
    ? filtered
    : [selected, ...filtered.filter(option => option.value !== current)];
  const pick = (option: typeof options[number]) => {
    setOpen(false);
    if (option.value === current) return;
    runtime.run(view.session.setModel(option.name, option.provider), 'Change model').catch(error => runtime.report(error));
  };
  return <Popover open={open} onOpenChange={setOpen} xstyle={styles.popupWide}
    trigger={<Button variant="ghost" aria-label="Model" disabled={!connected || !idle}
      title={`${root.meta.model || 'Model unavailable'}${root.meta.provider ? ` · ${root.meta.provider}` : ''}`}
      xstyle={styles.trigger}>
      <Sparkles size={14} {...stylex.props(styles.chevron)} />
      <span {...stylex.props(layout.ellipsis)}>{root.meta.model || 'Choose model'}</span>
      <ChevronDown size={14} {...stylex.props(styles.chevron)} />
    </Button>}>
    {open && <div {...stylex.props(layout.column, styles.pickerList)}>
      <div {...stylex.props(styles.searchBox)}>
        <Search size={14} {...stylex.props(styles.searchIcon)} />
        <input aria-label="Search models" placeholder="Search models" value={query} autoFocus
          {...stylex.props(styles.searchInput)}
          onChange={event => setQuery(event.target.value)} />
      </div>
      <div role="listbox" aria-label="Models" aria-activedescendant={current} {...stylex.props(styles.list)}>
        {catalog.isLoading && <p role="status" {...stylex.props(styles.listMeta)}>Loading models…</p>}
        {!catalog.isLoading && !filtered.length && <p {...stylex.props(styles.listMeta)}>No matching models</p>}
        {ordered.slice(0, 200).map(option => <Tooltip key={option.value} label={<ModelCard model={{ ...option.model, providers: [option.provider] }} />}
          side="right" align="start" sideOffset={8} collisionPadding={8} delay={0} disableHoverablePopup xstyle={styles.card}
          collisionAvoidance={{ side: 'flip', align: 'shift', fallbackAxisSide: 'end' }}>
          <button id={option.value} role="option" aria-selected={option.value === current}
            disabled={!connected || !idle}
            {...stylex.props(styles.option, option.value === highlighted && styles.optionActive)}
            onMouseEnter={() => setHighlighted(option.value)}
            onFocus={() => setHighlighted(option.value)}
            onClick={() => pick(option)}>
            <span {...stylex.props(layout.ellipsis, layout.grow)}>{option.label}</span>
            {option.value === current && <Check size={14} {...stylex.props(styles.check)} />}
        </button></Tooltip>)}
      </div>
      <div {...stylex.props(styles.footer)}>
        <Link to="/settings" search={{ section: 'providers' }} {...stylex.props(styles.manage)} onClick={() => setOpen(false)}>
          <SlidersHorizontal size={14} /> Manage models
        </Link>
      </div>
    </div>}
  </Popover>;
}

/** Composer toolbar controls: model picker + effort menu for the viewed agent. */
export function SessionModelPicker({ agentId, ...props }: ModelProps & Pick<InspectorProps, 'agentId'>) {
  const isRoot = agentId === props.view.session.rootId;
  const agent = props.root.agents?.find(item => item.id === agentId);
  if (!isRoot) {
    const label = [agent?.model, agent?.provider].filter(Boolean).join(' · ') || 'Model unavailable';
    return <span {...stylex.props(styles.childModel)} title="Child-agent models are set when the agent is created">{label}</span>;
  }
  return <>
    <ModelPicker {...props} />
    <EffortPicker {...props} />
  </>;
}

/** Shared by the session inspector; the composer uses ModelPicker/EffortPicker. */
export function ModelSelection({ view, root, connected }: ModelProps) {
  const runtime = useRuntime();
  const [model, setModel] = useState(root.meta.model);
  const [provider, setProvider] = useState(root.meta.provider);
  const [effort, setEffort] = useState(root.meta.effort || 'off');
  const idle = !Object.keys(root.active_turns ?? {}).length;
  const catalog = useCatalog(view, connected);
  const models = useMemo(() => catalogModels(catalog.data?.result, root.meta.provider), [catalog.data, root.meta.provider]);
  return <>
    {!idle && <Empty>Wait for active turns to finish before changing the model or reasoning.</Empty>}
    <Field label="Model" description="Choose a catalog model or enter an exact model ID.">
      <Combobox label="Model" value={model} onValueChange={setModel} onInputValueChange={setModel}
        loading={catalog.isLoading}
        options={modelOptions(catalog.data?.result).filter(option => option.provider === provider).slice(0, 1000)
          .map(option => ({ value: option.name, label: option.name }))} />
    </Field>
    {catalog.error && <p role="alert" {...stylex.props(layout.error)}>Could not load the model catalog. You can still enter an exact model ID.</p>}
    <Field label="Provider">
      <Input value={provider} onChange={event => setProvider(event.target.value)} />
    </Field>
    <Action disabled={!connected || !idle || !model.trim()}
      run={() => runtime.run(view.session.setModel(model, provider), 'Change model')}>Apply model</Action>
    <Field label="Reasoning effort" description={model !== root.meta.model ? 'Levels shown for the applied model.' : undefined}>
      <Select label="Reasoning effort" value={modelEfforts(models, root.meta.model).includes(effort) ? effort : 'off'}
        onValueChange={setEffort}
        options={modelEfforts(models, root.meta.model).map(value => ({ value, label: effortLabel(value) }))} />
    </Field>
    <Action disabled={!connected || !idle}
      run={() => runtime.run(view.session.setEffort(effort), 'Set reasoning effort')}>Apply effort</Action>
  </>;
}

const styles = stylex.create({
  trigger: { minWidth: 0, maxWidth: 220, flexShrink: 1, paddingInline: 6, gap: 6 },
  chevron: { flexShrink: 0 },
  childModel: { color: surface.secondaryText, fontSize: typography.size12, paddingInline: 6, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' },
  popup: { padding: 4, maxHeight: 'min(320px, var(--available-height))', overflow: 'auto' },
  menu: { display: 'flex', flexDirection: 'column', gap: 2, minWidth: 140 },
  menuItem: { display: 'flex', alignItems: 'center', gap: 8, width: '100%', paddingBlock: 5, paddingInline: 8, borderRadius: 6, borderWidth: 0, backgroundColor: { default: 'transparent', ':hover': colors.hover }, color: colors.foreground, font: 'inherit', fontSize: typography.size13, textAlign: 'start', cursor: 'default', minHeight: 26 },
  popupWide: { display: 'flex', padding: 4, maxHeight: 'min(420px, var(--available-height))', overflow: 'hidden' },
  pickerList: { width: 'min(300px, calc(100vw - 48px))', minHeight: 0 },
  searchBox: { display: 'flex', flexShrink: 0, alignItems: 'center', gap: 8, paddingInline: 8, paddingBlock: 6, borderBottomWidth: 1, borderBottomStyle: 'solid', borderBottomColor: surface.quietBorder, marginBottom: 4 },
  searchIcon: { color: surface.secondaryText, flexShrink: 0 },
  searchInput: { borderWidth: 0, outline: 'none', backgroundColor: 'transparent', color: colors.foreground, font: 'inherit', fontSize: typography.size13, width: '100%', padding: 0 },
  list: { overflowY: 'auto', maxHeight: 320, minHeight: 0, paddingBottom: 12, maskImage: 'linear-gradient(to bottom, black calc(100% - 24px), transparent)', WebkitMaskImage: 'linear-gradient(to bottom, black calc(100% - 24px), transparent)' },
  listMeta: { color: surface.secondaryText, fontSize: typography.size12, padding: 8, margin: 0 },
  option: { display: 'flex', alignItems: 'center', gap: 8, width: '100%', paddingBlock: 7, paddingInline: 8, borderRadius: 6, borderWidth: 0, backgroundColor: { default: 'transparent', ':hover': colors.hover }, color: colors.foreground, font: 'inherit', fontSize: typography.size13, textAlign: 'start', cursor: 'default', minHeight: 30 },
  optionActive: { backgroundColor: colors.hover },
  check: { color: surface.secondaryText, flexShrink: 0 },
  footer: { flexShrink: 0, borderTopWidth: 1, borderTopStyle: 'solid', borderTopColor: surface.quietBorder, boxShadow: '0 -6px 10px -6px rgb(0 0 0 / 0.12)' },
  manage: { display: 'flex', alignItems: 'center', gap: 8, paddingBlock: 7, paddingInline: 8, borderRadius: 6, color: colors.foreground, fontSize: typography.size13, textDecoration: 'none', backgroundColor: { default: 'transparent', ':hover': colors.hover } },
  card: { width: 232, maxWidth: 'var(--available-width)', maxHeight: 'var(--available-height)', overflow: 'auto', borderWidth: 1, borderStyle: 'solid', borderColor: surface.quietBorder, borderRadius: 8, padding: 8, display: 'flex', flexDirection: 'column', gap: 5, fontSize: typography.size12, color: colors.foreground, backgroundColor: colors.panel, boxShadow: '0 4px 16px rgb(0 0 0 / 0.10)' },
  cardRow: { display: 'flex', flexShrink: 0, justifyContent: 'space-between', gap: 10, minWidth: 0 },
  cardLabel: { color: surface.secondaryText, flexShrink: 0, fontSize: typography.size11 },
  cardValue: { textAlign: 'end', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', fontSize: typography.size12 },
});
