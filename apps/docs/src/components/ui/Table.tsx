import { Children, cloneElement, isValidElement, type ReactElement, type ReactNode, type TableHTMLAttributes } from 'react';
import * as stylex from '@stylexjs/stylex';
import { styles } from './Table.stylex';

// The old CSS styled descendants of .table-scroll (th/td/caption and the
// sibling :is(th, td) + :is(th, td) border). StyleX has no descendant
// selectors, so styles are injected into the MDX-authored table markup here.
function styleChildren(children: ReactNode, tag: 'th' | 'td'): ReactNode {
  return Children.map(children, child => {
    if (!isValidElement<{ children?: ReactNode }>(child)) return child;
    if (child.type === 'tr') {
      const cells = Children.toArray(child.props.children);
      const styled = cells.map((cell, index) => {
        if (!isValidElement(cell) || cell.type !== tag) return cell;
        const extra = index > 0 ? styles.tableCellSibling : null;
        return cloneElement(cell as ReactElement<Record<string, unknown>>, stylex.props(tag === 'th' ? styles.tableTh : styles.tableTd, extra));
      });
      return cloneElement(child, undefined, styled);
    }
    if (child.type === 'caption') {
      return cloneElement(child as ReactElement<Record<string, unknown>>, stylex.props(styles.tableCaption));
    }
    return cloneElement(child, undefined, styleChildren(child.props.children, tag));
  });
}

export function Table({ children, ...props }: TableHTMLAttributes<HTMLTableElement>) {
  return <div {...stylex.props(styles.tableScroll)} role="region" aria-label={props['aria-label'] || 'Table'} tabIndex={0}><table {...stylex.props(styles.table)} {...props}>{styleChildren(styleChildren(children as ReactNode, 'th'), 'td')}</table></div>;
}
