import { useEffect, useState, type ReactNode } from 'react';
import * as stylex from '@stylexjs/stylex';
import type { DocHeading, DocMeta } from '../content/types';
import { PagerCard, SidebarItem } from '../../../components/navigation';
import { colors, scale } from '~/tokens.stylex';
import { text } from '~/styles/typography.stylex';

import { docSections as sections } from '../content/sections';

const styles = stylex.create({
  layout: {
    width: { default: `min(${scale.siteMaxWidth}, 100% - 64px)`, [scale.phone]: 'calc(100% - 40px)', [scale.print]: '100%' },
    marginInline: 'auto',
    paddingTop: { default: 56, [scale.phone]: 24, [scale.print]: 0 },
    paddingBottom: { default: 96, [scale.phone]: 64, [scale.print]: 0 },
    display: { default: 'grid', [scale.phone]: 'block', [scale.print]: 'block' },
    gridTemplateColumns: { default: '228px minmax(0, 1fr) 188px', [scale.tablet]: '200px minmax(0, 1fr)' },
    gap: 32,
    alignItems: 'start',
  },
  sidebar: {
    borderRightWidth: 1,
    borderRightStyle: 'solid',
    borderRightColor: colors.border,
    paddingRight: 16,
    alignSelf: 'stretch',
    display: { default: null, [scale.phone]: 'none', [scale.print]: 'none' },
  },
  navigation: {
    position: 'sticky',
    top: 32,
    maxHeight: 'calc(100dvh - 64px)',
    overflowY: 'auto',
  },
  sidebarGroupSpacing: {
    marginTop: 32,
  },
  sidebarLabel: {
    paddingLeft: 8,
    marginBottom: 8,
  },
  readingColumn: {
    minWidth: 0,
  },
  main: {
    minWidth: 0,
    outlineWidth: { ':focus': 0 },
    outlineStyle: { ':focus': 'none' },
  },
  tocAside: {
    minWidth: 0,
    position: 'sticky',
    top: 32,
    maxHeight: 'calc(100dvh - 64px)',
    overflowY: 'auto',
    display: { default: null, [scale.tablet]: 'none', [scale.print]: 'none' },
  },
  // Keep the TOC in the DOM for search engines, but hide it visually.
  mobileToc: {
    display: { default: 'none', [scale.print]: 'none' },
    position: { [scale.tablet]: 'absolute' },
    width: { [scale.tablet]: 1 },
    height: { [scale.tablet]: 1 },
    margin: { [scale.tablet]: -1 },
    padding: { [scale.tablet]: 0 },
    borderWidth: { [scale.tablet]: 0 },
    borderStyle: { [scale.tablet]: 'none' },
    overflow: { [scale.tablet]: 'hidden' },
    clip: { [scale.tablet]: 'rect(0 0 0 0)' },
    clipPath: { [scale.tablet]: 'inset(50%)' },
    whiteSpace: { [scale.tablet]: 'nowrap' },
  },
  mobileTocSummary: {
    display: { [scale.tablet]: 'none' },
  },
  docHeading: {
    paddingBottom: 32,
    borderBottomWidth: 1,
    borderBottomStyle: 'solid',
    borderBottomColor: colors.border,
    marginBottom: 32,
  },
  docHeadingGettingStarted: {
    marginBottom: 36,
  },
  titleRow: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    gap: 24,
  },
  title: {
    flexGrow: 1,
    flexShrink: 1,
    flexBasis: '0%',
    minWidth: 0,
  },
  pageLead: {
    marginTop: 16,
  },
  pagination: {
    display: { default: 'grid', [scale.print]: 'none' },
    gridTemplateColumns: 'repeat(2, minmax(0, 1fr))',
    gap: { default: 16, [scale.phone]: 12 },
    marginTop: 64,
    paddingTop: 32,
    borderTopWidth: 1,
    borderTopStyle: 'solid',
    borderTopColor: colors.border,
  },
  paginationGettingStarted: {
    gap: 12,
    paddingTop: 24,
  },
  tocTitle: {
    marginBottom: 12,
  },
  tocList: {
    padding: 0,
    margin: 0,
    borderLeftWidth: 1,
    borderLeftStyle: 'solid',
    borderLeftColor: colors.border,
    listStyle: 'none',
  },
  tocLink: {
    display: 'block',
    marginLeft: -1,
    borderLeftWidth: 2,
    borderLeftStyle: 'solid',
    borderLeftColor: 'transparent',
    paddingTop: 8,
    paddingBottom: 8,
    paddingRight: 12,
    paddingLeft: 12,
    fontWeight: 400,
    fontSize: 11,
    lineHeight: '18px',
    fontFamily: 'var(--font-sans)',
    color: { default: colors.textMuted, ':hover': colors.text },
    textDecorationLine: 'none',
  },
  tocLinkLevel3: {
    paddingLeft: 24,
  },
  tocLinkActive: {
    borderLeftColor: colors.text,
    color: colors.text,
    fontWeight: 500,
  },
});

export function DocsSidebar({ entries, current }: { entries: readonly DocMeta[]; current?: DocMeta }) {
  const nav = stylex.props(styles.navigation);
  return <nav aria-label="Documentation" {...nav} className={`docs-navigation ${nav.className}`}>{sections.map((section, index) => <div {...stylex.props(index > 0 && styles.sidebarGroupSpacing)} key={section.id}>
    <div {...stylex.props(text.label, styles.sidebarLabel)} className={`sidebar-label ${stylex.props(text.label, styles.sidebarLabel).className}`}>{section.label}</div>
    {entries.filter(entry => entry.section === section.id).sort((a, b) => a.order - b.order).map(entry => <SidebarItem key={entry.path} href={`/docs/${entry.path}`} active={current?.path === entry.path}>{entry.navTitle ?? entry.title}</SidebarItem>)}
  </div>)}</nav>;
}

export function TableOfContents({ headings }: { headings: readonly DocHeading[] }) {
  const [active, setActive] = useState(headings[0]?.id || '');
  useEffect(() => {
    let frame = 0;
    const update = () => {
      frame = 0;
      let next = headings[0]?.id || '';
      for (const heading of headings) {
        const node = document.getElementById(heading.id);
        if (node && node.getBoundingClientRect().top <= 140) next = heading.id;
      }
      setActive(next);
    };
    const schedule = () => { if (!frame) frame = requestAnimationFrame(update); };
    update();
    window.addEventListener('scroll', schedule, { passive: true });
    window.addEventListener('resize', schedule);
    return () => { cancelAnimationFrame(frame); window.removeEventListener('scroll', schedule); window.removeEventListener('resize', schedule); };
  }, [headings]);
  if (headings.length === 0) return null;
  const titleSx = stylex.props(text.label, styles.tocTitle);
  return <nav aria-label="On this page" className="toc"><div {...titleSx} className={`toc-title ${titleSx.className}`}>On this page</div><ul {...stylex.props(styles.tocList)}>{headings.map(heading => { const linkSx = stylex.props(styles.tocLink, heading.level === 3 && styles.tocLinkLevel3, heading.id === active && styles.tocLinkActive); return <li key={heading.id}><a href={`#${heading.id}`} {...linkSx} className={`toc-link toc-level-${heading.level} ${linkSx.className}`} aria-current={heading.id === active ? 'location' : undefined} onClick={() => setActive(heading.id)}>{heading.text}</a></li>; })}</ul></nav>;
}

export function DocsLayout({ entries, current, children, actions, className = '', tocHeadings, nextPage }: {
  entries: readonly DocMeta[]; current?: DocMeta; children: ReactNode;
  actions?: ReactNode; className?: string; tocHeadings?: readonly DocHeading[]; nextPage?: DocMeta;
}) {
  const sorted = sections.flatMap(section => entries.filter(entry => entry.section === section.id).sort((a, b) => a.order - b.order));
  const currentIndex = current ? sorted.findIndex(entry => entry.path === current.path) : -1;
  const previous = sorted[currentIndex - 1];
  const next = nextPage ?? (currentIndex >= 0 ? sorted[currentIndex + 1] : undefined);
  const headings = tocHeadings ?? current?.headings ?? [];
  const gettingStarted = className.split(' ').includes('getting-started-page');
  const layoutSx = stylex.props(styles.layout);
  const sidebarSx = stylex.props(styles.sidebar);
  const columnSx = stylex.props(styles.readingColumn);
  const mobileTocSx = stylex.props(styles.mobileToc);
  const mainSx = stylex.props(styles.main);
  const headingSx = stylex.props(styles.docHeading, gettingStarted && styles.docHeadingGettingStarted);
  const titleRowSx = stylex.props(styles.titleRow);
  const leadSx = stylex.props(text.pageLead, styles.pageLead);
  const paginationSx = stylex.props(styles.pagination, gettingStarted && styles.paginationGettingStarted);
  const tocAsideSx = stylex.props(styles.tocAside);
  return <div {...layoutSx} className={`docs-layout ${layoutSx.className} ${className}`.trim()}>
    <aside {...sidebarSx} className={`docs-sidebar ${sidebarSx.className}`}><DocsSidebar entries={entries} current={current} /></aside>
    <div {...columnSx} className={`docs-reading-column ${columnSx.className}`}>
      {headings.length > 0 && <details {...mobileTocSx} className={`mobile-toc ${mobileTocSx.className}`}><summary {...stylex.props(styles.mobileTocSummary)}>On this page</summary><TableOfContents headings={headings} /></details>}
      <main id="main-content" {...mainSx} className={`docs-main ${mainSx.className}`} tabIndex={-1}>
        {current && <header {...headingSx} className={`doc-heading ${headingSx.className}`}><div {...titleRowSx} className={`doc-title-row ${titleRowSx.className}`}><h1 {...stylex.props(styles.title)}>{current.title}</h1>{actions}</div><p {...leadSx} className={`page-lead ${leadSx.className}`}>{current.description}</p></header>}
        <article className="prose">{children}</article>
        {(previous || next) && <nav {...paginationSx} className={`doc-pagination ${paginationSx.className}`} aria-label="Adjacent pages">{previous ? <PagerCard href={`/docs/${previous.path}`} title={previous.title} direction="previous" /> : <span />}{next && <PagerCard href={`/docs/${next.path}`} title={next.title} direction="next" />}</nav>}
      </main>
    </div>
    <aside {...tocAsideSx} className={`docs-toc ${tocAsideSx.className}`}>{current && <TableOfContents headings={headings} />}</aside>
  </div>;
}
