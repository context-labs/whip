import type { Meta, StoryObj } from '@storybook/react-vite';
import { SiteHeader } from '../navigation/SiteHeader';
import { DocsLayout } from '../../features/docs/components/DocsLayout';
import { DocPageActions } from '../../features/docs/components/DocPageActions';
import { docsComponents } from '../../features/docs/docs-components';
import { docsManifest } from '../../features/docs/content/manifest.gen';
import Content from './ArticleFixture.mdx';
import source from './ArticleFixture.mdx?raw';

function GettingStartedPage() {
  // Preserve the accepted full-article design as a library specimen, independent of outline copy.
  const current = { ...docsManifest[0], title: 'Getting started', description: 'Install whipcode, connect a model, and run your first coding session.', headings: [
    { id: 'start-your-first-session', text: 'Start your first session', level: 2 as const },
    { id: 'continue-from-the-terminal', text: 'Continue from the terminal', level: 2 as const },
    { id: 'understand-workspace-scope', text: 'Understand workspace scope', level: 2 as const },
    { id: 'troubleshooting', text: 'Troubleshooting', level: 2 as const },
  ] };
  return <><SiteHeader /><DocsLayout entries={docsManifest} current={current} className="getting-started-page"
    tocHeadings={current.headings.filter(heading => heading.level === 2)}
    nextPage={docsManifest.find(entry => entry.path === 'tui')}
    actions={<DocPageActions source={source} filename="getting-started.mdx" />}>
    <Content components={docsComponents} />
  </DocsLayout></>;
}
const meta = { title: 'Docs/Getting started', component: GettingStartedPage, parameters: { layout: 'fullscreen' } } satisfies Meta<typeof GettingStartedPage>;
export default meta;
type Story = StoryObj<typeof meta>;
export const Dark: Story = { globals: { theme: 'dark' } };
export const Light: Story = { globals: { theme: 'light' } };
