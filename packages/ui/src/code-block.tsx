import * as stylex from '@stylexjs/stylex';
import {useEffect, useMemo, useState} from 'react';
import type {ReactNode} from 'react';
import {appearance, colors, surface, typography} from './tokens.stylex';
import {useTheme} from './themes';
import {boundedCode, codeTokenStyle} from './code-data';
import type {HighlightedCode} from './code-highlight';
import type {Styled} from './actions';

export interface CodeBlockProps extends Styled {
  code: string;
  language?: string;
  label?: string;
  maxBytes?: number;
  truncated?: boolean;
  /** Caller owns content authorization, fetching and downloading. */
  downloadAction?: ReactNode;
}
const styles = stylex.create({
  root: {minWidth: 0, margin: 0, border: `1px solid ${surface.quietBorder}`, borderRadius: 8, overflow: 'hidden'},
  header: {display: 'flex', justifyContent: 'space-between', alignItems: 'center', gap: 12, paddingBlock: 6, paddingInline: 12, color: surface.secondaryText, backgroundColor: colors.panel, fontFamily: typography.mono, fontSize: typography.size11},
  pre: {margin: 0, padding: 12, fontFamily: typography.mono, fontSize: typography.codeSize, lineHeight: '1.6667', overflow: 'auto', tabSize: 2, whiteSpace: appearance.codeWhiteSpace, overflowWrap: appearance.codeOverflowWrap, maxHeight: 520},
  status: {paddingBlock: 7, paddingInline: 12, margin: 0, color: surface.secondaryText, backgroundColor: colors.panel, fontSize: typography.size12, lineHeight: '1.5'},
});
export function CodeBlock({code, language, label, maxBytes, truncated, downloadAction, xstyle}: CodeBlockProps) {
  const {resolvedTheme} = useTheme();
  const bounded = useMemo(() => boundedCode(code, maxBytes), [code, maxBytes]);
  const [highlighted, setHighlighted] = useState<{text: string; language?: string; result: HighlightedCode} | null>(null);
  useEffect(() => {
    let active = true;
    if (!language || ['plaintext', 'text', 'txt'].includes(language)) return;
    void import('./code-highlight').then(module => {
      if (!active) return;
      const result = module.highlightCode(bounded.text, language);
      if (active) setHighlighted({text: bounded.text, language, result});
    }, () => {if (active) setHighlighted({text: bounded.text, language, result: {tokens: [{text: bounded.text, kind: 'plain', offset: 0}], unavailable: 'Syntax highlighting could not load. Showing plain text.'}});});
    return () => {active = false;};
  }, [bounded.text, language]);
  const result = highlighted?.text === bounded.text && highlighted.language === language ? highlighted.result : undefined;
  const limited = truncated || bounded.truncated;
  return <figure {...stylex.props(styles.root, xstyle)} data-highlighted={result ? 'true' : 'false'}><figcaption {...stylex.props(styles.header)}><span>{label ?? language ?? 'Plain text'}</span>{downloadAction}</figcaption><pre role="region" tabIndex={0} aria-label={label ?? `${language ?? 'Plain text'} code`} {...stylex.props(styles.pre)} style={{color: resolvedTheme.code.foreground, backgroundColor: resolvedTheme.code.background}}><code>{result ? result.tokens.map(token => {
    const style = codeTokenStyle(resolvedTheme, token.kind);
    return <span key={token.offset} data-token={token.kind} style={{color: style.color, backgroundColor: style.background, fontWeight: style.bold ? 700 : undefined, fontStyle: style.italic ? 'italic' : undefined, textDecoration: style.underline ? 'underline' : undefined}}>{token.text}</span>;
  }) : bounded.text}</code></pre>{limited && <p role="status" {...stylex.props(styles.status)}>Showing a bounded excerpt ({bounded.bytes.toLocaleString()} bytes). The remaining output is not displayed.</p>}{result?.unavailable && <p {...stylex.props(styles.status)}>{result.unavailable}</p>}</figure>;
}
