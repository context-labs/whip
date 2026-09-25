// @vitest-environment node
import { afterEach, expect, it, vi } from 'vitest'
import ts from 'typescript'
import { mkdtemp, mkdir, readFile, rm, writeFile } from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { unified } from 'unified'
import remarkParse from 'remark-parse'
import remarkMdx from 'remark-mdx'
import remarkFrontmatter from 'remark-frontmatter'
import { visit } from 'unist-util-visit'

afterEach(() => vi.restoreAllMocks())

const app = fileURLToPath(new URL('../', import.meta.url))
const workspace = path.resolve(app, '../..')
const article = await readFile(path.join(app, 'src/content/docs/typescript-sdk/index.mdx'), 'utf8')
const examples = new Map<string, string>()
const tree = unified().use(remarkParse).use(remarkMdx).use(remarkFrontmatter).parse(article)
visit(tree, 'code', (node: any) => {
  if (node.lang !== 'typescript') return
  const name = /^file=([a-z-]+\.mts)$/.exec(node.meta ?? '')?.[1]
  if (!name || examples.has(name)) throw new Error('Each SDK TypeScript example needs a unique file=*.mts name')
  examples.set(name, node.value)
})

it('typechecks every published SDK example against the actual source API', async () => {
  expect(examples.size).toBe(10)
  const output = path.join(app, 'test-results')
  await mkdir(output, { recursive: true })
  const directory = await mkdtemp(path.join(output, 'sdk-examples-'))
  try {
    for (const [name, source] of examples) await writeFile(path.join(directory, name), source)
    const program = ts.createProgram([...examples.keys()].map(name => path.join(directory, name)), {
      target: ts.ScriptTarget.ES2022,
      module: ts.ModuleKind.NodeNext,
      moduleResolution: ts.ModuleResolutionKind.NodeNext,
      strict: true, noUncheckedIndexedAccess: true, noEmit: true, skipLibCheck: true, allowImportingTsExtensions: true,
      types: ['node'],
      baseUrl: workspace,
      paths: {
        '@whip/sdk': ['packages/sdk/src/index.ts'],
        '@whip/sdk/*': ['packages/sdk/src/*.ts'],
        '@whip/protocol': ['packages/protocol/generated/index.d.ts'],
      },
    })
    const diagnostics = ts.getPreEmitDiagnostics(program)
    expect(ts.formatDiagnostics(diagnostics, {
      getCurrentDirectory: () => workspace,
      getCanonicalFileName: name => name,
      getNewLine: () => '\n',
    })).toBe('')
  } finally { await rm(directory, { recursive: true, force: true }) }
}, 20000)

async function helper(name: string) {
  const code = ts.transpileModule(examples.get(name)!, {
    compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ESNext },
  }).outputText
  // These helpers have only type imports: tests execute them against local doubles,
  // never connecting to a host or making a model request.
  return import(/* @vite-ignore */ `data:text/javascript;base64,${Buffer.from(code).toString('base64')}`)
}

it('stream example separates child output, resets discarded drafts and returns the authoritative result', async () => {
  const log = vi.spyOn(console, 'log').mockImplementation(() => {})
  const { streamTask } = await helper('stream.mts')
  let submitted: unknown
  const events = [
    { type: 'text', agentId: 'root', delta: 'old draft' },
    { type: 'text', agentId: 'child', delta: 'do not merge me' },
    { type: 'discard', agentId: 'root', discarded: 9 },
    { type: 'text', agentId: 'root', delta: 'new draft' },
  ]
  const turn = {
    async *[Symbol.asyncIterator]() { yield* events },
    result: async () => ({ status: 'succeeded', text: 'final answer' }),
  }
  const session = { rootId: 'root', run: (prompt: string, options: unknown) => { submitted = { prompt, options }; return turn } }
  expect(await streamTask(session, 'Explain')).toBe('final answer')
  expect(submitted).toEqual({ prompt: 'Explain', options: { includeChildren: true } })
  expect(log.mock.calls).toEqual([['Draft:', 'old draft'], ['Draft:', ''], ['Draft:', 'new draft']])
  log.mockRestore()
})

it('attachment example uploads to the session before submitting the reference', async () => {
  const { summarizeNotes } = await helper('attachment.mts')
  const calls: string[] = []
  const session = {
    rootId: 'root',
    client: { upload: async (bytes: Uint8Array, options: unknown) => {
      calls.push('upload')
      expect(new TextDecoder().decode(bytes)).toBe('Notes')
      expect(options).toEqual({ rootId: 'root', mediaType: 'text/plain' })
      return { asAttachment: (kind: string, filename: string) => ({ kind, filename, handle: 'content-id' }) }
    } },
    run: (input: any) => {
      calls.push('run')
      expect(input.attachments).toEqual([{ kind: 'text', filename: 'notes.txt', handle: 'content-id' }])
      return { text: async () => 'summary' }
    },
  }
  expect(await summarizeNotes(session, 'Notes')).toBe('summary')
  expect(calls).toEqual(['upload', 'run'])
})

it('deadline example passes cancellation to the turn, not only the result wait', async () => {
  const { runWithDeadline } = await helper('deadline.mts')
  const session = { run: (prompt: string, options: { signal: AbortSignal }) => {
    expect(prompt).toBe('Explain')
    expect(options.signal).toBeInstanceOf(AbortSignal)
    expect(options.signal.aborted).toBe(false)
    return { text: async () => 'done' }
  } }
  expect(await runWithDeadline(session, 'Explain')).toBe('done')
})

it('approval example waits for an explicit user decision and does not remember consent', async () => {
  const { reviewPermission } = await helper('approval.mts')
  const allow = vi.fn(async () => {})
  const deny = vi.fn(async () => {})
  const event = { operation: 'files.write', command: '', path: '/project/readme.md', allow, deny }
  const ask = vi.fn(async () => false)
  await reviewPermission(event, ask)
  expect(ask).toHaveBeenCalledWith({ operation: 'files.write', command: '', path: '/project/readme.md' })
  expect(allow).not.toHaveBeenCalled()
  expect(deny).toHaveBeenCalledWith('Declined by the user')
  deny.mockClear()
  await reviewPermission(event, async () => true)
  expect(allow).toHaveBeenCalledWith()
  expect(deny).not.toHaveBeenCalled()
})

it('stream example reports a failed turn rather than returning a partial draft', async () => {
  vi.spyOn(console, 'log').mockImplementation(() => {})
  const { streamTask } = await helper('stream.mts')
  const turn = {
    async *[Symbol.asyncIterator]() { yield { type: 'text', agentId: 'root', delta: 'partial' } },
    result: async () => ({ status: 'failed', text: 'partial', failure: { message: 'Provider unavailable' } }),
  }
  await expect(streamTask({ rootId: 'root', run: () => turn }, 'Explain')).rejects.toThrow('Provider unavailable')
})
