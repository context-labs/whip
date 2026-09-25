import type { AnchorHTMLAttributes } from 'react';
import { sitePath } from '../../features/docs/content/site-path';
import { GitHubMark, Icon } from '../ui/Icons';
export function TopNavLink({ active, className = '', ...props }: AnchorHTMLAttributes<HTMLAnchorElement> & { active?: boolean }) {
  return <a className={`top-nav-link ${className}`} aria-current={active ? 'page' : undefined} {...props} href={props.href && sitePath(props.href)} />;
}
export function SidebarItem({ active, className = '', ...props }: AnchorHTMLAttributes<HTMLAnchorElement> & { active?: boolean }) {
  return <a className={`sidebar-item ${className}`} aria-current={active ? 'page' : undefined} {...props} href={props.href && sitePath(props.href)} />;
}
export function CommunityLink({ href = 'https://github.com/context-labs/whip', label = 'GitHub', children = <GitHubMark />, ...props }: AnchorHTMLAttributes<HTMLAnchorElement> & { label?: string }) {
  return <a className="community-link" href={href} aria-label={label} title={label} {...props}>{children}</a>;
}
export function PagerCard({ href, title, direction }: { href: string; title: string; direction: 'previous' | 'next' }) {
  return <a href={sitePath(href)} className={`pager-card pager-${direction}`}>
    {direction === 'previous' && <Icon name="arrow-left" />}
    <span><span className="label">{direction === 'previous' ? 'Previous' : 'Next'}</span><span className="pager-title">{title}</span></span>
    {direction === 'next' && <Icon name="arrow-right" />}
  </a>;
}
