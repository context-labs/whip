import { useState } from 'react';
import type { Meta, StoryObj } from '@storybook/react-vite';
import { Button, Callout, CodeBlock, CopyButton, SplitButton, Table } from '../ui';
import { PagerCard } from '../navigation';
import { ThemeMenu } from '../ui/theme/ThemeMenu';
import { DocsSidebar, TableOfContents } from '../../features/docs/components';
import { docsComponents } from '../../features/docs/docs-components';
import CodeFixture from './CodeFixture.mdx';
import InstallationFixture from './InstallationFixture.mdx';
import ConfigurationFixture from './ConfigurationFixture.mdx';
import LanguagesFixture from './LanguagesFixture.mdx';
import type { DocMeta } from '../../features/docs/content/types';

const meta = { title: 'Docs/Components', decorators: [Story => <div className="component-story"><Story /></div>] } satisfies Meta;
export default meta;
type Story = StoryObj<typeof meta>;
export const LinksAndInlineCode: Story = { render: () => <>
  <p>Follow the <a href="https://github.com/context-labs/whip">project on GitHub</a> or read <a href="/docs/configuration">configuration</a>.</p>
  <p>Set <code>WHIP_HOME</code> to choose a configuration directory. Run <code>whipcode --help</code> to see the available commands.</p>
  <p>A long identifier wraps without changing the surrounding line spacing: <code>WHIP_DESKTOP_CERTIFICATE_P12_BASE64_EXAMPLE_IDENTIFIER</code>.</p>
  <Callout title="Before you begin"><p>Keep <code>config.json</code> local and review <a href="/docs/tools-and-permissions">tool permissions</a>.</p></Callout>
  <Table><thead><tr><th>Setting</th><th>Reference</th></tr></thead><tbody><tr><td><code>permission_mode</code></td><td><a href="/docs/tools-and-permissions">Approval modes</a></td></tr></tbody></Table>
  <p>Linked code keeps its green treatment: <a href="/docs/using-whipcode/cli"><code>whipcode run</code></a>.</p>
</> };
export const Callouts: Story = { render: () => <>{(['info', 'tip', 'warning', 'alert'] as const).map(type => <Callout key={type} title={`${type[0].toUpperCase()}${type.slice(1)} example`} type={type}><p>Full-strength prose, a quiet panel, and a meaningful border.</p><p>Longer content wraps naturally without changing the title style.</p></Callout>)}</> };
export const CodeTabs: Story = { render: () => <CodeFixture components={docsComponents} /> };
export const SyntaxLanguages: Story = { render: () => <div className="prose"><LanguagesFixture components={docsComponents} /></div> };
export const CodeOverflow: Story = { render: () => <CodeBlock data-language="text" data-raw={'A long line ' + 'keeps its whitespace. '.repeat(20) + '\n'}><code>{'A long line ' + 'keeps its whitespace. '.repeat(20) + '\n'}</code></CodeBlock> };
export const CopyFeedback: Story = { render: () => <><p>Allow clipboard access to test success. Deny it to test the actionable failure message.</p><CopyButton text={'  whitespace\t世界 <script>&\n'} /></> };
export const ButtonStates: Story = { render: function Buttons() {
  const [result, setResult] = useState('No action yet');
  return <><Button onClick={() => setResult('Primary action ran')}>Run example</Button><Button disabled>Unavailable</Button><SplitButton label="Run example" onClick={() => setResult('Primary action ran')} actions={[{ label: 'Run alternate', onClick: () => setResult('Alternate action ran') }]} /><p role="status">{result}</p></>;
} };
const entries: DocMeta[] = [
  { path: 'getting-started', title: 'Getting started', description: '', section: 'start', order: 1, headings: [] },
  { path: 'installation', title: 'Installation', description: '', section: 'start', order: 2, headings: [] },
  { path: 'configuration', title: 'Configuration with a deliberately long label to test wrapping', description: '', section: 'configuration', order: 1, headings: [] },
];
export const Contents: Story = { render: () => <><TableOfContents headings={[{ id: 'overview', text: 'Overview', level: 2 }, { id: 'long-label', text: 'A longer nested heading wraps without colliding with the rail', level: 3 }]} /><h2 id="overview">Overview</h2><h3 id="long-label">A longer nested heading</h3></> };
export const ThemeSelection: Story = { render: () => <><p>Choose a persistent colour theme or follow your system.</p><ThemeMenu /></> };
export const TableOverflow: Story = { render: () => <Table aria-label="Overflow example"><thead><tr><th>Setting</th><th>Description</th><th>Example value</th></tr></thead><tbody><tr><td><code>name</code></td><td>A long explanatory value that can wrap.</td><td><code>{'example-'.repeat(20)}</code></td></tr></tbody></Table> };
export const Pagination: Story = { render: () => <nav className="doc-pagination" aria-label="Adjacent example pages"><PagerCard href="/docs/getting-started" title="A previous page with a long title" direction="previous" /><PagerCard href="/docs/installation" title="The next page with a long title" direction="next" /></nav> };

export const InstallationExample: Story = { render: () => <article className="prose"><InstallationFixture components={docsComponents} /></article> };
export const ConfigurationExample: Story = { render: () => <article className="prose"><ConfigurationFixture components={docsComponents} /></article> };
