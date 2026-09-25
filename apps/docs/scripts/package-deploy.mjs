import assert from 'node:assert/strict'
import { execFileSync } from 'node:child_process'
import { cp, mkdir, readFile, rm, writeFile } from 'node:fs/promises'
import { environments } from './deployment.mjs'

const target = process.env.DOCS_ENVIRONMENT
assert(Object.hasOwn(environments, target), 'Set DOCS_ENVIRONMENT to production or preview')
// Run from apps/docs. Nothing from dist/server or Storybook enters this package.
execFileSync('node', ['scripts/verify-static.mjs'], { stdio: 'inherit' })
await rm('dist/deploy', { recursive: true, force: true })
await mkdir('dist/deploy', { recursive: true })
execFileSync('wrangler', ['versions', 'upload', '--env', target, '--dry-run', '--outdir', 'dist/deploy'], { stdio: 'inherit' })
await rm('dist/deploy/worker.js.map', { force: true })
await rm('dist/deploy/README.md', { force: true })
await cp('dist/client', 'dist/deploy/public', { recursive: true })
const source = process.env.DOCS_SOURCE_SHA || execFileSync('git', ['rev-parse', 'HEAD'], { encoding: 'utf8' }).trim()
await writeFile('dist/deploy/source.json', JSON.stringify({ target, source }) + '\n')
const config = JSON.parse(await readFile('wrangler.jsonc', 'utf8'))
await writeFile('dist/deploy/wrangler.json', JSON.stringify({
  account_id: config.account_id, name: environments[target].worker,
  main: 'worker.js', find_additional_modules: false, compatibility_date: config.compatibility_date,
  workers_dev: false, preview_urls: false,
  vars: config.env[target].vars,
  assets: { ...config.assets, directory: './public' },
}, null, 2) + '\n')
