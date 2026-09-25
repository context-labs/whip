import type { MDXComponents } from 'mdx/types';
import { Callout, CodeBlock, CodeTab, CodeTabs, Table } from '../../components/ui';

import { sitePath } from './content/site-path';

export const docsComponents: MDXComponents = {
  a: ({ href, ...props }) => <a {...props} href={href && sitePath(href)} />,
  pre: CodeBlock,
  code: ({ className = '', ...props }) => <code className={className} {...props} />,
  table: Table,
  th: props => <th scope="col" {...props} />,
  Callout,
  CodeTabs,
  CodeTab,
};
