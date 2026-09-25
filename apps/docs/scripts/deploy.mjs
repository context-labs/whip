import assert from 'node:assert/strict'
import { execFileSync } from 'node:child_process'
import { readFileSync, mkdtempSync, rmSync, appendFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import path from 'node:path'
import { environments } from './deployment.mjs'

// Activate only an explicitly named uploaded version. Routes are operator-managed.
const target = process.env.DOCS_ENVIRONMENT
assert(Object.hasOwn(environments, target), 'DOCS_ENVIRONMENT must be production or preview')
const site = environments[target]
const sha = process.env.DOCS_SOURCE_SHA
assert(/^[a-f0-9]{40}$/.test(sha), 'DOCS_SOURCE_SHA must identify the tested source')
const directory = mkdtempSync(path.join(tmpdir(), 'docs-version-'))
const output = path.join(directory, 'wrangler.jsonl')
const run = (...args) => execFileSync('wrangler', [...args, '--config', 'wrangler.json'], { stdio: 'inherit', env: { ...process.env, WRANGLER_OUTPUT_FILE_PATH: output } })
try {
  assert.deepEqual(JSON.parse(readFileSync('source.json', 'utf8')), { target, source: sha }, 'Package must match tested source and environment')
  const config = JSON.parse(readFileSync('wrangler.json', 'utf8'))
  assert.equal(config.name, site.worker)
  assert.equal(config.account_id, 'b0c6d69f10b59b4d5b3a1a6d882d95ba')
  assert.equal(config.find_additional_modules, false)
  assert.equal(config.main, 'worker.js')
  assert.equal(config.assets.directory, './public')
  assert.equal(config.vars.DOCS_INDEXABLE, String(site.indexable))
  assert(!('routes' in config) && !('route' in config), 'CI must not mutate routes')
  run('versions', 'upload', '--no-bundle', '--tag', sha.slice(0, 12), '--message', `docs ${target} ${sha}`)
  const records = readFileSync(output, 'utf8').trim().split('\n').map(line => JSON.parse(line))
  const uploads = records.filter(record => record.type === 'version-upload')
  assert.equal(uploads.length, 1)
  const version = uploads[0]
  assert.equal(version.worker_name, site.worker)
  assert(/^[a-f0-9-]{36}$/.test(version.version_id), 'Missing uploaded version ID; inspect before retrying')
  run('versions', 'deploy', `${version.version_id}@100%`, '--yes', '--message', `docs ${target} ${sha}`)
  const receipt = `${site.origin} — ${site.worker} — version ${version.version_id} — source ${sha}\n`
  console.log(receipt)
  if (process.env.GITHUB_STEP_SUMMARY) appendFileSync(process.env.GITHUB_STEP_SUMMARY, receipt)
} finally { rmSync(directory, { recursive: true, force: true }) }
