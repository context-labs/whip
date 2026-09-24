import type { Meta, StoryObj } from '@storybook/react-vite';
import { SiteHeader } from '../navigation/SiteHeader';
import { DocsLayout } from '../../features/docs/components/DocsLayout';
import { DocPageActions } from '../../features/docs/components/DocPageActions';
import { docsComponents } from '../../features/docs/docs-components';
import { docsManifest } from '../../features/docs/content/manifest.gen';
import Content from '../../content/docs/getting-started/index.mdx';
import source from '../../content/docs/getting-started/index.mdx?raw';

function GettingStartedPage() {
  const current = docsManifest.find(entry => entry.path === 'getting-started')!;
  return <><SiteHeader /><DocsLayout entries={docsManifest} current={current} className="getting-started-page"
    tocHeadings={current.headings.filter(heading => heading.level === 2)}
    nextPage={docsManifest.find(entry => entry.path === 'using-whipcode/cli')}
    actions={<DocPageActions source={source} filename="getting-started.mdx" />}>
    <Content components={docsComponents} />
  </DocsLayout></>;
}
const meta = { title: 'Docs/Getting started', component: GettingStartedPage, parameters: { layout: 'fullscreen' } } satisfies Meta<typeof GettingStartedPage>;
export default meta;
type Story = StoryObj<typeof meta>;
export const Dark: Story = { globals: { theme: 'dark' } };
export const Light: Story = { globals: { theme: 'light' } };
