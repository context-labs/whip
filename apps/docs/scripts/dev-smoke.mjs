import { docRedirects } from '../src/features/docs/content/redirects.ts'
import assert from 'node:assert/strict'
import { spawn } from 'node:child_process'
import { mkdir, writeFile, rm, readFile } from 'node:fs/promises'
import { setTimeout as sleep } from 'node:timers/promises'
import { once } from 'node:events'
import path from 'node:path'
import { appRoot, contentRoot, generatedFile, generateManifest } from './content.mjs'

const directory = path.join(contentRoot, 'dev-smoke-proof')
const url = 'http://127.0.0.1:3102'
let output = ''
let ownsDirectory = false
const child = spawn(process.execPath, [path.join(appRoot, '../../node_modules/vite/bin/vite.js'), '--host', '127.0.0.1', '--port', '3102', '--strictPort'], { cwd: appRoot, stdio: ['ignore', 'pipe', 'pipe'] })
child.stdout.on('data', (chunk) => { output = (output + chunk).slice(-10000) })
child.stderr.on('data', (chunk) => { output = (output + chunk).slice(-10000) })
async function until(check, label) {
  for (let attempt = 0; attempt < 100; attempt++) {
    if (child.exitCode !== null) throw new Error(`Dev server exited: ${output}`)
    try { if (await check()) return } catch { /* Wait for startup or regeneration. */ }
    await sleep(100)
  }
  throw new Error(`Timed out: ${label}\n${output}`)
}
const source = (title) => `---
title: ${title}
description: Temporary live-watch integration proof.
section: configuration
order: 99
---

## Live heading

Temporary test article.
`
try {
  await until(async () => (await fetch(url)).ok, 'dev startup')
  const redirect = await fetch(url, { redirect: 'manual' })
  assert.equal(redirect.status, 308)
  assert.equal(redirect.headers.get('location'), '/docs/quickstart')
  for (const [from, to] of Object.entries(docRedirects)) {
    const alias = await fetch(url + from + '?check=yes', { redirect: 'manual' })
    assert.equal(alias.status, 308, from)
    assert.equal(alias.headers.get('location'), to + '?check=yes')
  }
  // Never overwrite an authored page if the reserved test name was used.
  await mkdir(directory)
  ownsDirectory = true
  await writeFile(path.join(directory, 'index.mdx'), source('Live addition'))
  await until(async () => (await readFile(generatedFile, 'utf8')).includes('Live addition'), 'metadata addition')
  await until(async () => (await (await fetch(url + '/docs/dev-smoke-proof')).text()).includes('Live addition'), 'new article SSR')
  await writeFile(path.join(directory, 'index.mdx'), source('Live edit'))
  await until(async () => (await readFile(generatedFile, 'utf8')).includes('Live edit'), 'metadata edit')
  assert((await (await fetch(url + '/docs/dev-smoke-proof')).text()).includes('Live edit'))
  await rm(directory, { recursive: true })
  await until(async () => !(await readFile(generatedFile, 'utf8')).includes('dev-smoke-proof'), 'metadata deletion')
  await until(async () => (await fetch(url + '/docs/dev-smoke-proof')).status === 404, 'deleted route 404')
  console.log('Live dev add/edit/delete and route refresh passed.')
} finally {
  child.kill('SIGTERM')
  if (child.exitCode === null) await once(child, 'exit')
  if (ownsDirectory) await rm(directory, { recursive: true, force: true })
  await generateManifest()
}
