// @vitest-environment node
import { expect, it } from 'vitest'
import { mkdtempSync, writeFileSync, readFileSync, rmSync } from 'node:fs'
import { spawnSync } from 'node:child_process'
import { tmpdir } from 'node:os'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { environments } from '../scripts/deployment.mjs'

it('deploys the exact uploaded version and fails closed before wrong-target, source or ambiguous activation', () => {
  const directory = mkdtempSync(path.join(tmpdir(), 'docs-deploy-test-'))
  const source = 'a'.repeat(40)
  const version = '11111111-2222-3333-4444-555555555555'
  try {
    writeFileSync(path.join(directory, 'wrangler'), `#!${process.execPath}
const fs = require('fs');
fs.appendFileSync('calls.jsonl', JSON.stringify(process.argv.slice(2)) + '\\n');
if (process.env.FAIL_UPLOAD === 'true' || (process.env.FAIL_DEPLOY === 'true' && process.argv[3] === 'deploy')) process.exit(1);
if (process.argv[3] === 'upload') fs.writeFileSync(process.env.WRANGLER_OUTPUT_FILE_PATH, JSON.stringify({type:'version-upload', worker_name:process.env.WORKER, version_id:process.env.VERSION}) + '\\n');
if (process.argv[3] === 'upload' && process.env.DUPLICATE === 'true') fs.appendFileSync(process.env.WRANGLER_OUTPUT_FILE_PATH, fs.readFileSync(process.env.WRANGLER_OUTPUT_FILE_PATH));
`, { mode: 0o755 })
    for (const [target, site] of Object.entries(environments)) {
      for (const failure of ['', 'source', 'worker', 'routes', 'version', 'upload', 'activate', 'duplicate']) {
        const config = { account_id: 'b0c6d69f10b59b4d5b3a1a6d882d95ba', name: site.worker,
          main: 'worker.js', find_additional_modules: false, assets: { directory: './public' }, vars: { DOCS_INDEXABLE: String(site.indexable) },
          ...(failure === 'routes' ? { routes: [] } : {}),
        }
        writeFileSync(path.join(directory, 'wrangler.json'), JSON.stringify(config))
        writeFileSync(path.join(directory, 'source.json'), JSON.stringify({ target, source: failure === 'source' ? 'b'.repeat(40) : source }))
        writeFileSync(path.join(directory, 'calls.jsonl'), '')
        const result = spawnSync(process.execPath, [fileURLToPath(new URL('../scripts/deploy.mjs', import.meta.url))], { cwd: directory, encoding: 'utf8', env: {
          PATH: directory + ':/usr/bin:/bin', HOME: directory, WHIPCODE_HOME: directory, TMPDIR: directory,
          DOCS_ENVIRONMENT: target, DOCS_SOURCE_SHA: source, WORKER: failure === 'worker' ? 'other-worker' : site.worker,
          VERSION: failure === 'version' ? '' : version, FAIL_UPLOAD: String(failure === 'upload'), FAIL_DEPLOY: String(failure === 'activate'), DUPLICATE: String(failure === 'duplicate'),
        } })
        expect(result.status === 0, result.stderr).toBe(!failure)
        const calls = readFileSync(path.join(directory, 'calls.jsonl'), 'utf8').trim().split('\n').filter(Boolean).map(line => JSON.parse(line))
        expect(calls.length).toBe(!failure || failure === 'activate' ? 2 : ['source', 'routes'].includes(failure) ? 0 : 1)
        if (!failure) {
          expect(calls[0].slice(0, 3)).toEqual(['versions', 'upload', '--no-bundle'])
          expect(calls[1].slice(0, 4)).toEqual(['versions', 'deploy', version + '@100%', '--yes'])
          expect(result.stdout).toContain(site.origin)
          expect(result.stdout).toContain(source)
        }
      }
    }
  } finally { rmSync(directory, { recursive: true, force: true }) }
})
