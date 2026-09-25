import { useEffect, useState, type ReactNode } from 'react';
import type { DocHeading, DocMeta } from '../content/types';
import { MobileNavigation, PagerCard, SidebarItem } from '../../../components/navigation';

import { docSections as sections } from '../content/sections';
export function DocsSidebar({ entries, current }: { entries: readonly DocMeta[]; current?: DocMeta }) {
  return <nav aria-label="Documentation" className="docs-navigation">{sections.map(section => <div className="sidebar-group" key={section.id}>
    <div className="label sidebar-label">{section.label}</div>
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
  return <nav aria-label="On this page" className="toc"><div className="label toc-title">On this page</div><ul>{headings.map(heading => <li key={heading.id}><a href={`#${heading.id}`} className={`toc-link toc-level-${heading.level}`} aria-current={heading.id === active ? 'location' : undefined} onClick={() => setActive(heading.id)}>{heading.text}</a></li>)}</ul></nav>;
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
  return <div className={`docs-layout ${className}`}>
    <aside className="docs-sidebar"><DocsSidebar entries={entries} current={current} /></aside>
    <div className="docs-reading-column">
      <MobileNavigation title="Documentation" className="docs-mobile-navigation"><DocsSidebar entries={entries} current={current} /></MobileNavigation>
      {headings.length > 0 && <details className="mobile-toc"><summary>On this page</summary><TableOfContents headings={headings} /></details>}
      <main id="main-content" className="docs-main" tabIndex={-1}>
        {current && <header className="doc-heading"><div className="doc-title-row"><h1>{current.title}</h1>{actions}</div><p className="page-lead">{current.description}</p></header>}
        <article className="prose">{children}</article>
        {(previous || next) && <nav className="doc-pagination" aria-label="Adjacent pages">{previous ? <PagerCard href={`/docs/${previous.path}`} title={previous.title} direction="previous" /> : <span />}{next && <PagerCard href={`/docs/${next.path}`} title={next.title} direction="next" />}</nav>}
      </main>
    </div>
    <aside className="docs-toc">{current && <TableOfContents headings={headings} />}</aside>
  </div>;
}
