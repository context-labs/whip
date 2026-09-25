import type { AnchorHTMLAttributes } from 'react';
import * as stylex from '@stylexjs/stylex';
import { sitePath } from '../../features/docs/content/site-path';
import { GitHubMark, Icon } from '../ui/Icons';
import { text } from '~/styles/typography.stylex';
import { styles } from './Navigation.stylex';
export function TopNavLink({ active, className = '', ...props }: AnchorHTMLAttributes<HTMLAnchorElement> & { active?: boolean }) {
  const sx = stylex.props(styles.topNavLink, active && styles.topNavLinkActive);
  return <a className={`top-nav-link ${sx.className} ${className}`.trim()} aria-current={active ? 'page' : undefined} {...props} href={props.href && sitePath(props.href)} />;
}
export function SidebarItem({ active, className = '', ...props }: AnchorHTMLAttributes<HTMLAnchorElement> & { active?: boolean }) {
  const sx = stylex.props(styles.sidebarItem, active && styles.sidebarItemActive);
  return <a className={`sidebar-item ${sx.className} ${className}`.trim()} aria-current={active ? 'page' : undefined} {...props} href={props.href && sitePath(props.href)} />;
}
export function CommunityLink({ href = 'https://github.com/context-labs/whip', label = 'GitHub', children = <GitHubMark />, className = '', ...props }: AnchorHTMLAttributes<HTMLAnchorElement> & { label?: string }) {
  const sx = stylex.props(styles.communityLink);
  return <a className={`community-link ${sx.className} ${className}`.trim()} href={href} aria-label={label} title={label} {...props}>{children}</a>;
}
export function PagerCard({ href, title, direction }: { href: string; title: string; direction: 'previous' | 'next' }) {
  const cardSx = stylex.props(styles.pagerCard, direction === 'next' && styles.pagerNext);
  const labelSx = stylex.props(text.label, styles.pagerLabel);
  const titleSx = stylex.props(styles.pagerTitle);
  return <a href={sitePath(href)} {...cardSx} className={`pager-card pager-${direction} ${cardSx.className}`}>
    {direction === 'previous' && <Icon name="arrow-left" {...stylex.props(styles.pagerIcon)} />}
    <span><span {...labelSx} className={`label ${labelSx.className}`}>{direction === 'previous' ? 'Previous' : 'Next'}</span><span {...titleSx} className={`pager-title ${titleSx.className}`}>{title}</span></span>
    {direction === 'next' && <Icon name="arrow-right" {...stylex.props(styles.pagerIcon)} />}
  </a>;
}
