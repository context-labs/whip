import { Children, createContext, isValidElement, useContext, useState, type HTMLAttributes, type ReactNode } from 'react';
import { Tabs } from '@base-ui/react/tabs';
import * as stylex from '@stylexjs/stylex';
import { CopyButton } from './CopyButton';
import { useHydrated } from './useHydrated';
import { styles } from './CodeBlock.stylex';

const TabbedCode = createContext(false);
type CodeBlockProps = HTMLAttributes<HTMLPreElement> & { 'data-raw'?: string; 'data-language'?: string };

// The browser tests select these structural classes (.code-block, .code-header,
// .code-tab-list, ...), so the StyleX class names are merged with them instead
// of being replaced by a trailing className prop.
export function CodeBlock({ children, 'data-raw': raw, 'data-language': language = 'text', className = '', ...props }: CodeBlockProps) {
  const tabbed = useContext(TabbedCode);
  const blockSx = stylex.props(styles.codeBlock, tabbed && styles.codeBlockTabbed);
  const headerSx = stylex.props(styles.codeHeader);
  const preSx = stylex.props(styles.codePre);
  return <div {...blockSx} className={['code-block', tabbed && 'code-block-tabbed', blockSx.className].filter(Boolean).join(' ')}>
    {!tabbed && <div {...headerSx} className={`code-header ${headerSx.className}`}><span className="label">{language}</span>{raw !== undefined && <CopyButton text={raw} />}</div>}
    <pre {...props} {...preSx} className={[preSx.className, className].filter(Boolean).join(' ') || undefined} data-language={language} tabIndex={0} aria-label={`${language} code`}>{children}</pre>
  </div>;
}

export type CodeTabProps = { label: string; children: ReactNode };
export function CodeTab({ children }: CodeTabProps) { return <>{children}</>; }

function rawCode(children: ReactNode): string | undefined {
  for (const child of Children.toArray(children)) {
    if (!isValidElement<Record<string, unknown>>(child)) continue;
    if (typeof child.props['data-raw'] === 'string') return child.props['data-raw'];
    const nested = rawCode(child.props.children as ReactNode);
    if (nested !== undefined) return nested;
  }
  return undefined;
}

export function CodeTabs({ children, label = 'Code examples' }: { children: ReactNode; label?: string }) {
  const hydrated = useHydrated();
  const tabs = Children.toArray(children).filter(isValidElement<CodeTabProps>);
  const [active, setActive] = useState(0);
  if (tabs.length === 0) return null;
  const selected = Math.min(active, tabs.length - 1);
  const raw = rawCode(tabs[selected]?.props.children);
  // With no JavaScript every example is readable, labelled, selectable and scrollable.
  if (!hydrated) return <div className="code-tabs-fallback">{tabs.map((tab, index) => {
    const sectionSx = stylex.props(styles.codeTabs);
    const sectionHeaderSx = stylex.props(styles.codeHeader);
    return <section key={index} {...sectionSx} className={`code-tabs ${sectionSx.className}`}><div {...sectionHeaderSx} className={`code-header ${sectionHeaderSx.className}`}><span className="label">{tab.props.label}</span></div><TabbedCode.Provider value={true}>{tab.props.children}</TabbedCode.Provider></section>;
  })}</div>;
  const rootSx = stylex.props(styles.codeTabs);
  const headerSx = stylex.props(styles.codeHeader, styles.codeTabsHeader);
  const listSx = stylex.props(styles.codeTabList);
  const panelSx = stylex.props(styles.codeTabPanel);
  return <Tabs.Root value={selected} onValueChange={value => setActive(Number(value))} {...rootSx} className={`code-tabs ${rootSx.className}`}>
    <div {...headerSx} className={`code-header code-tabs-header ${headerSx.className}`}><Tabs.List {...listSx} className={`code-tab-list ${listSx.className}`} aria-label={label} activateOnFocus>
      {tabs.map((tab, index) => <Tabs.Tab key={index} value={index} className={state => {
        const tabSx = stylex.props(styles.codeTab, state.active && styles.codeTabActive);
        return `code-tab ${tabSx.className}`;
      }}>{tab.props.label}</Tabs.Tab>)}
    </Tabs.List>{raw !== undefined && <CopyButton key={selected} text={raw} />}</div>
    <TabbedCode.Provider value={true}>{tabs.map((tab, index) => <Tabs.Panel key={index} value={index} {...panelSx} className={`code-tab-panel ${panelSx.className}`}>{tab.props.children}</Tabs.Panel>)}</TabbedCode.Provider>
  </Tabs.Root>;
}
