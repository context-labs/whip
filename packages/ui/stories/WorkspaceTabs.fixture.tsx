import { useState } from 'react';
import * as stylex from '@stylexjs/stylex';
import { DirectionProvider } from '@base-ui/react/direction-provider';
import { Circle, CircleDot, MessageCircle, Plus, MoreHorizontal } from 'lucide-react';
import { Button, ContextMenu, IconButton, Menu, Row, Stack } from '../src';
import { WorkspaceTabs, workspaceTabId } from '@whip/ui/workspace-tabs';
import type { WorkspaceTabItem } from '@whip/ui/workspace-tabs';
import { colors } from '../src/tokens.stylex';

const styles = stylex.create({ canvas: { color: colors.foreground, backgroundColor: colors.background } });

const examples = [
  { value: 'alpha', label: 'Inspect event delivery', status: 'Running', project: 'whip' },
  { value: 'beta', label: 'Investigate storage ownership', status: 'Needs input', project: 'whip' },
  { value: 'gamma', label: 'A long translated session name that remains readable and accessible', status: 'Idle', project: 'docs' },
  { value: 'delta', label: 'Retained draft', status: 'Stale', project: 'sdk' },
];

export function WorkspaceTabsFixture({ many = false, links = true, rtl = false }: { many?: boolean; links?: boolean; rtl?: boolean }) {
  const initial = many ? Array.from({ length: 32 }, (_, i) => ({ ...examples[i % examples.length]!, value: `session-${i}`, label: `Session ${i + 1}: ${examples[i % examples.length]!.label}` })) : examples;
  const [items, setItems] = useState(initial);
  const [active, setActive] = useState<string | null>(initial[0]!.value);
  const [navigations, setNavigations] = useState(0);
  const [status, setStatus] = useState(false);
  const [closed, setClosed] = useState('');
  const [order, setOrder] = useState('');
  const navigate = (value: string) => { setActive(value); setNavigations(current => current + 1); };
  const close = (value: string) => {
    const index = items.findIndex(item => item.value === value);
    if (active === value) setActive(items[index + 1]?.value ?? items[index - 1]?.value ?? null);
    setItems(current => current.filter(item => item.value !== value)); setClosed(value);
  };
  const reorder = (values: string[]) => { setItems(values.map(value => items.find(item => item.value === value)!)); setOrder(values.join(',')); };
  const move = (value: string, delta: number) => {
    const values = items.map(item => item.value); const index = values.indexOf(value); const next = index + delta;
    if (next < 0 || next >= values.length) return;
    [values[index], values[next]] = [values[next]!, values[index]!]; reorder(values);
  };
  const tabs: WorkspaceTabItem[] = items.map((item, index) => {
    const state = status ? 'Stale' : item.status;
    const actions = [
      { id: 'left', label: 'Move left', disabled: index === 0, onSelect: () => move(item.value, -1) },
      { id: 'right', label: 'Move right', disabled: index === items.length - 1, onSelect: () => move(item.value, 1) },
      { id: 'close', label: 'Close session', onSelect: () => close(item.value) },
    ];
    return {
      value: item.value, label: item.label, accessibleLabel: `${item.label}, ${state}, ${item.project}`,
      tooltip: `${item.label} · ${item.project} · Local host · ${state}`,
      status: state === 'Running' ? <CircleDot size={12}/> : state === 'Needs input' ? <MessageCircle size={12}/> : <Circle size={10}/>,
      metadata: item.project,
      menu: <Menu trigger={<IconButton label={`Actions for ${item.value}`} variant="ghost" size="sm" tabIndex={item.value === active ? 0 : -1}><MoreHorizontal size={12}/></IconButton>} items={actions}/>,
      wrap: tab => <ContextMenu items={actions}>{tab}</ContextMenu>,
      render: links ? <a href={`?session=${item.value}`} onClick={event => { if (event.button === 0 && !event.metaKey && !event.ctrlKey && !event.shiftKey && !event.altKey && !event.defaultPrevented) { event.preventDefault(); navigate(item.value); } }}/> : undefined,
    };
  });
  return <DirectionProvider direction={rtl ? 'rtl' : 'ltr'}><div dir={rtl ? 'rtl' : 'ltr'} {...stylex.props(styles.canvas)}>
    <Stack>
      <h1>Workspace tabs</h1>
      <WorkspaceTabs items={tabs} value={active} onValueChange={navigate} onClose={close} onReorder={reorder} panelId="workspace-panel" utilities={<><IconButton label="New session" variant="ghost"><Plus size={16}/></IconButton><Button size="sm">All open tabs</Button></>}/>
      <section role={active ? 'tabpanel' : undefined} id="workspace-panel" aria-labelledby={active ? workspaceTabId(active) : undefined} tabIndex={0}><p>{active ? items.find(item => item.value === active)?.label : 'Home — no selected session'}</p><textarea aria-label="Conversation draft" defaultValue="A draft stays mounted while tabs are moved."/></section>
      <Row><Button onClick={() => setActive(null)}>Home</Button><Button onClick={() => navigate(items.at(-1)!.value)}>Select last</Button><Button onClick={() => setStatus(current => !current)}>Change status</Button></Row>
      <output aria-label="Active session">{active ?? 'none'}</output><output aria-label="Navigation count">{navigations}</output><output aria-label="Closed session">{closed}</output><output aria-label="Reordered sessions">{order}</output>
    </Stack>
  </div></DirectionProvider>;
}
