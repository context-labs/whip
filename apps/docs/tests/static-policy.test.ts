import { it, expect } from 'vitest'
import { robotsText, runtimeHighlighter } from '../scripts/static-policy.mjs'
it('emits real robots lines with no default publication claim', () => {
  expect(robotsText('').split(String.fromCharCode(10))).toEqual(['User-agent: *', 'Disallow: /', ''])
  expect(robotsText('https://docs.example.test').split(String.fromCharCode(10))).toEqual(['User-agent: *', 'Allow: /', 'Sitemap: https://docs.example.test/sitemap.xml', ''])
})
it('recognizes deliberately leaked runtime highlighter/grammar implementations', () => {
  for (const code of ['refractor.highlight(raw)', 'Prism.languages.bash = {}', 'function bash(Prism) {}', 'function javascript(Prism) {}']) expect(runtimeHighlighter.test(code)).toBe(true)
  expect(runtimeHighlighter.test('Article about Prism token categories')).toBe(false)
})
