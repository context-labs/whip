import * as stylex from '@stylexjs/stylex';
import { useState } from 'react';
import { Button, ContextMenu, IconButton, Menu } from '@whip/ui';
import { WorkspaceLayout, workspacePanelId } from '@whip/ui/workspace-layout';
import type { WorkspaceLayoutNode, WorkspaceDrop } from '@whip/ui/workspace-layout';
import { WorkspaceTabs, workspaceTabId } from '@whip/ui/workspace-tabs';
import { colors } from '../src/tokens.stylex';

const styles = stylex.create({
  fixture: { display: 'flex', flexDirection: 'column', height: '100vh', color: colors.foreground, backgroundColor: colors.background },
  toolbar: { display: 'flex', flexWrap: 'wrap', gap: 8, padding: 8 },
  view: { display: 'flex', flexDirection: 'column', height: '100%', minHeight: 0, padding: 12, gap: 12 },
  scroll: { flex: '1 1 auto', minHeight: 0, overflow: 'auto' },
  draft: { minHeight: 60, color: colors.foreground, backgroundColor: colors.element },
});
const initial: WorkspaceLayoutNode = { type: 'split', id: 'outer', direction: 'horizontal', ratio: .58,
  first: { type: 'split', id: 'inner', direction: 'vertical', ratio: .5, first: { type: 'pane', id: 'one' }, second: { type: 'pane', id: 'two' } },
  second: { type: 'pane', id: 'three' } };
function resize(node: WorkspaceLayoutNode, id: string, ratio: number): WorkspaceLayoutNode {
  return node.type === 'pane' ? node : node.id === id ? { ...node, ratio } : { ...node, first: resize(node.first, id, ratio), second: resize(node.second, id, ratio) };
}
function split(node: WorkspaceLayoutNode, paneId: string, edge: NonNullable<WorkspaceDrop['edge']>, id: string): WorkspaceLayoutNode {
  if (node.type === 'split') return { ...node, first: split(node.first, paneId, edge, id), second: split(node.second, paneId, edge, id) };
  if (node.id !== paneId) return node;
  const newPane = { type: 'pane' as const, id }, before = edge === 'left' || edge === 'top';
  return { type: 'split', id: `split-${id}`, direction: edge === 'left' || edge === 'right' ? 'horizontal' : 'vertical', ratio: .5, first: before ? newPane : node, second: before ? node : newPane };
}
function prune(node: WorkspaceLayoutNode, panes: Record<string, string[]>): WorkspaceLayoutNode | undefined {
  if (node.type === 'pane') return panes[node.id]?.length ? node : undefined;
  const first = prune(node.first, panes), second = prune(node.second, panes);
  return first && second ? { ...node, first, second } : first ?? second;
}
function NotesView() {
  const [notes, setNotes] = useState('Notes stay editable independently of conversations.');
  return <div {...stylex.props(styles.view)}><h2>Workspace notes</h2><textarea aria-label="Workspace notes" value={notes} onChange={event => setNotes(event.target.value)} {...stylex.props(styles.draft)}/><output>{notes.length} characters</output></div>;
}
function ExampleView({ id }: { id: string }) {
  const [draft, setDraft] = useState(`Draft ${id}`);
  return <div {...stylex.props(styles.view)}><div data-view-scroll={id} {...stylex.props(styles.scroll)}>{Array.from({ length: 70 }, (_, index) => <p key={index}>{id}: retained message {index + 1}</p>)}</div><textarea aria-label={`Draft ${id}`} value={draft} onChange={event => setDraft(event.target.value)} {...stylex.props(styles.draft)}/></div>;
}

export function WorkspaceLayoutFixture() {
  const [layout, setLayout] = useState(initial);
  const [panes, setPanes] = useState<Record<string, string[]>>({ one: ['alpha', 'beta'], two: ['delta'], three: ['gamma'] });
  const [selected, setSelected] = useState<Record<string, string>>({ one: 'alpha', two: 'delta', three: 'gamma' });
  const [focused, setFocused] = useState('one');
  const [compact, setCompact] = useState(false);
  const [lastDrop, setLastDrop] = useState('');
  const [lastResize, setLastResize] = useState('');
  function move(drop: WorkspaceDrop) {
    const sourceId = Object.keys(panes).find(id => panes[id]!.includes(drop.viewId));
    if (!sourceId) return;
    const sourceIndex = panes[sourceId]!.indexOf(drop.viewId);
    const next = Object.fromEntries(Object.entries(panes).map(([id, values]) => [id, values.filter(value => value !== drop.viewId)]));
    const destination = drop.edge ? `extra-${Object.keys(panes).length}` : drop.paneId;
    if (drop.edge) next[destination] = [];
    const index = drop.index === undefined ? next[destination]!.length : drop.index - (sourceId === destination && sourceIndex < drop.index ? 1 : 0);
    next[destination]!.splice(index, 0, drop.viewId);
    setLayout(current => prune(drop.edge ? split(current, drop.paneId, drop.edge, destination) : current, next)!);
    setPanes(next);
    setSelected(current => ({ ...current, [sourceId]: current[sourceId] === drop.viewId ? next[sourceId]![0] ?? '' : current[sourceId]!, [destination]: drop.viewId }));
    setFocused(destination); setLastDrop(JSON.stringify(drop));
  }
  return <main {...stylex.props(styles.fixture)}>
    <div {...stylex.props(styles.toolbar)}><Button onClick={() => move({ viewId: 'alpha', paneId: 'three' })}>Move alpha right</Button><Button onClick={() => setFocused('two')}>Focus lower pane</Button><output aria-label="Compact layout">{String(compact)}</output><output aria-label="Last drop">{lastDrop}</output><output aria-label="Last resize">{lastResize}</output></div>
    <WorkspaceLayout layout={layout} focusedPaneId={focused} onFocusPane={setFocused} onCompactChange={setCompact} onDrop={move}
      onResize={(id, ratio) => { setLayout(current => resize(current, id, ratio)); setLastResize(`${id}:${ratio}`); }}
      renderHeader={id => <WorkspaceTabs groupId={id} label={`Tabs in ${id}`} items={(panes[id] ?? []).map(value => ({ value, label: value, tooltip: `Details for ${value}`, render: <a href={`?view=${value}`} onClick={event => { if (!event.ctrlKey && !event.metaKey && !event.shiftKey && !event.altKey) { event.preventDefault(); setSelected(current => ({ ...current, [id]: value })); setFocused(id); } }}/>, menu: <Menu trigger={<IconButton label={`Tab actions ${value}`}>⋯</IconButton>} items={[{ id: 'move', label: 'Move to right pane', disabled: id === 'three', onSelect: () => move({ viewId: value, paneId: 'three' }) }]}/>, wrap: element => <ContextMenu items={[{ id: 'move', label: 'Move to right pane', disabled: id === 'three', onSelect: () => move({ viewId: value, paneId: 'three' }) }]}>{element}</ContextMenu> }))} value={selected[id] ?? null}
        onValueChange={value => { setSelected(current => ({ ...current, [id]: value })); setFocused(id); }} onClose={() => {}} panelId={workspacePanelId(selected[id] ?? '')}/>}
      panels={Object.entries(selected).filter(([, id]) => id).map(([paneId, id]) => ({ id, paneId, label: id, labelledBy: workspaceTabId(id), content: id === 'gamma' ? <NotesView/> : <ExampleView id={id}/> }))}/>
  </main>;
}
