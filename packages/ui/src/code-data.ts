import type {ThemeDefinition, TokenStyle} from './generated/theme-catalog';

export const codeByteLimit = 16384;
/** encodeInto stops at the byte budget without allocating/encoding an entire
 * potentially large caller-owned string, and never splits a Unicode character. */
export function boundedCode(code: string, requested = codeByteLimit) {
  const max = Number.isFinite(requested) ? Math.max(1, Math.min(codeByteLimit, Math.floor(requested))) : codeByteLimit;
  const buffer = new Uint8Array(max);
  const {read = 0, written = 0} = new TextEncoder().encodeInto(code, buffer);
  return {text: code.slice(0, read), bytes: written, truncated: read < code.length};
}
const chromaRoles: Record<string, readonly string[]> = {
  comment: ['Comment'], prolog: ['CommentPreproc', 'Comment'], doctype: ['CommentPreproc', 'Comment'],
  keyword: ['Keyword'], boolean: ['KeywordConstant', 'Keyword'], builtin: ['NameBuiltin', 'NameOther'],
  function: ['NameFunction', 'NameOther'], 'function-variable': ['NameFunction', 'NameOther'],
  'class-name': ['NameClass', 'KeywordType'], 'type-class-name': ['KeywordType', 'NameClass'],
  'class-name-definition': ['NameClass', 'KeywordType'], 'function-definition': ['NameFunction', 'NameOther'],
  property: ['NameAttribute', 'NameOther'], parameter: ['NameVariable', 'NameOther'], variable: ['NameVariable', 'NameOther'],
  decorator: ['NameDecorator', 'NameOther'], constant: ['NameConstant', 'NameOther'],
  string: ['LiteralString'], char: ['LiteralStringChar', 'LiteralString'], regex: ['LiteralStringRegex', 'LiteralString'],
  'template-string': ['LiteralString'], number: ['LiteralNumber'], operator: ['Operator'], punctuation: ['Punctuation'],
};
const syntaxRoles: Record<string, keyof ThemeDefinition['syntax']> = {comment: 'comment', keyword: 'keyword', boolean: 'keyword', function: 'function', 'function-definition': 'function', builtin: 'function', 'class-name': 'type', string: 'string', char: 'string', regex: 'string', 'template-string': 'string', number: 'number', operator: 'operator', punctuation: 'punctuation'};
export function codeTokenStyle(theme: ThemeDefinition, kind: string): TokenStyle {
  for (const role of chromaRoles[kind] ?? []) {const style = theme.code.tokens[role]; if (style) return style;}
  return {color: syntaxRoles[kind] ? theme.syntax[syntaxRoles[kind]!] : theme.code.foreground, background: theme.code.background, bold: false, italic: false, underline: false};
}
