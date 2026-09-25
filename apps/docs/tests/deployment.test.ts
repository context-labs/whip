// @vitest-environment node
import { it, expect, vi } from 'vitest'
import { readFileSync, mkdtempSync, writeFileSync, rmSync } from 'node:fs'
import { execFileSync, spawnSync } from 'node:child_process'
import { tmpdir } from 'node:os'
import path from 'node:path'
import { parse } from 'yaml'
import { docsSite, environments } from '../scripts/deployment.mjs'
import { robotsText } from '../scripts/static-policy.mjs'
import { pageHead } from '../src/features/docs/content/head'
import worker from '../worker'

it('requires explicit approved indexing and rejects mismatched target URLs', () => {
  expect(docsSite({})).toEqual({ origin: '', indexable: false })
  expect(docsSite({ DOCS_SITE_URL: 'https://docs.example.test' }).indexable).toBe(false)
  for (const target of ['production', 'preview'] as const) {
    expect(docsSite({ DOCS_ENVIRONMENT: target })).toEqual(environments[target])
    expect(() => docsSite({ DOCS_ENVIRONMENT: target, DOCS_SITE_URL: 'https://wrong.test' })).toThrow()
  }
  for (const target of ['main', 'development', 'toString', 'bad']) expect(() => docsSite({ DOCS_ENVIRONMENT: target })).toThrow()
  expect(robotsText(environments.preview.origin)).toBe('User-agent: *\nDisallow: /\n')
})

it('keeps preview canonical URLs separate from permission to index', () => {
  for (const site of Object.values(environments)) {
    vi.stubGlobal('__DOCS_SITE_URL__', site.origin)
    vi.stubGlobal('__DOCS_INDEXABLE__', site.indexable)
    try {
      const head = pageHead('Test', 'Description', '/docs/quickstart')
      expect(head.links).toEqual([{ rel: 'canonical', href: site.origin + '/docs/quickstart' }])
      expect(head.meta).toContainEqual({ name: 'robots', content: site.indexable ? 'index,follow' : 'noindex,nofollow' })
      expect(pageHead('Missing', 'Description', '/404').meta).toContainEqual({ name: 'robots', content: 'noindex,nofollow' })
      expect(pageHead('Missing', 'Description', '/404').links).toEqual([])
    } finally { vi.unstubAllGlobals() }
  }
})

it('applies preview noindex to HTML, assets, redirects and errors, defaulting closed', async () => {
  for (const value of [undefined, 'false', 'true']) {
    for (const route of ['', '/docs/quickstart', '/missing', '/assets/test.js']) {
      const response = await worker.fetch(new Request('https://inference.cool/whipcode' + route), {
        DOCS_INDEXABLE: value, ASSETS: { fetch: async () => new Response('test') },
      })
      expect(response.headers.get('x-robots-tag')).toBe(value === 'true' ? null : 'noindex, nofollow')
    }
  }
})

const config = JSON.parse(readFileSync(new URL('../wrangler.jsonc', import.meta.url), 'utf8'))
const workflow = parse(readFileSync(new URL('../../../.github/workflows/docs-deploy.yml', import.meta.url), 'utf8'))
it('pins the two Worker targets and only exact/subtree routes without changing root ownership', () => {
  expect(config.workers_dev).toBe(false)
  expect(config.preview_urls).toBe(false)
  for (const [target, site] of Object.entries(environments)) {
    expect(config.env[target].name).toBe(site.worker)
    expect(config.env[target].vars.DOCS_INDEXABLE).toBe(String(site.indexable))
    const host = new URL(site.origin).host
    expect(config.env[target].routes).toEqual([
      { pattern: host + '/whipcode', zone_name: host },
      { pattern: host + '/whipcode/*', zone_name: host },
    ])
  }
})

it('isolates direct trusted branch deploys from PRs, called release CI and build credentials', () => {
  expect(Object.keys(workflow.on).sort()).toEqual(['push', 'workflow_dispatch'])
  expect(workflow.on.push.branches).toEqual(['main', 'development'])
  expect(workflow.jobs.build.if).toContain("github.repository == 'context-labs/whip'")
  expect(workflow.jobs.build.if).toContain("github.ref == 'refs/heads/main' || github.ref == 'refs/heads/development'")
  expect(workflow.jobs.build.if).toContain("github.event_name == 'push' || github.event_name == 'workflow_dispatch'")
  expect(workflow.concurrency['cancel-in-progress']).toBe(false)
  expect(workflow.jobs.deploy.needs).toBe('build')
  expect(workflow.jobs.deploy.environment).toBe("${{ github.ref == 'refs/heads/main' && 'docs-production' || 'docs-preview' }}")
  expect(JSON.stringify(workflow.jobs.build)).not.toContain('secrets.')
  expect(JSON.stringify(workflow.jobs.verify)).not.toContain('secrets.')
  for (const job of Object.values(workflow.jobs) as any[]) {
    for (const step of job.steps) {
      if (step.uses?.startsWith('actions/checkout@')) expect(step.with.ref).toBe('${{ github.sha }}')
      if (JSON.stringify(step).includes('secrets.')) expect(step.name).toBe('Upload and activate only this Worker version')
    }
  }
})

it('runs the actual stale-source shell under both Bash 3.2 and modern Bash with fail-closed API errors', () => {
  const directory = mkdtempSync(path.join(tmpdir(), 'docs-freshness-'))
  try {
    writeFileSync(path.join(directory, 'gh'), '#!/bin/sh\n[ "$FAIL" != 1 ] || exit 1\nprintf "%s" "$HEAD_SHA"\n', { mode: 0o755 })
    const command = workflow.jobs.deploy.steps.find((step: any) => step.id === 'current').run
    const shells = [...new Set(['/bin/bash', execFileSync('which', ['bash'], { encoding: 'utf8' }).trim()])]
    for (const bash of shells) for (const [head, fail] of [['abc', '0'], ['other', '0'], ['abc', '1']]) {
      const output = path.join(directory, 'output')
      writeFileSync(output, '')
      const result = spawnSync(bash, ['-euo', 'pipefail', '-c', command], { env: {
        PATH: directory + ':/usr/bin:/bin', HOME: directory, WHIPCODE_HOME: directory,
        HEAD_SHA: head, FAIL: fail, DOCS_SOURCE_SHA: 'abc', GITHUB_OUTPUT: output,
        GITHUB_STEP_SUMMARY: path.join(directory, 'summary'), GITHUB_REPOSITORY: 'context-labs/whip', SOURCE_BRANCH: 'main',
      } })
      expect(result.status === 0).toBe(fail !== '1')
      expect(readFileSync(output, 'utf8')).toBe(head === 'abc' && fail !== '1' ? 'current=true\n' : '')
    }
  } finally { rmSync(directory, { recursive: true, force: true }) }
})
