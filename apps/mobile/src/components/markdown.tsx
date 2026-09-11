import { memo } from 'react';
import { Linking, Platform, ScrollView } from 'react-native';
import { EnrichedMarkdownText, type MarkdownStyle } from 'react-native-enriched-markdown';
import { externalLink } from '../runtime/address';
import { useTheme, useDisplay } from '../theme/theme';
import { fonts } from '../theme/fonts';
import { Label } from './primitives';

/** Conservative source fallback prevents native markdown from fetching images.
 * Every CommonMark image starts with ![; all raw HTML uses <. Preserve the full
 * authored source (including code) instead of rewriting it with a regex parser.
 */
export function requiresSourcePresentation(markdown: string) { return markdown.includes('![') || markdown.includes('<'); }
export const Markdown = memo(function Markdown({ text }: { text: string }) {
  const theme = useTheme(); const display = useDisplay();
  if (!display.wrapCode && text.includes('```')) return <ScrollView horizontal><Label selectable style={{ fontFamily: display.mono, fontSize: display.codeSize }}>{text}</Label></ScrollView>;
  if (requiresSourcePresentation(text)) return <Label selectable>{text}</Label>;
  const body = { color: theme.colors.foreground, fontSize: 16 * display.textScale!, lineHeight: 26 * display.textScale!, fontFamily: display.regular };
  const heading = { color: theme.markdown.heading, fontFamily: display.semibold, fontSize: 20 * display.textScale!, lineHeight: 30 * display.textScale!, marginTop: 16, marginBottom: 8 };
  const style: MarkdownStyle = {
    paragraph: body, h1: { ...heading, fontSize: 26 * display.textScale!, lineHeight: 36 * display.textScale! }, h2: heading, h3: heading, h4: heading, h5: heading, h6: heading,
    strong: { color: theme.markdown.strong, fontFamily: display.semibold }, em: { color: theme.colors.emphasis },
    link: { color: theme.colors.link }, list: { ...body, bulletColor: theme.colors.muted, markerColor: theme.colors.muted, itemSpacing: 8 },
    codeBlock: { fontFamily: display.mono, fontSize: display.codeSize, lineHeight: display.codeSize! * 1.6, color: theme.code.foreground, backgroundColor: theme.code.background, borderColor: theme.colors.border, borderRadius: 12, padding: 14, syntaxColors: { ...theme.syntax, constant: theme.syntax.number, variable: theme.code.foreground, property: theme.syntax.type, tag: theme.syntax.keyword, attribute: theme.syntax.string, embedded: theme.code.foreground } },
    code: { fontFamily: display.mono, color: theme.markdown.code, backgroundColor: theme.web?.inlineCodeBackground ?? theme.colors.element },
    blockquote: { ...body, color: theme.markdown.quote, borderColor: theme.colors.border, backgroundColor: theme.colors.panel },
    thematicBreak: { color: theme.colors.border }, table: { color: theme.colors.foreground, headerTextColor: theme.markdown.strong, borderColor: theme.colors.border, headerBackgroundColor: theme.colors.element, rowEvenBackgroundColor: theme.colors.background, rowOddBackgroundColor: theme.colors.panel },
    taskList: { checkedColor: theme.colors.primary, checkmarkColor: theme.colors.onPrimary, borderColor: theme.colors.border, checkedTextColor: theme.colors.muted },
  };
  return <EnrichedMarkdownText markdown={text} markdownStyle={style} selectable allowFontScaling enableLinkPreview={false} flavor="github"
    onLinkPress={({ url }) => { const safe = externalLink(url); if (safe) void Linking.openURL(safe).catch(() => {}); }} />;
});
