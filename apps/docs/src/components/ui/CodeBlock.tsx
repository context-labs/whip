import { Children, createContext, isValidElement, useContext, useState, type HTMLAttributes, type ReactNode } from 'react';
import { Tabs } from '@base-ui/react/tabs';
import { CopyButton } from './CopyButton';
import { useHydrated } from './useHydrated';

const TabbedCode = createContext(false);
type CodeBlockProps = HTMLAttributes<HTMLPreElement> & { 'data-raw'?: string; 'data-language'?: string };

// MDX already contains safe, build-time token nodes. Never parse HTML or highlight at runtime.
export function CodeBlock({ children, 'data-raw': raw, 'data-language': language = 'text', className = '', ...props }: CodeBlockProps) {
  const tabbed = useContext(TabbedCode);
  return <div className={`code-block ${tabbed ? 'code-block-tabbed' : ''}`}>
    {!tabbed && <div className="code-header"><span className="label">{language}</span>{raw !== undefined && <CopyButton text={raw} />}</div>}
    <pre {...props} className={className} data-language={language} tabIndex={0} aria-label={`${language} code`}>{children}</pre>
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
  if (!hydrated) return <div className="code-tabs-fallback">{tabs.map((tab, index) => <section key={index} className="code-tabs"><div className="code-header"><span className="label">{tab.props.label}</span></div><TabbedCode.Provider value={true}>{tab.props.children}</TabbedCode.Provider></section>)}</div>;
  return <Tabs.Root value={selected} onValueChange={value => setActive(Number(value))} className="code-tabs">
    <div className="code-header code-tabs-header"><Tabs.List className="code-tab-list" aria-label={label} activateOnFocus>
      {tabs.map((tab, index) => <Tabs.Tab key={index} value={index} className="code-tab">{tab.props.label}</Tabs.Tab>)}
    </Tabs.List>{raw !== undefined && <CopyButton key={selected} text={raw} />}</div>
    <TabbedCode.Provider value={true}>{tabs.map((tab, index) => <Tabs.Panel key={index} value={index} className="code-tab-panel">{tab.props.children}</Tabs.Panel>)}</TabbedCode.Provider>
  </Tabs.Root>;
}
