import { docRedirects } from '../src/features/docs/content/redirects.ts'
import assert from 'node:assert/strict'
import { readFile, writeFile, readdir, mkdir } from 'node:fs/promises'
import path from 'node:path'
import { JSDOM } from 'jsdom'
import { appRoot, loadDocuments } from './content.mjs'
import { docsSite } from './deployment.mjs'
import { robotsText, runtimeHighlighter } from './static-policy.mjs'

const root = path.join(appRoot, 'dist/client')
const { origin, indexable: published } = docsSite()
const base = origin ? new URL(origin).pathname.replace(/\/$/, '') : ''
const publicPath = (url) => base + url
const docs = await loadDocuments()
const urls = [...docs.map((doc) => `/docs/${doc.path}`), '/404']
const rendered = new Map()
// Static-only hosts can follow legacy URLs without JavaScript or server rules.
for (const [from, to] of Object.entries(docRedirects)) {
  const html = `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>Redirecting — whipcode</title><meta name="robots" content="noindex,nofollow">
<meta http-equiv="refresh" content="0; url=${publicPath(to)}"></head>
<body><main><a href="${publicPath(to)}">Continue to documentation</a></main></body></html>`
  const filename = path.join(root, from === '/' ? 'index.html' : `${from.slice(1)}/index.html`)
  await mkdir(path.dirname(filename), { recursive: true })
  await writeFile(filename, html)
  rendered.set(from, new JSDOM(html).window.document)
}
async function htmlFor(url) {
  const relative = url === '/' ? 'index.html' : `${url.slice(1)}/index.html`
  return readFile(path.join(root, relative), 'utf8').catch(() => readFile(path.join(root, `${url.slice(1)}.html`), 'utf8'))
}
const html404 = await htmlFor('/404')
await writeFile(path.join(root, '404.html'), html404)
await writeFile(path.join(root, 'robots.txt'), robotsText(origin, published))
for (const url of urls) {
  const html = await htmlFor(url)
  const document = new JSDOM(html).window.document
  rendered.set(url, document)
  assert.equal(document.querySelectorAll('main').length, 1, `${url}: expected one main landmark`)
  assert.equal(document.querySelectorAll('h1').length, 1, `${url}: expected one H1`)
  assert(document.title.includes('whipcode'), `${url}: missing title`)
  assert(document.querySelector('meta[name="description"]'), `${url}: missing description`)
  const indexable = published && url !== '/404'
  assert.equal(document.querySelector('meta[name="robots"]')?.getAttribute('content'), indexable ? 'index,follow' : 'noindex,nofollow', `${url}: wrong indexing policy`)
  assert.equal(document.querySelector('link[rel="canonical"]')?.getAttribute('href'), origin && url !== '/404' ? `${origin}${url}` : undefined, `${url}: wrong canonical URL`)

  const doc = docs.find((entry) => url === `/docs/${entry.path}`)
  if (doc) {
    assert.equal(document.querySelector('h1')?.textContent, doc.title)
    for (const heading of doc.headings) assert.equal(document.getElementById(heading.id)?.textContent, heading.text, `${url}: missing heading ${heading.id}`)
  }
  for (const element of document.querySelectorAll('script[src],link[href]')) {
    const value = element.getAttribute('src') ?? element.getAttribute('href')
    if (element.getAttribute('rel') === 'canonical') continue
    assert(value.startsWith('/'), `${url}: asset must be local and root-relative: ${value}`)
    assert(!value.startsWith('//'), `${url}: remote asset ${value}`)
    assert(value.startsWith(base + '/'), `${url}: asset escaped base path: ${value}`)
    const asset = path.resolve(root, value.slice(base.length + 1).split('?')[0])
    assert(asset.startsWith(root + path.sep), `${url}: asset outside public output`)
    await readFile(asset)

  }
}
for (const [url, document] of rendered) {
  for (const link of document.querySelectorAll('a[href]')) {
    const href = link.getAttribute('href')
    if (/^(https?:|mailto:)/.test(href)) continue
    const target = new URL(href, `https://docs.invalid${publicPath(url)}`)
    assert.equal(target.origin, 'https://docs.invalid', `${url}: unsupported link ${href}`)
    assert(target.pathname.startsWith(base + '/'), `${url}: link escaped base path: ${href}`)
    const page = rendered.get(target.pathname.slice(base.length))
    assert(page, `${url}: broken internal link ${href}`)
    if (target.hash) assert(page.getElementById(decodeURIComponent(target.hash.slice(1))), `${url}: missing fragment ${href}`)
  }
}
async function files(directory) {
  const entries = await readdir(directory, { withFileTypes: true })
  return (await Promise.all(entries.map((entry) => entry.isDirectory() ? files(path.join(directory, entry.name)) : [path.join(directory, entry.name)]))).flat()
}
const emitted = await files(root)
assert.equal(await readFile(path.join(root, 'robots.txt'), 'utf8'), robotsText(origin, published))
if (published) {
  const sitemap = new JSDOM(await readFile(path.join(root, 'sitemap.xml'), 'utf8'), { contentType: 'text/xml' }).window.document
  assert.deepEqual([...sitemap.querySelectorAll('loc')].map((node) => node.textContent).sort(), urls.filter((url) => url !== '/404').map((url) => `${origin}${url}`).sort())
} else assert(!emitted.some((file) => file.endsWith('/sitemap.xml')), 'Preview build must not emit a sitemap')

for (const filename of emitted.filter((name) => name.endsWith('.js'))) {
  const source = await readFile(filename, 'utf8')
  assert(!runtimeHighlighter.test(source), `${filename}: browser runtime grammar/highlighter leaked`)
}
// Compiler and Storybook fixtures test syntax highlighting; outline pages need no code fences.
console.log(`Verified ${urls.length} prerendered documents and ${emitted.length} public files; deploy only dist/client.`)
