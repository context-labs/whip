import { memo } from 'react';
import { Linking, Platform } from 'react-native';
import { EnrichedMarkdownText, type MarkdownStyle } from 'react-native-enriched-markdown';
import { externalLink } from '../runtime/address';
import { useTheme } from '../theme/theme';
import { fonts } from '../theme/fonts';
import { Label } from './primitives';

/** Conservative source fallback prevents native markdown from fetching images.
 * Every CommonMark image starts with ![; all raw HTML uses <. Preserve the full
 * authored source (including code) instead of rewriting it with a regex parser.
 */
export function requiresSourcePresentation(markdown: string) { return markdown.includes('![') || markdown.includes('<'); }
export const Markdown = memo(function Markdown({ text }: { text: string }) {
  const theme = useTheme();
  if (requiresSourcePresentation(text)) return <Label selectable>{text}</Label>;
  const style: MarkdownStyle = {
    paragraph: { color: theme.colors.foreground, fontSize: 16, lineHeight: 24, fontFamily: fonts.regular },
    link: { color: theme.colors.link },
    codeBlock: { fontFamily: fonts.mono ?? (Platform.OS === 'ios' ? 'Menlo' : 'monospace'), fontSize: 13, color: theme.code.foreground, backgroundColor: theme.code.background },
    code: { fontFamily: fonts.mono, color: theme.markdown.code, backgroundColor: theme.colors.element },
    blockquote: { color: theme.markdown.quote, borderColor: theme.colors.border },
  };
  return <EnrichedMarkdownText markdown={text} markdownStyle={style} selectable allowFontScaling enableLinkPreview={false} flavor="github"
    onLinkPress={({ url }) => { const safe = externalLink(url); if (safe) void Linking.openURL(safe).catch(() => {}); }} />;
});
