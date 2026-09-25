import { readFile, readdir, mkdir, writeFile } from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { unified } from 'unified'
import remarkParse from 'remark-parse'
import remarkMdx from 'remark-mdx'
import remarkFrontmatter from 'remark-frontmatter'
import remarkGfm from 'remark-gfm'
import { visit } from 'unist-util-visit'
import { parse as parseYaml } from 'yaml'
import { z } from 'zod'
import { docSections } from '../src/features/docs/content/sections.ts'
import { remarkHeadings, languageAliases } from './mdx-plugins.mjs'

export const appRoot = fileURLToPath(new URL('../', import.meta.url))
export const contentRoot = path.join(appRoot, 'src/content/docs')
export const generatedFile = path.join(appRoot, 'src/features/docs/content/manifest.gen.ts')
const sections = docSections.map(section => section.id)
const metadataSchema = z.object({
  title: z.string().trim().min(1), navTitle: z.string().trim().min(1).optional(), description: z.string().trim().min(1),
  section: z.enum(sections), order: z.number().int().positive(),
}).strict()

export function parseDocument(source, filename) {
  const processor = unified().use(remarkParse).use(remarkMdx).use(remarkGfm).use(remarkFrontmatter).use(remarkHeadings)
  const file = { value: source, path: filename, data: {} }
  const tree = processor.runSync(processor.parse(file), file)
  const frontmatter = tree.children[0]
  if (frontmatter?.type !== 'yaml') throw new Error(`${filename}: required YAML frontmatter is missing`)
  let metadata
  try { metadata = metadataSchema.parse(parseYaml(frontmatter.value)) }
  catch (error) { throw new Error(`${filename}: invalid frontmatter: ${error.message}`) }
  const links = []
  const warnings = []
  visit(tree, (node) => {
    if (node.type === 'heading' && node.depth === 1) throw new Error(`${filename}: H1 comes from the title; use H2/H3 in articles`)
    if (node.type === 'code' && node.lang && !Object.hasOwn(languageAliases, node.lang)) warnings.push(`${filename}: unsupported code language "${node.lang}"; rendered as plain text`)
    if (node.type === 'link' || node.type === 'definition') links.push(node.url)
    if (node.type === 'mdxJsxFlowElement' || node.type === 'mdxJsxTextElement') {
      for (const attribute of node.attributes ?? []) {
        if (attribute.name === 'href' && typeof attribute.value === 'string') links.push(attribute.value)
      }
    }
  })
  return { ...metadata, headings: file.data.headings, links, warnings }
}

async function discover(directory) {
  const entries = await readdir(directory, { withFileTypes: true })
  const paths = await Promise.all(entries.map(async (entry) => {
    const full = path.join(directory, entry.name)
    if (entry.isSymbolicLink()) throw new Error(`${full}: content symlinks are not supported`)
    if (entry.isDirectory()) return discover(full)
    if (entry.name.endsWith('.mdx') && entry.name !== 'index.mdx') throw new Error(`${full}: articles must be named index.mdx`)
    return entry.name === 'index.mdx' ? [full] : []
  }))
  return paths.flat().sort()
}

export function validateDocuments(docs) {
  const paths = new Set()
  const orders = new Set()
  for (const doc of docs) {
    if (paths.has(doc.path)) throw new Error(`Duplicate documentation path: ${doc.path}`)
    paths.add(doc.path)
    const order = `${doc.section}:${doc.order}`
    if (orders.has(order)) throw new Error(`${doc.path}: duplicate order ${doc.order} in section ${doc.section}`)
    orders.add(order)
  }
  const known = new Map(docs.map((doc) => [`/docs/${doc.path}`, doc]))
  for (const doc of docs) for (const href of doc.links ?? []) {
    if (/^(?:https?:|mailto:)/.test(href)) continue
    if (href.startsWith('//')) throw new Error(`${doc.path}: protocol-relative links are not supported: ${href}`)
    if (!href.startsWith('/') && !href.startsWith('#')) throw new Error(`${doc.path}: use root-relative internal links: ${href}`)
    const url = new URL(href, `https://docs.invalid/docs/${doc.path}`)
    if (url.origin !== 'https://docs.invalid') throw new Error(`${doc.path}: unsupported link ${href}`)
    if (url.pathname !== '/' && url.pathname.endsWith('/')) throw new Error(`${doc.path}: internal links must omit trailing slashes: ${href}`)
    const target = known.get(url.pathname)
    if (!target && !['/', '/docs'].includes(url.pathname)) throw new Error(`${doc.path}: broken internal link ${href}`)
    if (url.hash && (!target || !target.headings.some((heading) => heading.id === decodeURIComponent(url.hash.slice(1))))) throw new Error(`${doc.path}: missing heading target ${href}`)
  }
}

export async function loadDocuments(root = contentRoot) {
  const docs = []
  for (const filename of await discover(root)) {
    const slug = path.relative(root, path.dirname(filename)).split(path.sep).join('/')
    if (!/^[a-z0-9]+(?:-[a-z0-9]+)*(?:[/][a-z0-9]+(?:-[a-z0-9]+)*)*$/.test(slug)) throw new Error(`${filename}: invalid documentation path`)
    docs.push({ path: slug, ...parseDocument(await readFile(filename, 'utf8'), filename) })
  }
  if (!docs.length) throw new Error(`${root}: no documentation articles found`)
  validateDocuments(docs)
  return docs.sort((a, b) => sections.indexOf(a.section) - sections.indexOf(b.section) || a.order - b.order)
}

export async function generateManifest() {
  const documents = await loadDocuments()
  for (const document of documents) for (const warning of document.warnings) console.warn(warning)
  const metadata = documents.map(({ links, warnings, ...doc }) => doc)
  const source = `// Generated from src/content/docs. Do not edit.
import type { DocMeta } from './types'
export const docsManifest = ${JSON.stringify(metadata, null, 2)} satisfies DocMeta[]
`
  await mkdir(path.dirname(generatedFile), { recursive: true })
  const previous = await readFile(generatedFile, 'utf8').catch((error) => { if (error.code === 'ENOENT') return ''; throw error })
  if (previous !== source) await writeFile(generatedFile, source)
  return metadata
}
