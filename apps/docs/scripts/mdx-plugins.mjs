import mdx from '@mdx-js/rollup'
import GithubSlugger from 'github-slugger'
import { toString } from 'mdast-util-to-string'
import { visit } from 'unist-util-visit'
import remarkGfm from 'remark-gfm'
import remarkFrontmatter from 'remark-frontmatter'
import { refractor } from 'refractor/core'
import bash from 'refractor/bash'
import javascript from 'refractor/javascript'
import typescript from 'refractor/typescript'
import python from 'refractor/python'
import go from 'refractor/go'
import json from 'refractor/json'
import yaml from 'refractor/yaml'

for (const grammar of [bash, javascript, typescript, python, go, json, yaml]) refractor.register(grammar)
export const languageAliases = { bash: 'bash', sh: 'bash', shell: 'bash', curl: 'bash', javascript: 'javascript', js: 'javascript', typescript: 'typescript', ts: 'typescript', python: 'python', py: 'python', starlark: 'python', go: 'go', golang: 'go', bzl: 'python', json: 'json', yaml: 'yaml', yml: 'yaml', text: '', txt: '', plaintext: '' }

// One AST transform owns both TOC metadata and rendered heading IDs.
export function remarkHeadings() {
  return (tree, file) => {
    const slugger = new GithubSlugger()
    const headings = []
    visit(tree, 'heading', (node) => {
      const text = toString(node)
      const id = slugger.slug(text)
      node.data = { ...node.data, hProperties: { ...node.data?.hProperties, id } }
      if (node.depth === 2 || node.depth === 3) headings.push({ id, text, level: node.depth })
    })
    file.data.headings = headings
  }
}

export function rehypeCode() {
  return (tree) => {
    visit(tree, 'element', (node) => {
      if (node.tagName !== 'pre') return
      const code = node.children[0]
      if (code?.type !== 'element' || code.tagName !== 'code') return
      const raw = code.children.map((child) => child.type === 'text' ? child.value : '').join('')
      const classes = code.properties.className ?? []
      const label = classes.find((name) => name.startsWith('language-'))?.slice(9) ?? ''
      const language = Object.hasOwn(languageAliases, label) ? languageAliases[label] : ''
      node.properties['data-raw'] = raw
      node.properties['data-language'] = label || 'text'
      if (language) code.children = refractor.highlight(raw, language).children
    })
  }
}

export const mdxOptions = {
  remarkPlugins: [remarkGfm, remarkFrontmatter, remarkHeadings],
  rehypePlugins: [rehypeCode],
}

export function docsMdx() {
  const plugin = mdx(mdxOptions)
  const transform = plugin.transform
  // @mdx-js/rollup strips queries before filtering, so exclude cannot protect ?raw.
  plugin.transform = function (source, id) {
    if (/[?&]raw(?:&|$)/.test(id)) return
    return transform.call(this, source, id)
  }
  return plugin
}
