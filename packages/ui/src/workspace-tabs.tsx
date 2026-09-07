import * as stylex from '@stylexjs/stylex';
import { Tabs } from '@base-ui/react/tabs';
import { DragDropProvider } from '@dnd-kit/react';
import { useSortable, isSortable } from '@dnd-kit/react/sortable';
import { AutoScroller, PointerSensor, PointerActivationConstraints } from '@dnd-kit/dom';
import { OptimisticSortingPlugin } from '@dnd-kit/dom/sortable';
import { X } from 'lucide-react';
import { useLayoutEffect, useRef } from 'react';
import type { ReactElement, ReactNode } from 'react';
import { Tooltip } from './actions';
import { styles, tabMarker } from './workspace-tabs.stylex';

export interface WorkspaceTabItem {
  value: string;
  label: ReactNode;
  /** Include status, project and other distinguishing text when the visual label is shortened. */
  accessibleLabel?: string;
  /** A link (including a router link) owns navigation. Button-only items use onValueChange. */
  render?: ReactElement;
  status?: ReactNode;
  metadata?: ReactNode;
  tooltip?: ReactNode;
  menu?: ReactNode;
  /** Compose an application-owned context menu around the tab and its sibling controls. */
  wrap?: (tab: ReactElement) => ReactElement;
}

export interface WorkspaceTabsProps {
  items: readonly WorkspaceTabItem[];
  value: string | null;
  onValueChange?: (value: string) => void;
  onClose: (value: string) => void;
  onReorder?: (values: string[]) => void;
  utilities?: ReactNode;
  label?: string;
  /** ID of the application's single route panel; no hidden panels or content are mounted here. */
  panelId?: string;
}

export function workspaceTabId(value: string): string {
  return `whip-workspace-tab-${encodeURIComponent(value)}`;
}

const pointerSensors = [PointerSensor.configure({
  activationConstraints: [new PointerActivationConstraints.Distance({ value: 6 })],
  preventActivation: event => event.pointerType === 'touch' || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey,
})];
// Default feedback/selection plugins inject CSS and conflict with the renderer's CSP.
// Sortable movement uses native Web Animations; keyboard reordering belongs to the app's menu.
const dragPlugins = [AutoScroller];
const sortablePlugins = [OptimisticSortingPlugin];
const transition = { duration: 100, easing: 'ease-out' };

export function WorkspaceTabs({ items, value, onValueChange, onClose, onReorder, utilities, label = 'Open sessions', panelId }: WorkspaceTabsProps) {
  const strip = useRef<HTMLDivElement>(null);
  const list = useRef<HTMLDivElement>(null);
  const controls = useRef(new Map<string, HTMLElement>());
  const pendingFocus = useRef<{ closed: string; next?: string } | null>(null);
  const activeExists = items.some(item => item.value === value);

  function reveal(control: HTMLElement | undefined) {
    if (!control || !list.current) return;
    const bounds = list.current.getBoundingClientRect();
    const tab = (control.closest('[data-workspace-tab]') ?? control).getBoundingClientRect();
    const delta = tab.left < bounds.left ? tab.left - bounds.left : tab.right > bounds.right ? tab.right - bounds.right : 0;
    const zoom = bounds.width / list.current.offsetWidth || 1;
    if (delta) list.current.scrollBy({ left: delta / zoom, behavior: 'instant' });
  }

  useLayoutEffect(() => {
    if (value !== null) reveal(controls.current.get(value));
  }, [value, activeExists]);

  useLayoutEffect(() => {
    const pending = pendingFocus.current;
    if (!pending || items.some(item => item.value === pending.closed)) return;
    pendingFocus.current = null;
    const next = controls.current.get(pending.next ?? '') ?? controls.current.get(value ?? '');
    if (next) { next.focus({ preventScroll: true }); reveal(next); }
    else strip.current?.querySelector<HTMLElement>('[data-tab-utilities] button, [data-tab-utilities] a[href]')?.focus();
  }, [items, value]);

  function close(closed: string) {
    const control = controls.current.get(closed);
    if (control?.parentElement?.contains(control.ownerDocument.activeElement)) {
      const index = items.findIndex(item => item.value === closed);
      pendingFocus.current = { closed, next: items[index + 1]?.value ?? items[index - 1]?.value };
    }
    onClose(closed);
  }

  return <DragDropProvider sensors={pointerSensors} plugins={dragPlugins} onDragStart={(event, manager) => {
    // Pointer collisions need initial geometry even though we do not render a floating clone.
    if (isSortable(event.operation.source)) manager.dragOperation.shape = event.operation.source.sortable.refreshShape() ?? null;
  }} onDragMove={(event, manager) => {
    if (!event.to) return;
    // Without floating feedback, pointer movement must explicitly refresh collisions.
    event.preventDefault();
    manager.dragOperation.position.current = event.to;
    manager.collisionObserver.forceUpdate();
  }} onDragEnd={event => {
    const { source } = event.operation;
    if (event.canceled || !onReorder || !isSortable(source)) return;
    const from = items.findIndex(item => item.value === source.id);
    const to = source.index;
    if (from < 0 || from === to || to < 0 || to >= items.length) return;
    const values = items.map(item => item.value);
    const [moved] = values.splice(from, 1);
    values.splice(to, 0, moved!);
    onReorder(values);
  }}>
    <Tabs.Root ref={strip} value={value} onValueChange={(next, details) => {
      const item = items.find(item => item.value === next);
      if (!item || item.render || details.reason !== 'none') return;
      onValueChange?.(item.value);
    }} {...stylex.props(styles.row)}>
      {/* aria-owns keeps sibling action buttons outside the tablist's required tab-only ownership. */}
      {items.length > 0 && <div role="tablist" aria-label={label} aria-owns={items.map(item => workspaceTabId(item.value)).join(' ')} {...stylex.props(styles.semanticList)}/>}
      <Tabs.List ref={list} activateOnFocus={false} role="presentation" data-workspace-tab-strip {...stylex.props(styles.list)}>
        {items.map((item, index) => <WorkspaceTab key={item.value} item={item} index={index} active={item.value === value} panelId={panelId} reorderable={!!onReorder}
          controlRef={element => { if (element) controls.current.set(item.value, element); else controls.current.delete(item.value); }}
          onFocus={element => reveal(element)} onClose={() => close(item.value)}/>) }
      </Tabs.List>
      {utilities && <div data-tab-utilities {...stylex.props(styles.utilities)}>{utilities}</div>}
    </Tabs.Root>
  </DragDropProvider>;
}

function WorkspaceTab({ item, index, active, panelId, reorderable, controlRef, onClose, onFocus }: {
  item: WorkspaceTabItem; index: number; active: boolean; panelId?: string; reorderable: boolean;
  controlRef: (element: HTMLElement | null) => void; onClose: () => void; onFocus: (element: HTMLElement) => void;
}) {
  const { ref, handleRef, isDragSource } = useSortable({ id: item.value, index, disabled: !reorderable, plugins: sortablePlugins, transition });
  const name = item.accessibleLabel ?? (typeof item.label === 'string' ? item.label : item.value);
  const control = <Tabs.Tab value={item.value} id={workspaceTabId(item.value)} aria-controls={active ? panelId : undefined} aria-label={item.accessibleLabel}
    nativeButton={!item.render} render={item.render} ref={element => { controlRef(element); handleRef(element); }}
    onFocus={event => onFocus(event.currentTarget)}
    onKeyDown={event => { if (event.key === 'Delete') { event.preventDefault(); onClose(); } }}
    onMouseDown={event => { if (event.button === 1) event.preventDefault(); }}
    onAuxClick={event => { if (event.button === 1) { event.preventDefault(); onClose(); } }}
    draggable={false} {...stylex.props(styles.tab, active && styles.activeText)}>
    <span aria-hidden="true" {...stylex.props(styles.status)}>{item.status}</span>
    <span {...stylex.props(styles.title)}>{item.label}</span>
    {item.metadata && <span {...stylex.props(styles.metadata)}>{item.metadata}</span>}
  </Tabs.Tab>;
  const tab = <div ref={ref} role="presentation" data-workspace-tab={item.value} data-dragging={isDragSource || undefined} {...stylex.props(tabMarker, styles.item, active && styles.active, isDragSource && styles.dragging)}>
    {item.tooltip ? <Tooltip label={item.tooltip}>{control}</Tooltip> : control}
    {item.menu && <div {...stylex.props(styles.menu, active && styles.menuVisible)}>{item.menu}</div>}
    <button type="button" aria-label={`Close ${name}`} tabIndex={active ? 0 : -1}
      onMouseDown={event => event.preventDefault()} onClick={onClose} {...stylex.props(styles.close, active && styles.closeVisible)}><X aria-hidden="true" size={13}/></button>
  </div>;
  return item.wrap ? item.wrap(tab) : tab;
}
