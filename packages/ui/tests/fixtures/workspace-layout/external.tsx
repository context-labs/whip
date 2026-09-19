import * as stylex from '@stylexjs/stylex';
import { useRef, useState } from 'react';
import { Button } from '@whip/ui';
import { WorkspaceLayout, workspacePanelId } from '../../../src/workspace-layout';
import type { WorkspaceDrop, WorkspaceLayoutNode } from '../../../src/workspace-layout';
import { WorkspaceDragScope, WorkspaceExternalSource, WorkspaceTabs, workspaceTabId } from '../../../src/workspace-tabs';
import { colors } from '../../../src/tokens.stylex';

const styles = stylex.create({
  fixture: { display: 'flex', flexDirection: 'column', height: '100vh', color: colors.foreground, backgroundColor: colors.background },
  zoom: { zoom: 1.25 },
  toolbar: { display: 'flex', gap: 8, padding: 8, flexWrap: 'wrap' },
  body: { display: 'flex', flex: 1, minHeight: 0 },
  sidebar: { display: 'flex', flexDirection: 'column', width: 240, flexShrink: 0, padding: 12, gap: 12 },
  source: { display: 'block', padding: 12, color: colors.foreground, backgroundColor: colors.element },
  view: { display: 'flex', flexDirection: 'column', height: '100%', padding: 12 },
  scroll: { overflow: 'auto', flex: 1, minHeight: 0 },
});
const layout: WorkspaceLayoutNode = { type: 'split', id: 'split', direction: 'horizontal', ratio: .5,
  first: { type: 'pane', id: 'one' }, second: { type: 'pane', id: 'two' } };
const payload = { session: 'alpha', runtime: 'fixture' };
function View({ id }: { id: string }) {
  const [draft, setDraft] = useState(`Draft ${id}`);
  return <div {...stylex.props(styles.view)}><div data-view-scroll={id} {...stylex.props(styles.scroll)}>
    {Array.from({ length: 70 }, (_, index) => <p key={index}>{id}: message {index}</p>)}
  </div><textarea aria-label={`Draft ${id}`} value={draft} onChange={event => setDraft(event.target.value)}/></div>;
}

export function ExternalFixture({ params }: { params: URLSearchParams }) {
  const [panes, setPanes] = useState<Record<string, string[]>>({ one: ['alpha', 'beta'], two: params.has('empty') ? [] : params.has('overflow') ? Array.from({ length: 20 }, (_, index) => `other-${index}`) : ['gamma', 'delta'] });
  const [selected, setSelected] = useState<Record<string, string>>({ one: 'alpha', two: panes.two![0] ?? '' });
  const [tree, setTree] = useState(layout);
  const [focused, setFocused] = useState('one');
  const [source, setSource] = useState(true);
  const [allowed, setAllowed] = useState(true);
  const [clicks, setClicks] = useState('');
  const [drops, setDrops] = useState<WorkspaceDrop[]>([]);
  const counter = useRef(0);
  function open(data: unknown, drop: WorkspaceDrop) {
    if (data !== payload || !allowed) throw new Error('Invalid external drop');
    const id = `external-view-${++counter.current}`;
    const paneId = drop.edge ? `external-pane-${counter.current}` : drop.paneId;
    if (drop.edge) {
      const edge = drop.edge;
      const newPane: WorkspaceLayoutNode = { type: 'pane', id: paneId };
      const split = (node: WorkspaceLayoutNode): WorkspaceLayoutNode => {
        if (node.type === 'split') return { ...node, first: split(node.first), second: split(node.second) };
        if (node.id !== drop.paneId) return node;
        const before = edge === 'left' || edge === 'top';
        return { type: 'split', id: `external-split-${id}`, direction: edge === 'left' || edge === 'right' ? 'horizontal' : 'vertical', ratio: .5, first: before ? newPane : node, second: before ? node : newPane };
      };
      setTree(split);
    }
    setPanes(current => {
      const values = [...(current[paneId] ?? [])];
      values.splice(drop.index ?? values.length, 0, id);
      return { ...current, [paneId]: values };
    });
    setSelected(current => ({ ...current, [paneId]: id }));
    setFocused(paneId);
    setDrops(current => [...current, drop]);
    return id;
  }
  return <main dir={params.has('rtl') ? 'rtl' : 'ltr'} {...stylex.props(styles.fixture, params.has('zoom') && styles.zoom)}>
    <div {...stylex.props(styles.toolbar)}>
      <Button onClick={() => setSource(false)}>Remove source</Button>
      <Button disabled={!allowed} onClick={() => setAllowed(false)}>Reject external drops</Button>
      <output aria-label="External drops">{JSON.stringify(drops)}</output>
      <output aria-label="Source clicks">{clicks}</output>
    </div>
    <WorkspaceDragScope><div {...stylex.props(styles.body)}>
      <aside aria-label="Saved sessions" {...stylex.props(styles.sidebar)}>
        {source && <WorkspaceExternalSource id="external:alpha" data={payload} label="Saved alpha" status={<span aria-hidden="true">◉</span>}>
          {props => <a {...props} href="#saved-alpha" {...stylex.props(styles.source)} onClick={event => {
            setClicks(event.ctrlKey ? 'ctrl' : event.metaKey ? 'meta' : event.shiftKey ? 'shift' : event.altKey ? 'alt' : 'plain');
          }}>Saved alpha</a>}
        </WorkspaceExternalSource>}
        <Button onClick={() => setClicks('action')}>Session action</Button>
        <WorkspaceExternalSource id="external:disabled" data={payload} label="Disabled source" disabled>
          {props => <a {...props} href="#disabled" {...stylex.props(styles.source)}>Disabled source</a>}
        </WorkspaceExternalSource>
      </aside>
      {params.has('standalone') ? <div {...stylex.props(styles.view)}><WorkspaceTabs groupId="two" label="Standalone tabs"
        items={panes.two!.map(value => ({ value, label: value }))} value={selected.two || null}
        onValueChange={value => setSelected(current => ({ ...current, two: value }))} onClose={() => {}}
        onExternalDrop={open} canDropExternal={data => allowed && data === payload}
        onReorder={values => setPanes(current => ({ ...current, two: values }))}/></div>
      : <WorkspaceLayout layout={tree} compact={params.has('compact')} focusedPaneId={focused} onFocusPane={setFocused} onResize={() => {}}
        onDrop={() => { throw new Error('External source reached internal drop callback'); }}
        canDropExternal={(data, drop) => allowed && data === payload && (!drop.edge || Object.keys(panes).length < 4)} onExternalDrop={open}
        renderHeader={paneId => <WorkspaceTabs groupId={paneId} label={`Tabs in ${paneId}`}
          items={panes[paneId]!.map(value => ({ value, label: value }))} value={selected[paneId] || null}
          onValueChange={value => setSelected(current => ({ ...current, [paneId]: value }))} onClose={() => {}}
          utilities={<Button aria-label={`Utility ${paneId}`}>Utility</Button>} panelId={workspacePanelId(selected[paneId] ?? '')}/>}
        panels={Object.entries(selected).filter(([, id]) => id).map(([paneId, id]) => ({ id, paneId, label: id, labelledBy: workspaceTabId(id), content: <View id={id}/> }))}/> }
    </div></WorkspaceDragScope>
  </main>;
}
