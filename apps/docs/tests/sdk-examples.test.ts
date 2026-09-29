// @vitest-environment node
import { afterEach, expect, it, vi } from 'vitest'
import ts from 'typescript'
import { build } from 'esbuild'
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

it('typechecks every authored SDK example against the actual source API', async () => {
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
  // Bundle against the actual browser-safe source; no prebuilt SDK dist or
  // private testing package is required by the standalone docs CI job.
  const result = await build({
    stdin: { contents: examples.get(name)!, sourcefile: name, resolveDir: workspace, loader: 'ts' },
    bundle: true, write: false, platform: 'node', format: 'esm',
    alias: {
      '@whip/sdk': path.join(workspace, 'packages/sdk/src/index.ts'),
      '@whip/protocol': path.join(workspace, 'packages/protocol/generated/index.js'),
    },
  })
  return import(/* @vite-ignore */ `data:text/javascript;base64,${Buffer.from(result.outputFiles[0]!.text).toString('base64')}`)
}

function view(sessionID = 'root') {
  let listener = () => {}
  let current = { sessionID, runtimeID: 'runtime', status: 'live', preview: null as { text: string } | null }
  return {
    getSnapshot: () => current,
    subscribe: (next: () => void) => { listener = next; return () => { listener = () => {} } },
    start: vi.fn(async () => {}), suspend: vi.fn(async () => {}),
    preview(text: string | null, status = 'live') { current = { ...current, status, preview: text === null ? null : { text } }; listener() },
  }
}

it('run example preserves caller creation/request identities and explicit captured model selection', async () => {
  const { runTask } = await helper('run.mts')
  const definition = { id: 'assistant', revision: 'a'.repeat(64) }
  const model = { provider: 'scripted', name: 'scripted', effort: '' }
  const result = { turn: { state: 'succeeded' } }
  const command = { send: vi.fn(async () => {}), wait: vi.fn(async () => result) }
  const submission = vi.fn(() => command), session = vi.fn(() => ({ submission }))
  const createTree = vi.fn(async () => ({ root: { id: 'root' } }))
  const client = { builtins: [definition], createTree, session }
  expect(await runTask(client, '/project', 'Explain', 'creation', 'request', { model })).toEqual(result)
  expect(createTree).toHaveBeenCalledWith({ engine: 'starlark', definition, working_directory: '/project', metadata: { title: null, archived: false, pinned: false }, overrides: { model } }, 'creation', expect.objectContaining({ signal: expect.any(AbortSignal) }))
  expect(session).toHaveBeenCalledWith('root')
  expect(submission).toHaveBeenCalledWith([{ type: 'text', text: 'Explain' }], 'request')
  expect(command.send).toHaveBeenCalledOnce(); expect(command.wait).toHaveBeenCalledOnce()
})

it('stream example replaces cleared provisional text and returns only the authoritative turn', async () => {
  const log = vi.spyOn(console, 'log').mockImplementation(() => {})
  const { streamTask } = await helper('stream.mts')
  const observation = view(), result = { turn: { id: 'turn', state: 'succeeded' } }
  const signal = new AbortController().signal
  const command = { send: vi.fn(async () => {}), wait: vi.fn(async () => {
    observation.preview('old draft'); observation.preview(null); observation.preview('new draft')
    observation.preview('must clear on disconnect', 'stale')
    return result
  }) }
  const submission = vi.fn(() => command)
  const session = { id: 'root', client: { runtimeID: 'runtime' }, submission }
  expect(await streamTask(session, observation, 'Explain', 'request', signal)).toEqual(result)
  expect(submission).toHaveBeenCalledWith([{ type: 'text', text: 'Explain' }], 'request')
  expect(command.send).toHaveBeenCalledWith({ signal })
  expect(command.wait).toHaveBeenCalledWith({ signal })
  expect(log.mock.calls).toEqual([['Draft:', 'old draft'], ['Draft:', ''], ['Draft:', 'new draft'], ['Draft:', '']])
  expect(observation.suspend).toHaveBeenCalledOnce()
  observation.preview('after cleanup'); expect(log.mock.calls).toHaveLength(4)
})

it('stream example never mixes a child view into root observation or returns a failed partial draft', async () => {
  vi.spyOn(console, 'log').mockImplementation(() => {})
  const { streamTask } = await helper('stream.mts')
  const submission = vi.fn(), session = { id: 'root', client: { runtimeID: 'runtime' }, submission }
  await expect(streamTask(session, view('child'), 'Explain', 'request', new AbortController().signal)).rejects.toThrow('another session')
  expect(submission).not.toHaveBeenCalled()
  const observation = view()
  submission.mockReturnValue({ send: async () => {}, wait: async () => {
    observation.preview('partial')
    return { turn: { state: 'failed', failure: 'Provider unavailable' } }
  } })
  await expect(streamTask(session, observation, 'Explain', 'request', new AbortController().signal)).rejects.toThrow('Provider unavailable')
  expect(observation.suspend).toHaveBeenCalledOnce()
})

it('attachment example waits for an owned upload before submitting its exact reference', async () => {
  const { summarizeNotes } = await helper('attachment.mts')
  const calls: string[] = [], signal = new AbortController().signal
  const result = { turn: { state: 'succeeded' } }
  const upload = vi.fn(async (id, mediaType, data, options) => {
    calls.push('upload'); expect(id).toBe('content-id'); expect(mediaType).toBe('text/plain')
    expect(new TextDecoder().decode(data)).toBe('Notes'); expect(options).toEqual({ signal })
    return { id: 'content-id', session_id: 'root' }
  })
  const submission = vi.fn((parts, requestID) => {
    calls.push('submission'); expect(requestID).toBe('request')
    expect(parts).toEqual([{ type: 'text', text: 'Summarize the attached notes.' }, { type: 'content', reference_id: 'content-id' }])
    return { send: async () => { calls.push('send') }, wait: async () => result }
  })
  expect(await summarizeNotes({ content: { upload }, submission }, 'Notes', 'content-id', 'request', signal)).toEqual(result)
  expect(calls).toEqual(['upload', 'submission', 'send'])
  upload.mockRejectedValueOnce(new Error('Upload delivery unknown'))
  await expect(summarizeNotes({ content: { upload }, submission }, 'Notes', 'same-id', 'request-2', signal)).rejects.toThrow('delivery unknown')
  expect(submission).toHaveBeenCalledOnce()
})

it('a local deadline only bounds observation; explicit cancel first verifies exact acceptance and owner', async () => {
  const { waitWithDeadline, cancelAcceptedInput } = await helper('deadline.mts')
  const cancel = vi.fn(async () => ({ state: 'cancelled' })), signal = new AbortController().signal
  const command = {
    params: { session_id: 'root' }, record: { runtimeID: 'runtime' },
    wait: vi.fn(async ({ signal }) => { expect(signal).toBeInstanceOf(AbortSignal); expect(signal.aborted).toBe(false); return 'observed' }),
    check: vi.fn(async () => ({ state: 'found', evidence: { input: { id: 'input' } } })),
  }
  const session = { id: 'root', client: { runtimeID: 'runtime' }, inputs: { cancel } }
  expect(await waitWithDeadline(command, 1000)).toBe('observed'); expect(cancel).not.toHaveBeenCalled()
  expect(await cancelAcceptedInput(session, command, signal)).toEqual({ state: 'cancelled' })
  expect(cancel).toHaveBeenCalledWith('input', { signal }); cancel.mockClear()
  command.check.mockResolvedValueOnce({ state: 'identity_only', evidence: { input: { id: 'input' } } })
  await expect(cancelAcceptedInput(session, command, signal)).rejects.toThrow('Exact accepted input')
  await expect(cancelAcceptedInput({ ...session, id: 'child' }, command, signal)).rejects.toThrow('another session')
  expect(cancel).not.toHaveBeenCalled()
})

it('approval reads exact capability/resource/arguments and asks again for every explicit decision', async () => {
  const { reviewPermission } = await helper('approval.mts')
  const operation = { id: 'operation', session_id: 'root', state: 'waiting', capability: 'files.write', resource: '/project/readme.md', arguments: { path: '/project/readme.md', content: 'example' } }
  const get = vi.fn(async () => operation), resolve = vi.fn(async () => ({})), ask = vi.fn(async () => false)
  const session = { operations: { get }, permissions: { resolve } }, signal = new AbortController().signal
  await reviewPermission(session, 'operation', ask, signal)
  expect(get).toHaveBeenCalledWith('operation', { signal }); expect(ask).toHaveBeenCalledWith(operation)
  expect(resolve).toHaveBeenLastCalledWith('operation', false, { signal })
  await reviewPermission(session, 'operation', async () => true, signal)
  expect(resolve).toHaveBeenLastCalledWith('operation', true, { signal })
  get.mockResolvedValueOnce({ ...operation, state: 'succeeded' })
  await expect(reviewPermission(session, 'operation', ask, signal)).rejects.toThrow('no longer pending')
  expect(resolve).toHaveBeenCalledTimes(2)
})

it('recovery inspection uses canonical payload matching and never resends or forgets a record', async () => {
  const { createJournal, inspectRecovery } = await helper('recovery.mts')
  const records = new Map<string, unknown>()
  const storage = { list: async () => [...records.values()], put: async (_ns: string, key: string, value: unknown) => { records.set(key, value) }, delete: vi.fn() }
  const journal = createJournal(storage)
  const params = { session_id: 'root', identity: { client_id: 'client', request_id: 'request' }, source: 'user', parts: [{ type: 'text', text: 'exact input' }] }
  await journal.put({ namespace: 'whip.v4.commands', version: 1, runtimeID: 'runtime', clientID: 'client', accepted: false, request: JSON.stringify({ method: 'sessions.submit', params }) })
  const evidence = { receipt: { identity: params.identity }, input: { session_id: 'root' }, turn: null }
  const call = vi.fn(async (method, input) => {
    expect(method).toBe('receipts.match'); expect(input.method).toBe('sessions.submit')
    expect(JSON.parse(Buffer.from(input.params_base64, 'base64').toString())).toEqual(params)
    return evidence
  })
  const checks = await inspectRecovery({ runtimeID: 'runtime', clientID: 'client', call }, journal, new AbortController().signal)
  expect(checks).toHaveLength(1); expect(checks[0].check).toEqual({ state: 'found', evidence })
  expect(checks[0].record.accepted).toBe(true); expect(call).toHaveBeenCalledOnce(); expect(storage.delete).not.toHaveBeenCalled()
})
