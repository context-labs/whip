import * as stylex from '@stylexjs/stylex';
import { DragDropProvider } from '@dnd-kit/react';
import { Group, Panel, Separator } from 'react-resizable-panels';
import { useCallback, useLayoutEffect, useRef, useState } from 'react';
import type { PointerEvent, ReactNode } from 'react';
import { workspaceDragContext, workspaceDragPlugins, workspacePointerSensors } from './workspace-tabs';
import { styles } from './workspace-layout.stylex';
import { tabDrop, useWorkspaceTabDrag } from './workspace-tab-drag';
import type { TabDrop } from './workspace-tab-drag';

export type WorkspaceLayoutNode = { readonly type: 'pane'; readonly id: string } | {
  readonly type: 'split'; readonly id: string; readonly direction: 'horizontal' | 'vertical';
  readonly ratio: number; readonly first: WorkspaceLayoutNode; readonly second: WorkspaceLayoutNode;
};
export interface WorkspaceDrop {
  viewId: string;
  paneId: string;
  index?: number;
  edge?: 'left' | 'right' | 'top' | 'bottom';
}
export interface WorkspaceLayoutProps {
  layout: WorkspaceLayoutNode;
  focusedPaneId: string;
  onResize: (splitId: string, ratio: number) => void;
  onFocusPane: (paneId: string) => void;
  onDrop: (drop: WorkspaceDrop) => void;
  /** Product limits (for example, a maximum pane count) can reject a proposed drop. */
  canDrop?: (drop: WorkspaceDrop) => boolean;
  renderHeader: (paneId: string) => ReactNode;
  /** Only the selected view of each visible pane belongs here; inactive tabs never mount. */
  panels: readonly { id: string; paneId: string; content: ReactNode; label: string; labelledBy?: string }[];
  compact?: boolean;
  onCompactChange?: (compact: boolean) => void;
}

export function workspacePanelId(viewId: string): string {
  return `whip-workspace-panel-${encodeURIComponent(viewId)}`;
}

const separatorSize = 1;
function minimumSize(node: WorkspaceLayoutNode): { width: number; height: number } {
  if (node.type === 'pane') return { width: 320, height: 240 };
  const first = minimumSize(node.first), second = minimumSize(node.second);
  return node.direction === 'horizontal'
    ? { width: first.width + second.width + separatorSize, height: Math.max(first.height, second.height) }
    : { width: Math.max(first.width, second.width), height: first.height + second.height + separatorSize };
}
function findPane(node: WorkspaceLayoutNode, id: string): WorkspaceLayoutNode | undefined {
  return node.type === 'pane' ? node.id === id ? node : undefined : findPane(node.first, id) ?? findPane(node.second, id);
}
function firstPane(node: WorkspaceLayoutNode): WorkspaceLayoutNode {
  return node.type === 'pane' ? node : firstPane(node.first);
}
type Rect = { left: number; top: number; width: number; height: number };
type DropPreview = { drop: WorkspaceDrop; rect: Rect };
function contains(rect: DOMRect, point: { x: number; y: number }) {
  return point.x >= rect.left && point.x <= rect.right && point.y >= rect.top && point.y <= rect.bottom;
}

export function WorkspaceLayout({ layout, focusedPaneId, onResize, onFocusPane, onDrop, canDrop, renderHeader, panels, compact = false, onCompactChange }: WorkspaceLayoutProps) {
  const root = useRef<HTMLDivElement>(null);
  const slots = useRef(new Map<string, HTMLDivElement>());
  const [labelledPanels, setLabelledPanels] = useState<ReadonlySet<string>>(new Set());
  const panelLabels = panels.map(panel => `${panel.id}:${panel.labelledBy ?? ""}`).join("|");
  const [geometry, setGeometry] = useState<ReadonlyMap<string, Rect>>(new Map());
  const [tooSmall, setTooSmall] = useState(() => typeof window !== 'undefined' && window.innerWidth < 768);
  const [preview, setPreview] = useState<DropPreview | null>(null);
  const isCompact = compact || tooSmall;
  const displayedLayout = isCompact ? findPane(layout, focusedPaneId) ?? firstPane(layout) : layout;
  const minimum = minimumSize(layout);

  const measure = useCallback(() => {
    const element = root.current;
    if (!element || !element.clientWidth || !element.clientHeight) return;
    const labels = new Set(panels.filter(panel => panel.labelledBy && element.ownerDocument.getElementById(panel.labelledBy)).map(panel => panel.id));
    setLabelledPanels(previous => previous.size === labels.size && [...labels].every(id => previous.has(id)) ? previous : labels);
    const bounds = element.getBoundingClientRect();
    const scaleX = bounds.width / element.clientWidth || 1, scaleY = bounds.height / element.clientHeight || 1;
    setTooSmall(window.innerWidth < 768 || element.clientWidth < minimum.width || element.clientHeight < minimum.height);
    const next = new Map<string, Rect>();
    for (const [id, slot] of slots.current) {
      const rect = slot.getBoundingClientRect();
      next.set(id, { left: (rect.left - bounds.left) / scaleX, top: (rect.top - bounds.top) / scaleY, width: rect.width / scaleX, height: rect.height / scaleY });
    }
    setGeometry(previous => previous.size === next.size && [...next].every(([id, rect]) => {
      const old = previous.get(id);
      return old && old.left === rect.left && old.top === rect.top && old.width === rect.width && old.height === rect.height;
    }) ? previous : next);
  }, [minimum.width, minimum.height, panelLabels]);

  useLayoutEffect(() => {
    measure();
    const observer = new ResizeObserver(measure);
    if (root.current) observer.observe(root.current);
    for (const slot of slots.current.values()) observer.observe(slot);
    window.addEventListener('resize', measure);
    return () => { observer.disconnect(); window.removeEventListener('resize', measure); };
  }, [measure, displayedLayout, renderHeader]);
  useLayoutEffect(() => onCompactChange?.(isCompact), [isCompact, onCompactChange]);

  function relative(rect: Rect): Rect {
    const element = root.current!, bounds = element.getBoundingClientRect();
    const scaleX = bounds.width / element.clientWidth || 1, scaleY = bounds.height / element.clientHeight || 1;
    return { left: (rect.left - bounds.left) / scaleX, top: (rect.top - bounds.top) / scaleY, width: rect.width / scaleX, height: rect.height / scaleY };
  }

  function locateDrop(viewId: string, point: { x: number; y: number }): TabDrop | null {
    for (const pane of root.current?.querySelectorAll<HTMLElement>('[data-workspace-frame]') ?? []) {
      const rect = pane.getBoundingClientRect(), paneId = pane.dataset.workspaceFrame!;
      if (!contains(rect, point)) continue;
      const header = pane.querySelector<HTMLElement>('[data-workspace-header]')!;
      if (contains(header.getBoundingClientRect(), point)) {
        const strip = header.querySelector<HTMLElement>('[data-workspace-tab-strip]');
        return strip ? tabDrop(strip, viewId, point) : null;
      }
      const slot = slots.current.get(paneId);
      if (!slot || !slot.clientWidth || !slot.clientHeight) return null;
      const content = slot.getBoundingClientRect();
      const x = (point.x - content.left) / content.width, y = (point.y - content.top) / content.height;
      const distances = [{ edge: 'left' as const, distance: x }, { edge: 'right' as const, distance: 1 - x }, { edge: 'top' as const, distance: y }, { edge: 'bottom' as const, distance: 1 - y }];
      const closest = distances.sort((a, b) => a.distance - b.distance)[0]!;
      const edge = !isCompact && closest.distance < .24 ? closest.edge : undefined;
      if (edge && ((edge === 'left' || edge === 'right') ? pane.clientWidth < 640 + separatorSize : pane.clientHeight < 480 + separatorSize)) return null;
      const previewRect = { left: content.left, top: content.top, width: content.width, height: content.height };
      if (edge === 'left' || edge === 'right') { previewRect.width /= 2; if (edge === 'right') previewRect.left += previewRect.width; }
      if (edge === 'top' || edge === 'bottom') { previewRect.height /= 2; if (edge === 'bottom') previewRect.top += previewRect.height; }
      return { drop: { viewId, paneId, edge }, rect: previewRect, insertion: false };
    }
    return null;
  }

  function focusFromPointer(paneId: string, event: PointerEvent<HTMLElement>) {
    if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
    if (event.target instanceof Element && event.target.closest('[data-workspace-tab]')) return;
    onFocusPane(paneId);
  }

  function renderTree(node: WorkspaceLayoutNode): ReactNode {
    if (node.type === 'pane') return <div key={node.id} data-workspace-frame={node.id} onPointerDownCapture={event => focusFromPointer(node.id, event)} onFocusCapture={event => { if (!(event.target instanceof Element) || !event.target.closest('[data-workspace-tab]')) onFocusPane(node.id); }} {...stylex.props(styles.pane)}>
      <div data-workspace-header={node.id} {...stylex.props(styles.header)}>{renderHeader(node.id)}</div>
      <div ref={element => { if (element) slots.current.set(node.id, element); else slots.current.delete(node.id); }} data-workspace-slot={node.id} {...stylex.props(styles.slot)}/>
    </div>;
    const firstMinimum = minimumSize(node.first), secondMinimum = minimumSize(node.second);
    const horizontal = node.direction === 'horizontal';
    return <Group key={node.id} id={`workspace-split-${node.id}`} orientation={node.direction} disableCursor
      defaultLayout={{ [node.first.id]: node.ratio * 100, [node.second.id]: (1 - node.ratio) * 100 }}
      onLayoutChanged={(sizes, meta) => { if (meta.isUserInteraction) onResize(node.id, (sizes[node.first.id] ?? 50) / 100); }}>
      <Panel id={node.first.id} minSize={horizontal ? firstMinimum.width : firstMinimum.height}>{renderTree(node.first)}</Panel>
      <Separator aria-label={horizontal ? 'Resize panes horizontally' : 'Resize panes vertically'} {...stylex.props(styles.separator, horizontal ? styles.horizontalSeparator : styles.verticalSeparator)}>
        {/* Keep the pointer target above the sibling content overlays on both axes. */}
        <span aria-hidden="true" {...stylex.props(styles.separatorTarget)}/>
      </Separator>
      <Panel id={node.second.id} minSize={horizontal ? secondMinimum.width : secondMinimum.height}>{renderTree(node.second)}</Panel>
    </Group>;
  }

  const drag = useWorkspaceTabDrag({
    locate: (id, point) => {
      const candidate = locateDrop(id, point);
      return candidate && (!canDrop || canDrop(candidate.drop)) ? candidate : null;
    },
    onDrop,
    onPreview: target => setPreview(target && !target.insertion ? { drop: target.drop, rect: relative(target.rect) } : null),
  });
  return <DragDropProvider sensors={workspacePointerSensors} plugins={workspaceDragPlugins} {...drag.handlers}>
    <workspaceDragContext.Provider value={true}>
      <div ref={root} data-workspace-layout data-workspace-compact={isCompact || undefined} {...stylex.props(styles.workspace)}>
        <div {...stylex.props(styles.tree)}>{renderTree(displayedLayout)}</div>
        {/* Stable sibling order matters too: DOM reordering can reset scroll and focused inputs. */}
        {panels.filter(panel => !isCompact || panel.paneId === displayedLayout.id).sort((a, b) => a.id.localeCompare(b.id)).map(panel => {
          const rect = geometry.get(panel.paneId);
          return <section key={panel.id} id={workspacePanelId(panel.id)} role="tabpanel" tabIndex={0}
            aria-label={labelledPanels.has(panel.id) ? undefined : panel.label} aria-labelledby={labelledPanels.has(panel.id) ? panel.labelledBy : undefined}
            data-workspace-view={panel.id} data-workspace-pane={panel.paneId}
            onPointerDownCapture={event => focusFromPointer(panel.paneId, event)} onFocusCapture={() => onFocusPane(panel.paneId)}
            {...stylex.props(styles.content, !rect && styles.unmeasured)} style={rect}>{panel.content}</section>;
        })}
        {preview && <div aria-hidden="true" data-workspace-drop={preview.drop.edge ?? 'tab'} {...stylex.props(styles.preview)} style={preview.rect}/>}
      </div>
      {drag.preview}
    </workspaceDragContext.Provider>
  </DragDropProvider>;
}
