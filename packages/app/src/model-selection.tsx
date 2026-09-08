import { useMemo, useRef, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import { Button, Combobox, Field, Input, Popover, Select } from '@whip/ui';
import { Check, ChevronDown, Search, SlidersHorizontal, Sparkles } from 'lucide-react';
import * as stylex from '@stylexjs/stylex';
import { colors, surface } from '@whip/ui/tokens.stylex';
import { useRuntime } from './context';
import { layout } from './styles';
import { Action, Empty, type InspectorProps } from './details/shared';

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

/** Merge the same model id reported by several provider catalogs. */
function catalogModels(catalog: CatalogResult | undefined): Map<string, CatalogModel & { providers: string[] }> {
  const merged = new Map<string, CatalogModel & { providers: string[] }>();
  for (const [provider, entry] of Object.entries(catalog?.catalogs ?? {}))
    for (const model of entry.models ?? []) {
      const existing = merged.get(model.id);
      if (existing) existing.providers.push(provider);
      else merged.set(model.id, { ...model, providers: [provider] });
    }
  return merged;
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
  const models = useMemo(() => catalogModels(catalog.data?.result), [catalog.data]);
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

/** Detail card for the hovered/highlighted catalog model, mirroring the mock. */
function ModelCard({ model, top }: { model: CatalogModel & { providers: string[] }; top: number }) {
  const modalities = model.input_modalities?.filter(item => item !== 'text') ?? [];
  return <div {...stylex.props(styles.card)} style={{ top }}>
    <div {...stylex.props(styles.cardRow)}><span {...stylex.props(styles.cardLabel)}>Model</span><span {...stylex.props(styles.cardValue)} title={model.id}>{model.id}</span></div>
    <div {...stylex.props(styles.cardRow)}><span {...stylex.props(styles.cardLabel)}>Provider</span><span {...stylex.props(styles.cardValue)}>{model.providers.join(', ')}</span></div>
    <div {...stylex.props(styles.cardRow)}><span {...stylex.props(styles.cardLabel)}>Inputs</span><span {...stylex.props(styles.cardValue)}>{['text', ...modalities].join(', ')}</span></div>
    <div {...stylex.props(styles.cardRow)}><span {...stylex.props(styles.cardLabel)}>Reasoning</span><span {...stylex.props(styles.cardValue)}>{model.reasoning_efforts?.length ? 'Yes' : 'No'}</span></div>
    {!!model.context_length && <div {...stylex.props(styles.cardRow)}><span {...stylex.props(styles.cardLabel)}>Context</span><span {...stylex.props(styles.cardValue)}>{model.context_length.toLocaleString()}</span></div>}
  </div>;
}

export function ModelPicker({ view, root, connected }: ModelProps) {
  const runtime = useRuntime();
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState('');
  const [highlighted, setHighlighted] = useState(root.meta.model);
  const [hovered, setHovered] = useState(false);
  const [anchorTop, setAnchorTop] = useState(0);
  const row = useRef<HTMLDivElement>(null);
  const idle = !Object.keys(root.active_turns ?? {}).length;
  const catalog = useCatalog(view, connected);
  const models = useMemo(() => catalogModels(catalog.data?.result), [catalog.data]);
  const names = useMemo(() => [...models.keys()].sort((left, right) => left.localeCompare(right)), [models]);
  const filtered = query.trim()
    ? names.filter(name => name.toLowerCase().includes(query.trim().toLowerCase()))
    : names;
  const searching = !!query.trim();
  const ordered = searching || !names.includes(root.meta.model)
    ? filtered
    : [root.meta.model, ...filtered.filter(name => name !== root.meta.model)];
  const selected = models.get(root.meta.model);
  const detail = models.get(highlighted) ?? selected;
  const pick = (name: string) => {
    setOpen(false);
    if (!name || name === root.meta.model) return;
    const provider = models.get(name)?.providers[0] ?? root.meta.provider;
    runtime.run(view.session.setModel(name, provider), 'Change model').catch(error => runtime.report(error));
  };
  return <Popover open={open} onOpenChange={setOpen} xstyle={styles.popupWide}
    trigger={<Button variant="ghost" aria-label="Model" disabled={!connected}
      title={`${root.meta.model || 'Model unavailable'}${root.meta.provider ? ` · ${root.meta.provider}` : ''}`}
      xstyle={styles.trigger}>
      <Sparkles size={14} {...stylex.props(styles.chevron)} />
      <span {...stylex.props(layout.ellipsis)}>{root.meta.model || 'Choose model'}</span>
      <ChevronDown size={14} {...stylex.props(styles.chevron)} />
    </Button>}>
    {open && <div ref={row} {...stylex.props(styles.pickerRow)}>
      <div {...stylex.props(layout.column, styles.pickerList)}>
        <div {...stylex.props(styles.searchBox)}>
          <Search size={14} {...stylex.props(styles.searchIcon)} />
          <input aria-label="Search models" placeholder="Search models" value={query} autoFocus
            {...stylex.props(styles.searchInput)}
            onChange={event => setQuery(event.target.value)} />
        </div>
        <div role="listbox" aria-label="Models" aria-activedescendant={root.meta.model} {...stylex.props(styles.list)}>
          {catalog.isLoading && <p role="status" {...stylex.props(styles.listMeta)}>Loading models…</p>}
          {!catalog.isLoading && !filtered.length && <p {...stylex.props(styles.listMeta)}>No matching models</p>}
          {ordered.slice(0, 200).map(name => <button key={name} id={name} role="option" aria-selected={name === root.meta.model}
            {...stylex.props(styles.option, name === highlighted && styles.optionActive)}
            onMouseEnter={event => {
              setHighlighted(name);
              setHovered(true);
              const container = row.current;
              if (container) {
                const rect = event.currentTarget.getBoundingClientRect();
                setAnchorTop(rect.top - container.getBoundingClientRect().top);
              }
            }}
            onMouseLeave={() => setHovered(false)}
            onClick={() => pick(name)}>
            <span {...stylex.props(layout.ellipsis, layout.grow)}>{name}</span>
            {name === root.meta.model && <Check size={14} {...stylex.props(styles.check)} />}
          </button>)}
        </div>
        <div {...stylex.props(styles.footer)}>
          <Link to="/settings" search={{ section: 'providers' }} {...stylex.props(styles.manage)} onClick={() => setOpen(false)}>
            <SlidersHorizontal size={14} /> Manage models
          </Link>
        </div>
      </div>
      {detail && hovered && <ModelCard model={detail} top={anchorTop} />}
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
  const models = useMemo(() => catalogModels(catalog.data?.result), [catalog.data]);
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
  childModel: { color: surface.secondaryText, fontSize: 12, paddingInline: 6, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' },
  popup: { padding: 4, maxHeight: 'min(320px, var(--available-height))', overflow: 'auto' },
  menu: { display: 'flex', flexDirection: 'column', gap: 2, minWidth: 140 },
  menuItem: { display: 'flex', alignItems: 'center', gap: 8, width: '100%', paddingBlock: 5, paddingInline: 8, borderRadius: 6, borderWidth: 0, backgroundColor: { default: 'transparent', ':hover': colors.hover }, color: colors.foreground, font: 'inherit', fontSize: 13, textAlign: 'start', cursor: 'default', minHeight: 26 },
  popupWide: { padding: 4, maxHeight: 'min(420px, var(--available-height))', overflow: 'visible' },
  pickerRow: { position: 'relative' },
  pickerList: { width: 'min(300px, calc(100vw - 48px))' },
  searchBox: { display: 'flex', alignItems: 'center', gap: 8, paddingInline: 8, paddingBlock: 6, borderBottomWidth: 1, borderBottomStyle: 'solid', borderBottomColor: surface.quietBorder, marginBottom: 4 },
  searchIcon: { color: surface.secondaryText, flexShrink: 0 },
  searchInput: { borderWidth: 0, outline: 'none', backgroundColor: 'transparent', color: colors.foreground, font: 'inherit', fontSize: 13, width: '100%', padding: 0 },
  list: { overflowY: 'auto', maxHeight: 320, minHeight: 0, paddingBottom: 12, maskImage: 'linear-gradient(to bottom, black calc(100% - 24px), transparent)', WebkitMaskImage: 'linear-gradient(to bottom, black calc(100% - 24px), transparent)' },
  listMeta: { color: surface.secondaryText, fontSize: 12, padding: 8, margin: 0 },
  option: { display: 'flex', alignItems: 'center', gap: 8, width: '100%', paddingBlock: 7, paddingInline: 8, borderRadius: 6, borderWidth: 0, backgroundColor: { default: 'transparent', ':hover': colors.hover }, color: colors.foreground, font: 'inherit', fontSize: 13, textAlign: 'start', cursor: 'default', minHeight: 30 },
  optionActive: { backgroundColor: colors.hover },
  check: { color: surface.secondaryText, flexShrink: 0 },
  footer: { borderTopWidth: 1, borderTopStyle: 'solid', borderTopColor: surface.quietBorder, boxShadow: '0 -6px 10px -6px rgb(0 0 0 / 0.12)' },
  manage: { display: 'flex', alignItems: 'center', gap: 8, paddingBlock: 7, paddingInline: 8, borderRadius: 6, color: colors.foreground, fontSize: 13, textDecoration: 'none', backgroundColor: { default: 'transparent', ':hover': colors.hover } },
  card: { position: 'absolute', left: 'calc(100% + 8px)', width: 232, borderWidth: 1, borderStyle: 'solid', borderColor: surface.quietBorder, borderRadius: 8, padding: 8, display: 'flex', flexDirection: 'column', gap: 5, fontSize: 12, backgroundColor: colors.panel, boxShadow: '0 4px 16px rgb(0 0 0 / 0.10)' },
  cardRow: { display: 'flex', justifyContent: 'space-between', gap: 10, minWidth: 0 },
  cardLabel: { color: surface.secondaryText, flexShrink: 0, fontSize: 11 },
  cardValue: { textAlign: 'end', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', fontSize: 12 },
});
