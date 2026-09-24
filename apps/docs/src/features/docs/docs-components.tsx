import type { MDXComponents } from 'mdx/types';
import { Callout, CodeBlock, CodeTab, CodeTabs, Table } from '../../components/ui';

export const docsComponents: MDXComponents = {
  pre: CodeBlock,
  code: ({ className = '', ...props }) => <code className={className} {...props} />,
  table: Table,
  th: props => <th scope="col" {...props} />,
  Callout,
  CodeTabs,
  CodeTab,
};
