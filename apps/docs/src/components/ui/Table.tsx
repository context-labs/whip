import type { TableHTMLAttributes } from 'react';
export function Table({ children, ...props }: TableHTMLAttributes<HTMLTableElement>) {
  return <div className="table-scroll" role="region" aria-label={props['aria-label'] || 'Table'} tabIndex={0}><table {...props}>{children}</table></div>;
}
