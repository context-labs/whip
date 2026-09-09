import * as stylex from '@stylexjs/stylex';
import { Tabs } from '@base-ui/react/tabs';
import { DragDropProvider } from '@dnd-kit/react';
import { useSortable } from '@dnd-kit/react/sortable';
import { AutoScroller, PointerSensor, PointerActivationConstraints } from '@dnd-kit/dom';
import { MoreHorizontal, X } from 'lucide-react';
import { containsPoint, tabDrop, useWorkspaceTabDrag } from './workspace-tab-drag';
import { createContext, useContext, useLayoutEffect, useRef } from 'react';
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
  leading?: ReactNode;
  label?: string;
  /** ID of this strip's selected content panel; no content is mounted here. */
  panelId?: string;
  /** A pane ID enables transfers inside WorkspaceLayout's shared drag provider. */
  groupId?: string;
  /** The strip doubles as the host window's drag region (inset window chrome). */
  windowDrag?: boolean;
  /** Reserve the host's inset window-control zone at the strip's leading edge. */
  trafficLightInset?: boolean;
}

export function workspaceTabId(value: string): string {
  return `whip-workspace-tab-${encodeURIComponent(value)}`;
}

export const workspaceDragContext = createContext(false);

export const workspacePointerSensors = [PointerSensor.configure({
  activationConstraints: [new PointerActivationConstraints.Distance({ value: 6 })],
  preventActivation: event => event.pointerType === 'touch' || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey,
})];
// Default feedback/selection plugins inject CSS and conflict with the renderer's CSP.
// Sortable movement uses native Web Animations; keyboard reordering belongs to the app's menu.
export const workspaceDragPlugins = [AutoScroller];

export function WorkspaceTabs({ items, value, onValueChange, onClose, onReorder, utilities, leading, label = 'Open sessions', panelId, groupId, windowDrag = false, trafficLightInset = false }: WorkspaceTabsProps) {
  const sharedDrag = useContext(workspaceDragContext);
  const strip = useRef<HTMLDivElement>(null);
  const list = useRef<HTMLDivElement>(null);
  const controls = useRef(new Map<string, HTMLElement>());
  const pendingFocus = useRef<{ closed: string; next?: string } | null>(null);
  const activeExists = items.some(item => item.value === value);
  const drag = useWorkspaceTabDrag({
    locate: (id, point) => list.current && containsPoint(list.current.getBoundingClientRect(), point) ? tabDrop(list.current, id, point) : null,
    onDrop: ({ viewId, index }) => {
      const from = items.findIndex(item => item.value === viewId);
      if (!onReorder || from < 0 || index === undefined) return;
      const to = index > from ? index - 1 : index;
      if (from === to) return;
      const values = items.map(item => item.value);
      values.splice(to, 0, values.splice(from, 1)[0]!);
      onReorder(values);
    },
  });

  function reveal(control: HTMLElement | undefined) {
    if (!control || !list.current) return;
    const bounds = list.current.getBoundingClientRect();
    const tab = (control.closest('[data-workspace-tab]') ?? control).getBoundingClientRect();
    const zoom = bounds.width / list.current.offsetWidth || 1;
    const shoulder = parseFloat(getComputedStyle(list.current).paddingInlineStart) * zoom;
    const delta = tab.left - shoulder < bounds.left ? tab.left - shoulder - bounds.left
      : tab.right + shoulder > bounds.right ? tab.right + shoulder - bounds.right : 0;
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

  const tabs = <Tabs.Root ref={strip} value={value} onValueChange={(next, details) => {
      const item = items.find(item => item.value === next);
      if (!item || item.render || details.reason !== 'none') return;
      onValueChange?.(item.value);
    }} {...stylex.props(styles.row, windowDrag && styles.windowDrag, trafficLightInset && styles.trafficLightInset)}>
      {leading && <div {...stylex.props(styles.utilities, windowDrag && styles.windowNoDrag)}>{leading}</div>}
      {/* aria-owns keeps sibling action buttons outside the tablist's required tab-only ownership. */}
      {items.length > 0 && <div role="tablist" aria-label={label} aria-owns={items.map(item => workspaceTabId(item.value)).join(' ')} {...stylex.props(styles.semanticList)}/>}
      <Tabs.List ref={list} activateOnFocus={false} role="presentation" data-workspace-tab-strip={groupId ?? ''} {...stylex.props(styles.list)}>
        {items.map((item, index) => <WorkspaceTab key={item.value} item={item} index={index} active={item.value === value} panelId={panelId} groupId={groupId} divider={item.value !== value && items[index + 1]?.value !== value && index < items.length - 1} reorderable={sharedDrag ? !!groupId : !!onReorder} windowDrag={windowDrag}
          controlRef={element => { if (element) controls.current.set(item.value, element); else controls.current.delete(item.value); }}
          onFocus={element => reveal(element)} onClose={() => close(item.value)}/>) }
      </Tabs.List>
      {utilities && <div data-tab-utilities {...stylex.props(styles.utilities, windowDrag && styles.windowNoDrag)}>{utilities}</div>}
    </Tabs.Root>;
  if (sharedDrag) return tabs;
  return <DragDropProvider sensors={workspacePointerSensors} plugins={workspaceDragPlugins} {...drag.handlers}>{tabs}{drag.preview}</DragDropProvider>;
}

function WorkspaceTab({ item, index, active, panelId, groupId, divider, reorderable, windowDrag, controlRef, onClose, onFocus }: {
  item: WorkspaceTabItem; index: number; active: boolean; panelId?: string; groupId?: string; divider: boolean; reorderable: boolean; windowDrag: boolean;
  controlRef: (element: HTMLElement | null) => void; onClose: () => void; onFocus: (element: HTMLElement) => void;
}) {
  const { ref, handleRef, isDragSource } = useSortable({ id: item.value, index, group: groupId, disabled: !reorderable, plugins: [], transition: { duration: 0 }, data: { tabFace: <WorkspaceTabPreview item={item}/> } });
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
  // Inset window chrome: the strip drags the window; only the tab item opts
  // out, so the empty stretch of the list still drags. The tab needs no-drag
  // anyway — inside a drag region its own dnd/reorder gestures and text
  // selection would start a window move instead.
  const tab = <div ref={ref} role="presentation" data-workspace-tab={item.value} data-workspace-tab-group={groupId} data-workspace-tab-index={index} data-dragging={isDragSource || undefined} {...stylex.props(tabMarker, styles.item, active && styles.active, isDragSource && styles.dragging, windowDrag && styles.windowNoDrag)}>
    <WorkspaceTabShape active={active}/>
    {divider && <span aria-hidden {...stylex.props(styles.divider)}/>}
    {item.tooltip ? <Tooltip label={item.tooltip}>{control}</Tooltip> : control}
    {item.menu && <div {...stylex.props(styles.menu, active && styles.menuVisible)}>{item.menu}</div>}
    <button type="button" aria-label={`Close ${name}`} tabIndex={active ? 0 : -1}
      onMouseDown={event => event.preventDefault()} onClick={onClose} {...stylex.props(styles.close, active && styles.closeVisible)}><X aria-hidden="true" size={13}/></button>
  </div>;
  return item.wrap ? item.wrap(tab) : tab;
}

function WorkspaceTabShape({ active }: { active: boolean }) {
  // Fixed-width ends keep both curves round as the tab stretches. Only the
  // upper contour is stroked, leaving the base open to the content below.
  const edge = 'M0 41.5 C6.627 41.5 12 36.127 12 29.5 V12.5 C12 5.873 17.373 .5 24 .5';
  return <span aria-hidden="true" data-workspace-tab-shape {...stylex.props(styles.shape, active && styles.shapeActive)}>
    <span {...stylex.props(styles.shapeCenter, active && styles.shapeCenterActive)}/>
    {[false, true].map(right => <svg key={String(right)} viewBox="0 0 24 42" preserveAspectRatio="none" focusable="false" {...stylex.props(styles.shapeEdge, right && styles.shapeEdgeRight)}>
      <path d={`${edge} V42 H0 Z`} fill="currentColor" stroke="none"/>
      <path d={edge} fill="none" strokeWidth="1" vectorEffect="non-scaling-stroke"/>
    </svg>)}
  </span>;
}

// Shared presentation only: no tab role, IDs, buttons, portals, or drag registration.
function WorkspaceTabPreview({ item }: { item: WorkspaceTabItem }) {
  return <>
    <WorkspaceTabShape active/>
    <div {...stylex.props(styles.tab, styles.activeText, styles.dragFace)}>
      <span {...stylex.props(styles.status)}>{item.status}</span>
      <span {...stylex.props(styles.title)}>{item.label}</span>
      {item.metadata && <span {...stylex.props(styles.metadata)}>{item.metadata}</span>}
    </div>
    {item.menu && <span {...stylex.props(styles.close, styles.closeVisible)}><MoreHorizontal size={13}/></span>}
    <span {...stylex.props(styles.close, styles.closeVisible)}><X size={13}/></span>
  </>;
}
