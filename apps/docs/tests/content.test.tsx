import type { DocHeading } from '../src/features/docs/content/types'
// @vitest-environment node
import { describe, it, expect } from 'vitest'
import { mkdtemp, mkdir, writeFile, rm, readFile } from 'node:fs/promises'
import os from 'node:os'
import path from 'node:path'
import { evaluate } from '@mdx-js/mdx'
import * as runtime from 'react/jsx-runtime'
import { renderToStaticMarkup } from 'react-dom/server'
import { JSDOM } from 'jsdom'
import { parseDocument, validateDocuments, loadDocuments } from '../scripts/content.mjs'
import { docsMdx, mdxOptions } from '../scripts/mdx-plugins.mjs'
import { siteUrl } from '../scripts/site-url.mjs'
import { docsComponents } from '../src/features/docs/docs-components'

const frontmatter = '---\ntitle: Example\ndescription: A useful example.\nsection: start\norder: 1\n---\n\n'
async function compile(source: string) {
  const { default: Content } = await evaluate(source, { ...mdxOptions, ...runtime })
  return new JSDOM(renderToStaticMarkup(<Content components={docsComponents} />)).window.document
}

describe('content validation', () => {
  it('extracts metadata and shared deterministic heading IDs from the AST', async () => {
    const source = frontmatter + '## Hello *world* & `code`!\n\n### Hello *world* & `code`!\n\n## 日本語 — café\n\n```text\n## Not a heading\n```\n'
    const parsed = parseDocument(source, 'headings.mdx')
    expect(parsed.headings).toEqual([
      { id: 'hello-world--code', text: 'Hello world & code!', level: 2 },
      { id: 'hello-world--code-1', text: 'Hello world & code!', level: 3 },
      { id: '日本語--café', text: '日本語 — café', level: 2 },
    ])
    const document = await compile(source)
    expect([...document.querySelectorAll('h2,h3')].map((node) => ({ id: node.id, text: node.textContent }))).toEqual(parsed.headings.map(({ id, text }: { id: string; text: string }) => ({ id, text })))
  })
  it.each([
    ['', /frontmatter/],
    [frontmatter.replace('section: start', 'section: bogus'), /invalid frontmatter/],
    [frontmatter.replace('order: 1', 'order: 0'), /invalid frontmatter/],
    [frontmatter + '# Not allowed', /H1/],
  ])('rejects invalid content with source context', (source, error) => {
    expect(() => parseDocument(source, 'broken.mdx')).toThrow(error)
    expect(() => parseDocument(source, 'broken.mdx')).toThrow('broken.mdx')
  })
  it('rejects duplicate paths and section order, broken links and fragments', () => {
    const doc = { path: 'one', ...parseDocument(frontmatter + '## Section', 'one.mdx') }
    expect(() => validateDocuments([doc, doc])).toThrow('Duplicate documentation path')
    expect(() => validateDocuments([doc, { ...doc, path: 'two' }])).toThrow('duplicate order')
    for (const link of ['/docs/missing', '/docs/one#missing', '/docs/one/', 'one', 'javascript:alert(1)', '//other.example/']) {
      expect(() => validateDocuments([{ ...doc, links: [link] }])).toThrow()
    }
    expect(() => validateDocuments([{ ...doc, links: ['#section', '/docs', '/', 'https://github.com/context-labs/whip'] }])).not.toThrow()
  })
  it('discovers nested additions and deletions without retaining article bodies', async () => {
    const root = await mkdtemp(path.join(os.tmpdir(), 'whip-docs-content-'))
    try {
      await mkdir(path.join(root, 'one'), { recursive: true })
      await writeFile(path.join(root, 'one/index.mdx'), frontmatter)
      expect((await loadDocuments(root)).map((doc) => doc.path)).toEqual(['one'])
      await mkdir(path.join(root, 'nested/two'), { recursive: true })
      await writeFile(path.join(root, 'nested/two/index.mdx'), frontmatter.replace('order: 1', 'order: 2'))
      expect((await loadDocuments(root)).map((doc) => doc.path)).toEqual(['one', 'nested/two'])
      await rm(path.join(root, 'one'), { recursive: true })
      expect((await loadDocuments(root)).map((doc) => doc.path)).toEqual(['nested/two'])
      expect(JSON.stringify(await loadDocuments(root))).not.toContain('---')
    } finally { await rm(root, { recursive: true, force: true }) }
  })
  it('warns on an unsupported explicit fence while retaining plain text fallback', () => {
    const doc = parseDocument(frontmatter + '```unknown-language\nsafe text\n```', 'warning.mdx')
    expect(doc.warnings).toEqual(['warning.mdx: unsupported code language "unknown-language"; rendered as plain text'])
  })
  it('validates every launch source and internal link', async () => {
    const docs = await loadDocuments()
    const outline = JSON.parse(await readFile(new URL('./fixtures/v1-outline.json', import.meta.url), 'utf8'))
    expect(docs).toHaveLength(21)
    expect(docs.map(doc => [doc.path, doc.title, doc.section, doc.description, doc.headings.filter((heading: DocHeading) => heading.level === 2).map((heading: DocHeading) => heading.text)])).toEqual(outline.pages)
    for (const doc of docs.filter(doc => doc.path !== 'typescript-sdk')) expect(doc.headings.every((heading: DocHeading) => heading.level === 2)).toBe(true)
  })
})

it('keeps a short navigation label independent from the article title', () => {
  const source = frontmatter.replace('title: Example', 'title: Getting started\nnavTitle: Get started')
  expect(parseDocument(source, 'index.mdx')).toMatchObject({ title: 'Getting started', navTitle: 'Get started' })
})

it('leaves Vite raw MDX imports uncompiled for page copy and download', async () => {
  const plugin = docsMdx()
  for (const id of ['/page.mdx?raw', '/page.mdx?raw&import', '/page.mdx?import&raw']) {
    expect(await plugin.transform('export default "original source"', id)).toBeUndefined()
  }
  expect(await plugin.transform('# Compiled heading', '/page.mdx')).toHaveProperty('code')
})

describe('compile-time syntax', () => {
  const examples = [
    ['bash', 'curl -X POST "https://example.invalid"\n'],
    ['typescript', 'const answer: number = 42; // comment\n'],
    ['javascript', 'function answer() { return "yes" }\n'],
    ['python', 'def answer():\n    return "yes" # comment\n'],
    ['go', 'package main\nfunc main() { println("yes") }\n'],
    ['json', '{"answer": true, "count": 42}\n'],
    ['yaml', 'answer: true\ncount: 42 # comment\n'],
    ['starlark', 'answer = {"value": True}\n'],
  ]
  it.each(examples)('renders %s token nodes without changing source', async (language, source) => {
    const document = await compile('```' + language + '\n' + source + '```')
    expect(document.querySelector('pre code')?.textContent).toBe(source)
    expect(document.querySelectorAll('pre .token').length).toBeGreaterThan(1)
    expect(document.querySelector('pre')?.getAttribute('data-language')).toBe(language)
  })
  it.each(['unknown-language', 'constructor', 'toString', 'text', ''])('escapes %s code and preserves Unicode/whitespace', async (language) => {
    const source = '<script>alert("&")</script>\n  café 日本語 \n\tend\n'
    const document = await compile('```' + language + '\n' + source + '```')
    expect(document.querySelector('pre code')?.textContent).toBe(source)
    expect(document.querySelector('pre script')).toBeNull()
    expect(document.querySelector('pre .token')).toBeNull()
  })
  it('compiles nested fenced tabs and keeps both readable without JavaScript', async () => {
    const document = await compile('<CodeTabs>\n<CodeTab label="CLI">\n\n```bash\necho "hello"\n```\n\n</CodeTab>\n<CodeTab label="JSON">\n\n```json\n{"hello": true}\n```\n\n</CodeTab>\n</CodeTabs>')
    expect(document.querySelectorAll('pre')).toHaveLength(2)
    expect(document.querySelectorAll('pre .token').length).toBeGreaterThan(2)
    expect(document.body.textContent).toContain('CLI')
    expect(document.body.textContent).toContain('JSON')
  })
})

it('requires a deliberate HTTPS publication origin', () => {
  expect(siteUrl(undefined)).toBe('')
  expect(siteUrl('https://inference.net/whipcode/')).toBe('https://inference.net/whipcode')
  expect(siteUrl('https://docs.example.test/')).toBe('https://docs.example.test')
  for (const value of ['http://example.test', 'https://user:secret@example.test', 'https://example.test?key=secret', 'https://example.test/Bad_Path', 'https://example.test/a//b', 'https://example.test/-bad']) expect(() => siteUrl(value)).toThrow()
})
