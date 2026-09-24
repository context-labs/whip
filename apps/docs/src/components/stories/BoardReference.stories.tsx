import { useState, type CSSProperties, type ReactNode } from 'react';
import type { Meta, StoryObj } from '@storybook/react-vite';
import { Button, IconButton, SplitButton, CopyButton, Callout, Table, Icon } from '../ui';
import { TopNavLink, SidebarItem, CommunityLink, PagerCard } from '../navigation';
import { docsComponents } from '../../features/docs/docs-components';
import CodeFixture from './CodeFixture.mdx';
import SingleCodeFixture from './SingleCodeFixture.mdx';

const groups = {
  Surfaces: ['bg', 'surface-panel', 'surface-element', 'surface-control', 'surface-hover'],
  Lines: ['border', 'border-control', 'divider'],
  Text: ['text', 'text-secondary', 'text-muted'],
  'Accent & semantic': ['accent', 'link', 'code', 'success', 'warning', 'error', 'emphasis'],
};
function Section({ number, title, description, children }: { number: string; title: string; description: string; children: ReactNode }) {
  return <section className="board-section"><header className="board-section-heading"><span className="label">{number}</span><div><h2>{title}</h2><p>{description}</p></div></header><div className="board-section-body">{children}</div></section>;
}
function Row({ title, description, children }: { title: string; description: string; children: ReactNode }) {
  return <div className="board-row"><div><h3>{title}</h3><p className="board-note">{description}</p></div><div className="board-examples">{children}</div></div>;
}
function State({ label, state, children }: { label: string; state?: string; children: ReactNode }) {
  return <div className="board-state" data-preview={state}><div className="label">{label}</div>{children}</div>;
}
function BoardReference() {
  const [action, setAction] = useState('Controls are live. Try a button, copy action, or tab.');
  return <main className="board-reference" id="board-top">
    <header className="board-header"><div><div className="label">whipcode · docs system</div><h1>Design tokens</h1><p className="page-lead">The canonical reference for whipcode docs. Carbonfox neutrals, system type, and purposeful colour.</p></div><dl className="board-facts"><div><dt className="label">Theme</dt><dd>Carbonfox</dd></div><div><dt className="label">Grid</dt><dd>4px</dd></div><div><dt className="label">Corners</dt><dd>6px</dd></div><div><dt className="label">Depth</dt><dd>Borders only</dd></div></dl></header>
    <Section number="01" title="Colour" description="Roles, not decoration. One token system for dark and light.">
      {Object.entries(groups).map(([title, names]) => <div key={title}><div className="label board-swatches-label">{title}</div><div className="board-swatches">{names.map(name => <div className="board-swatch" key={name}><div style={{ background: `var(--color-${name})` }} /><code>--color-{name}</code></div>)}</div></div>)}
    </Section>
    <Section number="02" title="Typography" description="System sans for reading. System mono only for code.">
      {[
        ['H1 · Page title', '32 / 40 · 500', <h1 key="h1">Getting started</h1>],
        ['H2 · Section', '22 / 28 · 500', <h2 key="h2">Your first session</h2>],
        ['H3 · Step', '16 / 24 · 600', <h3 key="h3">1. Open a project</h3>],
        ['Lead', '15 / 24 · 400', <p className="page-lead" key="lead">Work with agents from your terminal or desktop.</p>],
        ['Body', '13 / 22 · 400', <p key="body">Choose the interface that fits your workflow. Your files stay in your project.</p>],
        ['Callout title', '13 / 22 · 600', <strong key="title">Before you start</strong>],
        ['Small', '12 / 20 · 400', <p className="board-small" key="small">A quiet, readable detail.</p>],
        ['Label', '11 / 16 · 500 · 0.08em', <span className="label" key="label">On this page</span>],
        ['Code', '12 / 20 · mono', <span className="board-code" key="code">whipcode --help</span>],
      ].map(([title, description, sample], index) => <Row key={index} title={String(title)} description={String(description)}>{sample}</Row>)}
    </Section>
    <Section number="03" title="Spacing & radius" description="A 4px grid. One corner for containers; a smaller chip for inline code.">
      <div className="board-spacing">{[4, 8, 12, 16, 20, 24, 32, 64].map(size => <div key={size}><div style={{ width: size, height: 32, background: 'var(--color-text-secondary)' }} /><code>{size}px</code></div>)}</div>
      <div className="board-examples">{[0, 4, 6].map(radius => <div className="board-radius" key={radius} style={{ borderRadius: radius }}><code>{radius}px</code></div>)}</div>
    </Section>
    <Section number="04" title="Components" description="The same anatomy in every state. Neutral chrome; no decorative colour.">
      <Row title="Primary button" description="Control surface, full-strength label, 6px corners. 34px total height.">
        {['Default', 'Hover', 'Focus', 'Disabled'].map(label => <State key={label} label={label} state={label.toLowerCase()}><Button disabled={label === 'Disabled'} onClick={() => setAction(`${label} button activated`)}>Run example</Button></State>)}
      </Row>
      <Row title="Icon button" description="Copy actions are 28px square. Split actions share an outer boundary.">
        <State label="Copy · default"><CopyButton text="whipcode --help\n" /></State><State label="Copy · hover" state="hover"><CopyButton text="whipcode --help\n" /></State>
        <State label="Split button"><SplitButton label="Run example" onClick={() => setAction('Primary action ran')} actions={[{ label: 'Run alternate example', onClick: () => setAction('Alternate action ran') }, { label: 'Unavailable action', onClick: () => {}, disabled: true }]} /></State>
        <State label="Keyboard focus" state="focus"><IconButton label="Show example result" onClick={() => setAction('Icon action ran')}><Icon name="check" /></IconButton></State>
      </Row>
      <Row title="Sidebar nav" description="13 / 22. Active keeps the same weight, with a neutral surface and brighter text.">
        {['Default', 'Hover', 'Active'].map(label => <State key={label} label={label} state={label.toLowerCase()}><div style={{ width: 200 }}><div className="label sidebar-label">Get started</div><SidebarItem href="#board-top" active={label === 'Active'}>Getting started</SidebarItem></div></State>)}
      </Row>
      <Row title="Top nav link" description="12 / 16. Active is weight and colour only.">{['Default', 'Hover', 'Active'].map(label => <State key={label} label={label} state={label.toLowerCase()}><TopNavLink href="#board-top" active={label === 'Active'}>Documentation</TopNavLink></State>)}</Row>
      <Row title="Table of contents" description="11 / 18 on a 1px rail. Active overlaps it with a 2px neutral marker."><State label="Default"><div className="toc"><ul><li><a className="toc-link" href="#board-top">Overview</a></li></ul></div></State><State label="Active"><div className="toc"><ul><li><a className="toc-link" href="#board-top" aria-current="location">Getting started</a></li></ul></div></State></Row>
      <Row title="Community links" description="Official monochrome mark, 16px within a 32px target. Only verified destinations are linked."><State label="Default"><CommunityLink /></State><State label="Hover · GitHub" state="hover"><CommunityLink /></State></Row>
      <Row title="Callout" description="Panel, 6px corners, 16 / 20 padding. The border alone carries the status."><div className="board-callouts">{(['info', 'tip', 'warning', 'alert'] as const).map((type, index) => <State key={type} label={type}><Callout type={type} title={['Before you start', 'Choose your interface', 'Review permissions', 'Destructive actions'][index]}><p>{['Open a project you want to work on.', 'Use Desktop or the CLI for your session.', 'Check what an action can access before approving it.', 'Confirm the effect before changing or deleting files.'][index]}</p></Callout></State>)}</div></Row>
      <Row title="Code block" description="40px header, 20px body padding. Multicolour syntax is the approved update to this board."><div className="board-code-pair"><State label="Single"><SingleCodeFixture components={docsComponents} /></State><State label="Tabbed"><CodeFixture components={docsComponents} /></State></div></Row>
      <Row title="Inline code" description="Green mono at 0.85em, subtle element surface, 4px corners. No border or vertical padding."><State label="Identifier"><code>WHIP_HOME</code></State><State label="File"><code>config.json</code></State><State label="In prose"><p>Run <code>whipcode --help</code> in your terminal.</p></State></Row>
      <Row title="Table" description="One rounded outer boundary. Raised header, single row and column separators."><div style={{ width: 560, maxWidth: '100%' }}><Table aria-label="Example configuration"><thead><tr><th>Setting</th><th>Description</th></tr></thead><tbody><tr><td><code>model</code></td><td>Model used for a session</td></tr><tr><td><code>provider</code></td><td>Configured model provider</td></tr></tbody></Table></div></Row>
      <Row title="Next / previous card" description="Panel surface, 16px padding, 76px minimum height."><State label="Previous"><PagerCard href="#board-top" title="Getting started" direction="previous" /></State><State label="Next · hover" state="hover"><PagerCard href="#board-top" title="Installation" direction="next" /></State></Row>
      <p className="board-action-result" role="status">{action}</p>
    </Section>
    <Section number="05" title="Syntax roles" description="Build-time tokenization. Carbonfox colours exceed 4.5:1 on their code surface in both modes."><div className="board-swatches">{['keyword', 'string', 'number', 'comment', 'function', 'type', 'operator', 'punctuation'].map(role => <div className="board-swatch" key={role}><div style={{ background: `var(--color-syntax-${role})` } as CSSProperties} /><code>{role}</code></div>)}</div></Section>
  </main>;
}
const meta = { title: 'Docs/Board Reference', component: BoardReference, parameters: { layout: 'fullscreen' } } satisfies Meta<typeof BoardReference>;
export default meta;
type Story = StoryObj<typeof meta>;
export const Dark: Story = { globals: { theme: 'dark' } };
export const Light: Story = { globals: { theme: 'light' } };
